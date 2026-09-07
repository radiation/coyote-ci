#!/usr/bin/env bash
set -euo pipefail

namespace="${GKE_NAMESPACE:-coyote-ci}"
api_url="${COYOTE_INTERNAL_API_URL:-${API_URL:-}}"
expected_worker_gsa="${GKE_WORKER_GSA_EMAIL:-coyote-gke-worker@bryanchoate.iam.gserviceaccount.com}"
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

if ! kubectl -n "$namespace" get serviceaccount coyote-kubernetes-worker >/dev/null 2>&1; then
  echo "missing externally bootstrapped ServiceAccount coyote-kubernetes-worker in namespace $namespace" >&2
  exit 1
fi

worker_gsa="$(kubectl -n "$namespace" get serviceaccount coyote-kubernetes-worker -o jsonpath='{.metadata.annotations.iam\.gke\.io/gcp-service-account}')"
if [[ "$worker_gsa" != "$expected_worker_gsa" ]]; then
  echo "ServiceAccount coyote-kubernetes-worker must have iam.gke.io/gcp-service-account=$expected_worker_gsa; got ${worker_gsa:-<missing>}" >&2
  exit 1
fi

if ! kubectl -n "$namespace" get secretproviderclass coyote-database-secrets >/dev/null 2>&1; then
  echo "missing externally bootstrapped SecretProviderClass coyote-database-secrets in namespace $namespace" >&2
  exit 1
fi

kubectl -n "$namespace" create configmap coyote-gke-worker \
  --from-literal=WORKER_KUBERNETES_INTERNAL_API_URL="${api_url%/}" \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f "$repo_root/deploy/kubernetes/gke/worker.yaml"
kubectl -n "$namespace" rollout status deployment/coyote-kubernetes-worker --timeout="${GKE_DEPLOY_TIMEOUT_SECONDS:-300}s"
kubectl -n "$namespace" get deployment,pods -l app.kubernetes.io/name=coyote-kubernetes-worker -o wide
kubectl -n "$namespace" get deployment coyote-kubernetes-worker -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'