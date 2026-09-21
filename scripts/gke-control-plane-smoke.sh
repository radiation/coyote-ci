#!/usr/bin/env bash
set -euo pipefail

namespace="${GKE_NAMESPACE:-coyote-ci}"
timeout_seconds="${GKE_SMOKE_TIMEOUT_SECONDS:-300}"
server_port="${GKE_CONTROL_PLANE_SERVER_PORT:-18080}"
frontend_port="${GKE_CONTROL_PLANE_FRONTEND_PORT:-13000}"

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

wait_for_http() {
  local url="$1"
  local deadline=$(( $(date +%s) + timeout_seconds ))
  while (( $(date +%s) < deadline )); do
    if curl --fail --silent --show-error "$url" >/dev/null; then
      return
    fi
    sleep 2
  done
  echo "timed out waiting for $url" >&2
  return 1
}

require_command kubectl
require_command curl
require_command jq

[[ "$namespace" == "coyote-ci" ]] || { echo "GKE_NAMESPACE must be coyote-ci" >&2; exit 1; }
kubectl -n "$namespace" wait --for=condition=complete job/coyote-migrate --timeout="${timeout_seconds}s"
kubectl -n "$namespace" rollout status deployment/coyote-server --timeout="${timeout_seconds}s"
kubectl -n "$namespace" rollout status deployment/coyote-frontend --timeout="${timeout_seconds}s"

for deployment in coyote-server coyote-frontend; do
  image="$(kubectl -n "$namespace" get deployment "$deployment" -o jsonpath='{.spec.template.spec.containers[0].image}')"
  [[ "$image" == *@sha256:* ]] || { echo "$deployment is not digest-pinned: $image" >&2; exit 1; }
done

server_pod="$(kubectl -n "$namespace" get pods -l app.kubernetes.io/name=coyote-server -o jsonpath='{.items[0].metadata.name}')"
[[ -n "$server_pod" ]] || { echo "coyote-server Pod was not found" >&2; exit 1; }
pod_json="$(kubectl -n "$namespace" get pod "$server_pod" -o json)"
kubectl -n "$namespace" get pod "$server_pod" -o jsonpath='{.spec.serviceAccountName}' | grep -qx coyote-server
kubectl -n "$namespace" get pod "$server_pod" -o jsonpath='{.status.containerStatuses[?(@.name=="cloud-sql-proxy")].ready}' | grep -qx true
jq -e '.spec.containers[] | select(.name == "server") | [.env[] | select(.name == "DATABASE_URL_FILE" and .value == "/var/run/secrets/coyote/database-url")] | length == 1' <<<"$pod_json" >/dev/null
jq -e '.spec.containers[] | select(.name == "server") | [.env[] | select(.name == "COYOTE_WORKSPACE_HELPER_KUBECONFIG")] | length == 0' <<<"$pod_json" >/dev/null
jq -e '.spec.volumes[] | select(.name == "control-plane-secrets") | .csi.volumeAttributes.secretProviderClass == "coyote-server-secrets"' <<<"$pod_json" >/dev/null
jq -e '.spec.volumes[] | select(.name == "tmp") | .emptyDir.sizeLimit == "4Gi"' <<<"$pod_json" >/dev/null
config="$(kubectl -n "$namespace" get configmap coyote-control-plane -o json)"
jq -e '.data.WORKSPACE_REVISION_STORAGE_PROVIDER == "gcs" and .data.WORKER_CACHE_STORAGE_PROVIDER == "gcs" and .data.ARTIFACT_STORAGE_PROVIDER == "gcs"' <<<"$config" >/dev/null
migration_job="$(kubectl -n "$namespace" get job coyote-migrate -o json)"
jq -e '.spec.template.spec.volumes[] | select(.name == "database-url") | .csi.volumeAttributes.secretProviderClass == "coyote-migrate-secrets"' <<<"$migration_job" >/dev/null

kubectl -n "$namespace" port-forward service/coyote-server "${server_port}:8080" >/tmp/coyote-server-port-forward.log 2>&1 &
server_forward_pid=$!
kubectl -n "$namespace" port-forward service/coyote-frontend "${frontend_port}:3000" >/tmp/coyote-frontend-port-forward.log 2>&1 &
frontend_forward_pid=$!
cleanup() {
  kill "$server_forward_pid" "$frontend_forward_pid" 2>/dev/null || true
}
trap cleanup EXIT

wait_for_http "http://127.0.0.1:${server_port}/healthz"
wait_for_http "http://127.0.0.1:${server_port}/readyz"
wait_for_http "http://127.0.0.1:${frontend_port}/"
wait_for_http "http://127.0.0.1:${frontend_port}/api/readyz"
echo "gke control-plane smoke passed"
