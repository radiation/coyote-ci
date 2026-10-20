#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
control_chart="$repo_root/deploy/helm/coyote-ci"
worker_chart="$repo_root/deploy/helm/coyote-ci-worker"
control_values="${GKE_HELM_CONTROL_PLANE_VALUES:-$repo_root/.local/helm/gke-staging-control-plane-values.yaml}"
worker_values="${GKE_HELM_WORKER_VALUES:-$repo_root/.local/helm/gke-staging-worker-values.yaml}"
release_values="${GKE_HELM_RELEASE_VALUES:-}"
control_namespace="${GKE_HELM_CONTROL_PLANE_NAMESPACE:-coyote-ci}"
worker_namespace="${GKE_HELM_WORKER_NAMESPACE:-coyote-ci-staging}"
control_release="${GKE_HELM_CONTROL_PLANE_RELEASE:-coyote-ci}"
worker_release="${GKE_HELM_WORKER_RELEASE:-coyote-ci-worker}"
timeout_seconds="${GKE_DEPLOY_TIMEOUT_SECONDS:-600}"
dry_run="${GKE_HELM_DRY_RUN:-false}"
unmanaged_mcp_database_isolation_confirmed="${GKE_HELM_UNMANAGED_MCP_DATABASE_ISOLATION_CONFIRMED:-false}"
rollout_id="${HELM_ROLLOUT_ID:-$(date -u +%Y%m%d%H%M%S)-${RANDOM}}"
migration_job="coyote-migrate-${rollout_id}"

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

run() {
  printf '+'
  printf ' %q' "$@"
  printf '\n'
  if [[ "$dry_run" != "true" ]]; then
    "$@"
  fi
}

