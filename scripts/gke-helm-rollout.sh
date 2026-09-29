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
dry_run="${GKE_HELM_DRY_RUN:-false}"
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
[[ -f "$control_values" ]] || { echo "control-plane values file does not exist: $control_values" >&2; exit 1; }
[[ -f "$worker_values" ]] || { echo "worker values file does not exist: $worker_values" >&2; exit 1; }
[[ "$control_namespace" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || { echo "invalid control-plane namespace: $control_namespace" >&2; exit 1; }
[[ "$worker_namespace" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || { echo "invalid worker namespace: $worker_namespace" >&2; exit 1; }
[[ "$control_release" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || { echo "invalid control-plane release: $control_release" >&2; exit 1; }
[[ "$worker_release" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || { echo "invalid worker release: $worker_release" >&2; exit 1; }
[[ "$rollout_id" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ && ${#rollout_id} -le 48 ]] || {
  echo "HELM_ROLLOUT_ID must be a Kubernetes-safe lowercase identifier of at most 48 characters" >&2
  exit 1
}

# Rendering validates the supplied digest-pinned images and chart-required inputs before mutation.
helm template "$control_release" "$control_chart" --namespace "$control_namespace" --values "$control_values" >/dev/null
helm template "$worker_release" "$worker_chart" --namespace "$worker_namespace" --values "$worker_values" >/dev/null
gateway_manifest="$(helm template "$control_release" "$control_chart" --namespace "$control_namespace" --values "$control_values" --show-only templates/gateway.yaml)"
migration_manifest="$(helm template "$control_release" "$control_chart" --namespace "$control_namespace" --values "$control_values" --show-only templates/migration-job.yaml --set migration.render=true --set "migration.runID=$rollout_id")"
[[ -n "$migration_manifest" ]] || { echo "migration render did not produce a Job" >&2; exit 1; }

helm status "$control_release" --namespace "$control_namespace" >/dev/null || {
  echo "control-plane Helm release is not installed; Slice 3 adoption must complete before Helm rollout" >&2
  exit 1
}
helm status "$worker_release" --namespace "$worker_namespace" >/dev/null || {
  echo "worker Helm release is not installed; Slice 3 adoption must complete before Helm rollout" >&2
  exit 1
}

echo "Phase A: scale all Helm-managed database consumers to zero before migration"
run helm upgrade "$control_release" "$control_chart" --namespace "$control_namespace" --values "$control_values" --set server.replicas=0 --set frontend.replicas=0 --wait --timeout "${timeout_seconds}s"
run helm upgrade "$worker_release" "$worker_chart" --namespace "$worker_namespace" --values "$worker_values" --set worker.replicas=0 --wait --timeout "${timeout_seconds}s"

echo "Phase B: apply and wait for migration Job $migration_job"
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

echo "Phase C: restore the desired control-plane replicas"
run helm upgrade "$control_release" "$control_chart" --namespace "$control_namespace" --values "$control_values" --wait --timeout "${timeout_seconds}s"
run kubectl -n "$control_namespace" rollout status deployment/coyote-server --timeout="${timeout_seconds}s"
run kubectl -n "$control_namespace" rollout status deployment/coyote-frontend --timeout="${timeout_seconds}s"

echo "Phase D: restore the desired worker replicas"
run helm upgrade "$worker_release" "$worker_chart" --namespace "$worker_namespace" --values "$worker_values" --wait --timeout "${timeout_seconds}s"
run kubectl -n "$worker_namespace" rollout status deployment/coyote-kubernetes-worker-staging --timeout="${timeout_seconds}s"

if [[ -n "$gateway_manifest" ]]; then
  require_command jq
  gateway_ready() {
    jq -e 'def condition_true($type): any(.status.conditions[]?; .type == $type and .status == "True"); condition_true("Accepted") and condition_true("Programmed")' >/dev/null
  }
  http_route_ready() {
    jq -e 'def condition_true($type): any(.conditions[]?; .type == $type and .status == "True"); any(.status.parents[]?; condition_true("Accepted") and condition_true("ResolvedRefs"))' >/dev/null
  }

  echo "Phase E: verify Gateway and HTTPRoute conditions"
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
