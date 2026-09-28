#!/usr/bin/env bash
set -euo pipefail

namespace="${GKE_STAGING_NAMESPACE:-coyote-ci-staging}"
gcp_project="${GCP_PROJECT:-bryanchoate}"
google_cloud_project="${GOOGLE_CLOUD_PROJECT:-$gcp_project}"
cloud_sql_instance="${CLOUD_SQL_INSTANCE:-${gcp_project}:us-central1:bryanchoate-postgres}"
worker_gsa="${COYOTE_STAGING_WORKER_GSA_EMAIL:-}"
worker_image="${COYOTE_STAGING_WORKER_IMAGE:-}"
database_url_secret="${COYOTE_STAGING_DATABASE_URL_SECRET:-coyote-staging-database-url}"
artifact_bucket="${ARTIFACT_GCS_BUCKET:-}"
artifact_prefix="${ARTIFACT_GCS_PREFIX:-builds}"
cloud_build_location="${CLOUD_BUILD_LOCATION:-}"
cloud_build_runtime_service_account="${CLOUD_BUILD_RUNTIME_SERVICE_ACCOUNT:-}"
cloud_build_artifact_registry_repository="${CLOUD_BUILD_ARTIFACT_REGISTRY_REPOSITORY:-}"
cloud_build_source_bucket="${CLOUD_BUILD_SOURCE_BUCKET:-}"
cloud_build_source_prefix="${CLOUD_BUILD_SOURCE_PREFIX:-coyote-sources}"
max_in_flight_jobs="${WORKER_KUBERNETES_MAX_IN_FLIGHT_JOBS:-1}"
timeout_seconds="${GKE_DEPLOY_TIMEOUT_SECONDS:-300}"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
render_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$render_dir"
}
trap cleanup EXIT

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

require_digest_image() {
  [[ "$2" == *@sha256:* ]] || { echo "$1 must be an image@sha256 digest reference" >&2; exit 1; }
}

escape_sed() {
  printf '%s' "$1" | sed 's/[&|\\]/\\&/g'
}

require_command kubectl

[[ "$namespace" == "coyote-ci-staging" ]] || { echo "GKE_STAGING_NAMESPACE must be coyote-ci-staging" >&2; exit 1; }
[[ -n "$worker_gsa" ]] || { echo "COYOTE_STAGING_WORKER_GSA_EMAIL must not be empty" >&2; exit 1; }
[[ -n "$database_url_secret" ]] || { echo "COYOTE_STAGING_DATABASE_URL_SECRET must not be empty" >&2; exit 1; }
[[ -n "$artifact_bucket" ]] || { echo "ARTIFACT_GCS_BUCKET must not be empty" >&2; exit 1; }
[[ -n "$google_cloud_project" ]] || { echo "GOOGLE_CLOUD_PROJECT must not be empty for Cloud Build image execution" >&2; exit 1; }
[[ -n "$cloud_build_location" ]] || { echo "CLOUD_BUILD_LOCATION must not be empty for Cloud Build image execution" >&2; exit 1; }
[[ -n "$cloud_build_runtime_service_account" ]] || { echo "CLOUD_BUILD_RUNTIME_SERVICE_ACCOUNT must not be empty for Cloud Build image execution" >&2; exit 1; }
[[ -n "$cloud_build_artifact_registry_repository" ]] || { echo "CLOUD_BUILD_ARTIFACT_REGISTRY_REPOSITORY must not be empty for Cloud Build image execution" >&2; exit 1; }
[[ -n "$cloud_build_source_bucket" ]] || { echo "CLOUD_BUILD_SOURCE_BUCKET must not be empty for Cloud Build image execution" >&2; exit 1; }
require_digest_image COYOTE_STAGING_WORKER_IMAGE "$worker_image"

rendered_manifest="$render_dir/staging-worker.yaml"
sed \
  -e "s|__GCP_PROJECT__|$(escape_sed "$gcp_project")|g" \
  -e "s|__GOOGLE_CLOUD_PROJECT__|$(escape_sed "$google_cloud_project")|g" \
  -e "s|__CLOUD_SQL_INSTANCE__|$(escape_sed "$cloud_sql_instance")|g" \
  -e "s|__COYOTE_STAGING_WORKER_GSA__|$(escape_sed "$worker_gsa")|g" \
  -e "s|__COYOTE_STAGING_WORKER_IMAGE__|$(escape_sed "$worker_image")|g" \
  -e "s|__DATABASE_URL_SECRET__|$(escape_sed "$database_url_secret")|g" \
  -e "s|__ARTIFACT_GCS_BUCKET__|$(escape_sed "$artifact_bucket")|g" \
  -e "s|__ARTIFACT_GCS_PREFIX__|$(escape_sed "$artifact_prefix")|g" \
  -e "s|__CLOUD_BUILD_LOCATION__|$(escape_sed "$cloud_build_location")|g" \
  -e "s|__CLOUD_BUILD_RUNTIME_SERVICE_ACCOUNT__|$(escape_sed "$cloud_build_runtime_service_account")|g" \
  -e "s|__CLOUD_BUILD_ARTIFACT_REGISTRY_REPOSITORY__|$(escape_sed "$cloud_build_artifact_registry_repository")|g" \
  -e "s|__CLOUD_BUILD_SOURCE_BUCKET__|$(escape_sed "$cloud_build_source_bucket")|g" \
  -e "s|__CLOUD_BUILD_SOURCE_PREFIX__|$(escape_sed "$cloud_build_source_prefix")|g" \
  -e "s|__WORKER_KUBERNETES_MAX_IN_FLIGHT_JOBS__|$(escape_sed "$max_in_flight_jobs")|g" \
  "$repo_root/deploy/kubernetes/gke/staging-worker.yaml" > "$rendered_manifest"

if ! kubectl get namespace "$namespace" >/dev/null 2>&1; then
  kubectl create namespace "$namespace" --dry-run=server -o yaml >/dev/null
  kubectl create namespace "$namespace"
fi
kubectl apply --dry-run=server -f "$rendered_manifest"
kubectl apply -f "$rendered_manifest"
if ! kubectl -n "$namespace" rollout status deployment/coyote-kubernetes-worker-staging --timeout="${timeout_seconds}s"; then
  kubectl -n "$namespace" describe deployment/coyote-kubernetes-worker-staging >&2 || true
  kubectl -n "$namespace" get pods -o wide >&2 || true
  kubectl -n "$namespace" logs deployment/coyote-kubernetes-worker-staging --all-containers=true --tail=200 >&2 || true
  exit 1
fi

live_image="$(kubectl -n "$namespace" get deployment coyote-kubernetes-worker-staging -o jsonpath='{.spec.template.spec.containers[?(@.name=="worker")].image}')"
[[ "$live_image" == *@sha256:* ]] || { echo "staging worker is not digest-pinned: $live_image" >&2; exit 1; }
[[ "$live_image" == "$worker_image" ]] || { echo "staging worker image differs from requested image: $live_image" >&2; exit 1; }
kubectl -n "$namespace" get deployment,pods -l app.kubernetes.io/name=coyote-kubernetes-worker-staging -o wide
