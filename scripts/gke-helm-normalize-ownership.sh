#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
control_chart="$repo_root/deploy/helm/coyote-ci"
worker_chart="$repo_root/deploy/helm/coyote-ci-worker"
control_values="${GKE_HELM_CONTROL_PLANE_VALUES:-$repo_root/.local/helm/gke-staging-control-plane-values.yaml}"
worker_values="${GKE_HELM_WORKER_VALUES:-$repo_root/.local/helm/gke-staging-worker-values.yaml}"
control_namespace="${GKE_HELM_CONTROL_PLANE_NAMESPACE:-coyote-ci}"
worker_namespace="${GKE_HELM_WORKER_NAMESPACE:-coyote-ci-staging}"
control_release="${GKE_HELM_CONTROL_PLANE_RELEASE:-coyote-ci}"
worker_release="${GKE_HELM_WORKER_RELEASE:-coyote-ci-worker}"
timeout_seconds="${GKE_DEPLOY_TIMEOUT_SECONDS:-600}"
apply="${GKE_HELM_NORMALIZE_APPLY:-false}"
adoption_verifier="${GKE_HELM_ADOPT_SCRIPT:-$repo_root/scripts/gke-helm-adopt.sh}"
field_manager="coyote-rollout"

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

release_is_deployed() {
  local release="$1"
  local namespace="$2"
  helm status "$release" --namespace "$namespace" --output json |
    ruby -rjson -e 'exit(JSON.parse(STDIN.read).dig("info", "status") == "deployed" ? 0 : 1)'
}

external_replica_value() {
  local values="$1"
  local section="$2"
  ruby -ryaml -e '
    values = YAML.load_file(ARGV.fetch(0))
    section = values.fetch(ARGV.fetch(1))
    abort "#{ARGV.fetch(1)}.replicasManagedExternally must be true" unless section["replicasManagedExternally"] == true
    replicas = section["replicas"]
    abort "#{ARGV.fetch(1)}.replicas must be a non-negative integer" unless replicas.is_a?(Integer) && replicas >= 0
    puts replicas
  ' "$values" "$section"
}

run() {
  printf '+'
  printf ' %q' "$@"
  printf '\n'
  if [[ "$apply" == "true" ]]; then
    "$@"
  fi
}

scale_deployment() {
  local namespace="$1"
  local deployment="$2"
  local replicas="$3"
  run kubectl -n "$namespace" patch "deployment/$deployment" --subresource=scale --type merge --field-manager="$field_manager" --patch "{\"spec\":{\"replicas\":$replicas}}"
}

replica_ownership_is_external() {
  local namespace="$1"
  local deployment="$2"
  local desired="$3"
  kubectl -n "$namespace" get "deployment/$deployment" -o json |
    ruby -rjson -e '
      document = JSON.parse(STDIN.read)
      fields = document.fetch("metadata", {}).fetch("managedFields", [])
      owns = ->(entry) { entry.dig("fieldsV1", "f:spec", "f:replicas") }
      helm_owns = fields.any? { |entry| entry["manager"] == "helm" && entry["subresource"].to_s.empty? && owns.call(entry) }
      handoff_owns = fields.any? { |entry| entry["manager"] == "helm-replica-handoff" && owns.call(entry) }
      rollout_owns = fields.any? { |entry| entry["manager"] == ARGV.fetch(0) && entry["subresource"] == "scale" && owns.call(entry) }
      kubectl_owns = fields.any? { |entry| entry["manager"] == "kubectl" && entry["subresource"] == "scale" && owns.call(entry) }
      replicas = document.dig("spec", "replicas")
      exit(helm_owns || handoff_owns || kubectl_owns || !rollout_owns || replicas != Integer(ARGV.fetch(1)) ? 1 : 0)
    ' "$field_manager" "$desired"
}

require_command helm
require_command kubectl
require_command ruby
[[ "$apply" == "true" || "$apply" == "false" ]] || { echo "GKE_HELM_NORMALIZE_APPLY must be true or false" >&2; exit 1; }
[[ -x "$adoption_verifier" ]] || { echo "adoption verifier is not executable: $adoption_verifier" >&2; exit 1; }
[[ -f "$control_values" ]] || { echo "control-plane values file does not exist: $control_values" >&2; exit 1; }
[[ -f "$worker_values" ]] || { echo "worker values file does not exist: $worker_values" >&2; exit 1; }
release_is_deployed "$control_release" "$control_namespace" || { echo "control-plane Helm release is not deployed" >&2; exit 1; }
release_is_deployed "$worker_release" "$worker_namespace" || { echo "worker Helm release is not deployed" >&2; exit 1; }

GKE_HELM_CONTROL_PLANE_VALUES="$control_values" \
GKE_HELM_WORKER_VALUES="$worker_values" \
GKE_HELM_ADOPT_VERIFY_ONLY=true \
"$adoption_verifier"

server_replicas="$(external_replica_value "$control_values" server)"
frontend_replicas="$(external_replica_value "$control_values" frontend)"
worker_replicas="$(external_replica_value "$worker_values" worker)"

echo "One-time external replica ownership normalization: coyote-rollout owns the scale subresource."
scale_deployment "$control_namespace" coyote-server "$server_replicas"
scale_deployment "$control_namespace" coyote-frontend "$frontend_replicas"
scale_deployment "$worker_namespace" coyote-kubernetes-worker-staging "$worker_replicas"

run helm upgrade "$control_release" "$control_chart" --namespace "$control_namespace" --values "$control_values" --wait --timeout "${timeout_seconds}s"
run helm upgrade "$worker_release" "$worker_chart" --namespace "$worker_namespace" --values "$worker_values" --wait --timeout "${timeout_seconds}s"

if [[ "$apply" == "true" ]]; then
  replica_ownership_is_external "$control_namespace" coyote-server "$server_replicas" || { echo "external replica ownership verification failed for coyote-server" >&2; exit 1; }
  replica_ownership_is_external "$control_namespace" coyote-frontend "$frontend_replicas" || { echo "external replica ownership verification failed for coyote-frontend" >&2; exit 1; }
  replica_ownership_is_external "$worker_namespace" coyote-kubernetes-worker-staging "$worker_replicas" || { echo "external replica ownership verification failed for coyote-kubernetes-worker-staging" >&2; exit 1; }
  kubectl -n "$control_namespace" rollout status deployment/coyote-server --timeout="${timeout_seconds}s"
  kubectl -n "$control_namespace" rollout status deployment/coyote-frontend --timeout="${timeout_seconds}s"
  kubectl -n "$worker_namespace" rollout status deployment/coyote-kubernetes-worker-staging --timeout="${timeout_seconds}s"
  echo "External replica ownership normalization completed."
else
  echo "Dry run only. Set GKE_HELM_NORMALIZE_APPLY=true for the reviewed one-time normalization."
fi
