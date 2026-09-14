#!/usr/bin/env bash
set -euo pipefail

namespace="${GKE_NAMESPACE:-coyote-ci}"
api_url="${COYOTE_INTERNAL_API_URL:-${API_URL:-}}"
expected_worker_gsa="${GKE_WORKER_GSA_EMAIL:-}"
gcp_project="${GCP_PROJECT:-bryanchoate}"
gcp_region="${GCP_REGION:-us-central1}"
artifact_repository="${ARTIFACT_REPOSITORY:-coyote-ci}"
cloud_build_location="${CLOUD_BUILD_LOCATION:-$gcp_region}"
cloud_build_runtime_service_account="${CLOUD_BUILD_RUNTIME_SERVICE_ACCOUNT:-coyote-cloud-build@${gcp_project}.iam.gserviceaccount.com}"
cloud_build_artifact_registry_repository="${CLOUD_BUILD_ARTIFACT_REGISTRY_REPOSITORY:-${gcp_region}-docker.pkg.dev/${gcp_project}/${artifact_repository}}"
cloud_build_source_bucket="${CLOUD_BUILD_SOURCE_BUCKET:-${ARTIFACT_GCS_BUCKET:-bryanchoate-coyote-ci-artifacts}}"
max_in_flight_jobs="${WORKER_KUBERNETES_MAX_IN_FLIGHT_JOBS:-8}"
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

if [[ -z "$expected_worker_gsa" ]]; then
  echo "GKE_WORKER_GSA_EMAIL must name the Google service account bound to coyote-kubernetes-worker" >&2
  exit 1
fi

if [[ -z "$cloud_build_location" ]]; then
  echo "CLOUD_BUILD_LOCATION must not be empty" >&2
  exit 1
fi

if [[ -z "$cloud_build_runtime_service_account" ]]; then
  echo "CLOUD_BUILD_RUNTIME_SERVICE_ACCOUNT must not be empty" >&2
  exit 1
fi

if [[ -z "$cloud_build_artifact_registry_repository" ]]; then
  echo "CLOUD_BUILD_ARTIFACT_REGISTRY_REPOSITORY must not be empty" >&2
  exit 1
fi

if [[ -z "$cloud_build_source_bucket" ]]; then
  echo "CLOUD_BUILD_SOURCE_BUCKET must not be empty" >&2
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
sed \
  -e "s|__GCP_PROJECT__|$gcp_project|g" \
  -e "s|__CLOUD_BUILD_LOCATION__|$cloud_build_location|g" \
  -e "s|__CLOUD_BUILD_RUNTIME_SERVICE_ACCOUNT__|$cloud_build_runtime_service_account|g" \
  -e "s|__CLOUD_BUILD_ARTIFACT_REGISTRY_REPOSITORY__|$cloud_build_artifact_registry_repository|g" \
  -e "s|__CLOUD_BUILD_SOURCE_BUCKET__|$cloud_build_source_bucket|g" \
  -e "s|__WORKER_KUBERNETES_MAX_IN_FLIGHT_JOBS__|$max_in_flight_jobs|g" \
  "$repo_root/deploy/kubernetes/gke/worker.yaml" | kubectl apply -f -
kubectl -n "$namespace" rollout status deployment/coyote-kubernetes-worker --timeout="${GKE_DEPLOY_TIMEOUT_SECONDS:-300}s"
kubectl -n "$namespace" get deployment,pods -l app.kubernetes.io/name=coyote-kubernetes-worker -o wide
kubectl -n "$namespace" get deployment coyote-kubernetes-worker -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'