require_command helm
require_command kubectl
require_command ruby
[[ -f "$control_values" ]] || { echo "control-plane values file does not exist: $control_values" >&2; exit 1; }
[[ -f "$worker_values" ]] || { echo "worker values file does not exist: $worker_values" >&2; exit 1; }
[[ -z "$release_values" || -f "$release_values" ]] || { echo "release values file does not exist: $release_values" >&2; exit 1; }
[[ "$unmanaged_mcp_database_isolation_confirmed" == "true" || "$unmanaged_mcp_database_isolation_confirmed" == "false" ]] || {
  echo "GKE_HELM_UNMANAGED_MCP_DATABASE_ISOLATION_CONFIRMED must be true or false" >&2
  exit 1
}
[[ "$control_namespace" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || { echo "invalid control-plane namespace: $control_namespace" >&2; exit 1; }
[[ "$worker_namespace" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || { echo "invalid worker namespace: $worker_namespace" >&2; exit 1; }
[[ "$control_release" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || { echo "invalid control-plane release: $control_release" >&2; exit 1; }
[[ "$worker_release" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || { echo "invalid worker release: $worker_release" >&2; exit 1; }
[[ "$rollout_id" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ && ${#rollout_id} -le 48 ]] || {
  echo "HELM_ROLLOUT_ID must be a Kubernetes-safe lowercase identifier of at most 48 characters" >&2
  exit 1
}

external_replica_value() {
  local values="$1"
  local section="$2"
  ruby -ryaml -e '
    values = YAML.load_file(ARGV.fetch(0))
    section = values.fetch(ARGV.fetch(1))
    abort "#{ARGV.fetch(1)}.replicasManagedExternally must be true for GKE Helm rollout" unless section["replicasManagedExternally"] == true
    replicas = section["replicas"]
    abort "#{ARGV.fetch(1)}.replicas must be a non-negative integer" unless replicas.is_a?(Integer) && replicas >= 0
    puts replicas
  ' "$values" "$section"
}

worker_deployment_name() {
  ruby -ryaml -e '
    values = YAML.load_file(ARGV.fetch(0))
    name = values.fetch("worker").fetch("name", "coyote-kubernetes-worker-staging")
    abort "worker.name must be a Kubernetes-safe name" unless name.match?(/\A[a-z0-9]([-a-z0-9]*[a-z0-9])?\z/)
    puts name
  ' "$worker_values"
}

mcp_enabled() {
  ruby -ryaml -e '
    values = YAML.load_file(ARGV.fetch(0))
    puts values.fetch("mcp", {}).fetch("enabled", false) == true
  ' "$control_values"
}

mcp_deployment_state() {
  local deployment
  if ! deployment="$(kubectl -n "$control_namespace" get deployment/coyote-mcp --ignore-not-found -o json)"; then
    echo "failed to determine coyote-mcp ownership" >&2
    return 2
  fi
  if [[ -z "$deployment" ]]; then
    printf '%s\n' absent
    return
  fi
  ruby -rjson -e '
    metadata = JSON.parse(STDIN.read).fetch("metadata", {})
    actual = [
      metadata.fetch("labels", {})["app.kubernetes.io/managed-by"],
      metadata.fetch("annotations", {})["meta.helm.sh/release-name"],
      metadata.fetch("annotations", {})["meta.helm.sh/release-namespace"]
    ]
    expected = ["Helm", ARGV.fetch(0), ARGV.fetch(1)]
    puts actual == expected ? "managed" : actual.all?(&:nil?) ? "unowned" : "other"
  ' "$control_release" "$control_namespace" <<<"$deployment"
}

scale_deployment() {
  local namespace="$1"
  local deployment="$2"
  local replicas="$3"
  run kubectl -n "$namespace" patch "deployment/$deployment" --subresource=scale --type merge --field-manager=coyote-rollout --patch "{\"spec\":{\"replicas\":$replicas}}"
}

server_replicas="$(external_replica_value "$control_values" server)"
frontend_replicas="$(external_replica_value "$control_values" frontend)"
worker_replicas="$(external_replica_value "$worker_values" worker)"
worker_deployment="$(worker_deployment_name)"
control_values_args=(--values "$control_values")
worker_values_args=(--values "$worker_values")
if [[ -n "$release_values" ]]; then
  control_values_args+=(--values "$release_values")
  worker_values_args+=(--values "$release_values")
fi
mcp_replicas=""
mcp_is_desired=false
if [[ "$(mcp_enabled)" == "true" ]]; then
  mcp_is_desired=true
  mcp_replicas="$(external_replica_value "$control_values" mcp)"
fi

# Rendering validates the supplied digest-pinned images and chart-required inputs before mutation.
helm template "$control_release" "$control_chart" --namespace "$control_namespace" "${control_values_args[@]}" >/dev/null
helm template "$worker_release" "$worker_chart" --namespace "$worker_namespace" "${worker_values_args[@]}" >/dev/null
gateway_manifest="$(helm template "$control_release" "$control_chart" --namespace "$control_namespace" "${control_values_args[@]}" --show-only templates/gateway.yaml)"
migrator_prerequisite_manifest="$(helm template "$control_release" "$control_chart" --namespace "$control_namespace" "${control_values_args[@]}" --show-only templates/identities-rbac.yaml --show-only templates/secrets.yaml | ruby -ryaml -e '
  documents = YAML.load_stream(STDIN.read).compact
  required = [
    ["v1", "ServiceAccount", "coyote-migrate"],
    ["secrets-store.csi.x-k8s.io/v1", "SecretProviderClass", "coyote-migrate-secrets"]
  ]
  selected = required.map do |api_version, kind, name|
    documents.find do |document|
      document["apiVersion"] == api_version && document["kind"] == kind && document.dig("metadata", "name") == name
    end
  end
  missing = required.zip(selected).map { |resource, document| resource.join("/") unless document }.compact
  abort "missing migrator prerequisite resources: #{missing.join(", ")}" unless missing.empty?
  puts YAML.dump_stream(*selected)
')"
migration_manifest="$(helm template "$control_release" "$control_chart" --namespace "$control_namespace" "${control_values_args[@]}" --show-only templates/migration-job.yaml --set migration.render=true --set "migration.runID=$rollout_id")"
[[ -n "$migration_manifest" ]] || { echo "migration render did not produce a Job" >&2; exit 1; }

helm status "$control_release" --namespace "$control_namespace" >/dev/null || {
  echo "control-plane Helm release is not installed; Slice 3 adoption must complete before Helm rollout" >&2
  exit 1
}
helm status "$worker_release" --namespace "$worker_namespace" >/dev/null || {
  echo "worker Helm release is not installed; Slice 3 adoption must complete before Helm rollout" >&2
  exit 1
}

mcp_state="$(mcp_deployment_state)" || exit $?
case "$mcp_state:$mcp_is_desired" in
  managed:false)
    echo "coyote-mcp is owned by Helm release $control_release but mcp.enabled is false; refusing a rollout that would delete it" >&2
    exit 1
    ;;
  unowned:true|other:true)
    echo "coyote-mcp exists but is not owned by Helm release $control_release; refusing to adopt or scale it" >&2
    exit 1
    ;;
  unowned:false)
    [[ "$unmanaged_mcp_database_isolation_confirmed" == "true" ]] || {
      echo "coyote-mcp is unowned and disabled; verify it cannot access the coyote_ci database, then set GKE_HELM_UNMANAGED_MCP_DATABASE_ISOLATION_CONFIRMED=true to continue without changing it" >&2
      exit 1
    }
    echo "coyote-mcp is unowned; operator confirmed database isolation, leaving it unchanged" >&2
    ;;
  other:false)
    echo "coyote-mcp is owned by another or partial Helm release; refusing to continue while it remains unmanaged by $control_release" >&2
    exit 1
    ;;
esac

echo "Phase A: scale all Helm-managed database consumers to zero before migration"
scale_deployment "$control_namespace" coyote-server 0
scale_deployment "$control_namespace" coyote-frontend 0
if [[ "$mcp_is_desired" == "true" && "$mcp_state" == "managed" ]]; then
  scale_deployment "$control_namespace" coyote-mcp 0
fi
scale_deployment "$worker_namespace" "$worker_deployment" 0
run kubectl -n "$control_namespace" rollout status deployment/coyote-server --timeout="${timeout_seconds}s"
run kubectl -n "$control_namespace" rollout status deployment/coyote-frontend --timeout="${timeout_seconds}s"
if [[ "$mcp_is_desired" == "true" && "$mcp_state" == "managed" ]]; then
  run kubectl -n "$control_namespace" rollout status deployment/coyote-mcp --timeout="${timeout_seconds}s"
fi
run kubectl -n "$worker_namespace" rollout status "deployment/$worker_deployment" --timeout="${timeout_seconds}s"

echo "Phase B: update migrator prerequisites"
if [[ "$dry_run" == "true" ]]; then
  printf '+ kubectl -n %q apply -f - # rendered migrator ServiceAccount and SecretProviderClass\n' "$control_namespace"
else
  if ! printf '%s\n' "$migrator_prerequisite_manifest" | kubectl -n "$control_namespace" apply -f -; then
    echo "migrator prerequisites were not applied; no migration Job or application replicas were started" >&2
    exit 1
  fi
fi

echo "Phase C: apply and wait for migration Job $migration_job"
if [[ "$dry_run" == "true" ]]; then
  printf '+ kubectl -n %q apply -f - # rendered migration Job %q\n' "$control_namespace" "$migration_job"
  printf '+ kubectl -n %q wait --for=condition=complete job/%q --timeout=%qs\n' "$control_namespace" "$migration_job" "$timeout_seconds"
else
  if ! printf '%s\n' "$migration_manifest" | kubectl -n "$control_namespace" apply -f -; then
    echo "migration Job $migration_job was not applied; no application replicas were started" >&2
    exit 1
  fi
  if ! kubectl -n "$control_namespace" wait --for=condition=complete "job/$migration_job" --timeout="${timeout_seconds}s"; then
    echo "migration Job $migration_job failed or timed out; it remains for inspection" >&2
    kubectl -n "$control_namespace" describe "job/$migration_job" >&2 || true
    kubectl -n "$control_namespace" logs "job/$migration_job" --all-containers=true >&2 || true
    exit 1
  fi
fi

echo "Phase D: restore the desired control-plane replicas"
run helm upgrade "$control_release" "$control_chart" --namespace "$control_namespace" "${control_values_args[@]}" --wait --timeout "${timeout_seconds}s"
scale_deployment "$control_namespace" coyote-server "$server_replicas"
scale_deployment "$control_namespace" coyote-frontend "$frontend_replicas"
if [[ "$mcp_is_desired" == "true" ]]; then
  scale_deployment "$control_namespace" coyote-mcp "$mcp_replicas"
fi
run kubectl -n "$control_namespace" rollout status deployment/coyote-server --timeout="${timeout_seconds}s"
run kubectl -n "$control_namespace" rollout status deployment/coyote-frontend --timeout="${timeout_seconds}s"
if [[ "$mcp_is_desired" == "true" ]]; then
  run kubectl -n "$control_namespace" rollout status deployment/coyote-mcp --timeout="${timeout_seconds}s"
fi

echo "Phase E: restore the desired worker replicas"
run helm upgrade "$worker_release" "$worker_chart" --namespace "$worker_namespace" "${worker_values_args[@]}" --wait --timeout "${timeout_seconds}s"
scale_deployment "$worker_namespace" "$worker_deployment" "$worker_replicas"
run kubectl -n "$worker_namespace" rollout status "deployment/$worker_deployment" --timeout="${timeout_seconds}s"

if [[ -n "$gateway_manifest" ]]; then
  require_command jq
  gateway_ready() {
    jq -e 'def condition_true($type): any(.status.conditions[]?; .type == $type and .status == "True"); condition_true("Accepted") and condition_true("Programmed")' >/dev/null
  }
  http_route_ready() {
    jq -e 'def condition_true($type): any(.conditions[]?; .type == $type and .status == "True"); any(.status.parents[]?; condition_true("Accepted") and condition_true("ResolvedRefs"))' >/dev/null
  }

  echo "Phase F: verify Gateway and HTTPRoute conditions"
  if [[ "$dry_run" == "true" ]]; then
    printf '+ kubectl -n %q get gateway coyote-temporary-public -o json # wait for Accepted and Programmed\n' "$control_namespace"
    printf '+ kubectl -n %q get httproute coyote-temporary-public coyote-temporary-http-redirect -o json # wait for Accepted and ResolvedRefs\n' "$control_namespace"
  else
    deadline=$(( $(date +%s) + timeout_seconds ))
    while (( $(date +%s) < deadline )); do
      gateway="$(kubectl -n "$control_namespace" get gateway coyote-temporary-public -o json)"
      route="$(kubectl -n "$control_namespace" get httproute coyote-temporary-public -o json)"
      redirect_route="$(kubectl -n "$control_namespace" get httproute coyote-temporary-http-redirect -o json)"
      if gateway_ready <<<"$gateway" && http_route_ready <<<"$route" && http_route_ready <<<"$redirect_route"; then
        break
      fi
      sleep 5
    done
    if ! gateway_ready <<<"$gateway" || ! http_route_ready <<<"$route" || ! http_route_ready <<<"$redirect_route"; then
      echo "Gateway or HTTPRoute did not become ready within ${timeout_seconds}s" >&2
      kubectl -n "$control_namespace" describe gateway coyote-temporary-public >&2 || true
      kubectl -n "$control_namespace" describe httproute coyote-temporary-public coyote-temporary-http-redirect >&2 || true
      exit 1
    fi
  fi
fi

echo "Helm rollout completed; run the existing smoke tests as the final validation layer."
