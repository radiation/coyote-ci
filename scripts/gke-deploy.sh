#!/usr/bin/env bash
set -euo pipefail

namespace="${GKE_NAMESPACE:-coyote-ci}"
api_url="${COYOTE_INTERNAL_API_URL:-${API_URL:-}}"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

require_command kubectl

if [[ "$namespace" != "coyote-ci" ]]; then
  echo "GKE_NAMESPACE must be coyote-ci because the GKE worker manifest is namespace-specific" >&2
  exit 1
fi

if [[ -z "$api_url" || "$api_url" == *"localhost"* || "$api_url" == *"host.docker.internal"* ]]; then
  echo "COYOTE_INTERNAL_API_URL must be a GKE-reachable Coyote server URL" >&2
  exit 1
fi

if ! kubectl -n "$namespace" get secret coyote-gke-worker >/dev/null 2>&1; then
  echo "missing secret coyote-gke-worker in namespace $namespace; create it with DATABASE_URL first" >&2
  exit 1
fi

kubectl -n "$namespace" create configmap coyote-gke-worker \
  --from-literal=WORKER_KUBERNETES_INTERNAL_API_URL="${api_url%/}" \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f "$repo_root/deploy/kubernetes/gke/worker.yaml"
kubectl -n "$namespace" rollout status deployment/coyote-kubernetes-worker --timeout="${GKE_DEPLOY_TIMEOUT_SECONDS:-300}s"
kubectl -n "$namespace" get deployment,pods -l app.kubernetes.io/name=coyote-kubernetes-worker -o wide
kubectl -n "$namespace" get deployment coyote-kubernetes-worker -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'