#!/usr/bin/env bash
set -euo pipefail

namespace="${GKE_NAMESPACE:-coyote-ci}"
gcp_project="${GCP_PROJECT:-bryanchoate}"
cloud_sql_instance="${CLOUD_SQL_INSTANCE:-${gcp_project}:us-central1:bryanchoate-postgres}"
server_gsa="${COYOTE_SERVER_GSA_EMAIL:-}"
migrate_gsa="${COYOTE_MIGRATE_GSA_EMAIL:-}"
server_image="${COYOTE_SERVER_IMAGE:-}"
frontend_image="${COYOTE_FRONTEND_IMAGE:-}"
migrate_image="${COYOTE_MIGRATE_IMAGE:-}"
database_url_secret="${COYOTE_STAGING_DATABASE_URL_SECRET:-coyote-staging-database-url}"
capability_secret="${COYOTE_WORKSPACE_HELPER_CAPABILITY_SECRET_NAME:-coyote-workspace-helper-capability-secret}"
artifact_bucket="${ARTIFACT_GCS_BUCKET:-}"
artifact_prefix="${ARTIFACT_GCS_PREFIX:-builds}"
cache_bucket="${WORKER_CACHE_GCS_BUCKET:-}"
cache_prefix="${WORKER_CACHE_GCS_PREFIX:-coyote-ci/cache}"
revision_bucket="${WORKSPACE_REVISION_GCS_BUCKET:-}"
revision_prefix="${WORKSPACE_REVISION_GCS_PREFIX:-workspace-revisions}"
auth_mode="${CONTROL_PLANE_AUTH_MODE:-}"
bootstrap_admin_emails="${CONTROL_PLANE_BOOTSTRAP_ADMIN_EMAILS:-}"
oidc_issuer_url="${CONTROL_PLANE_OIDC_ISSUER_URL:-}"
oidc_client_id="${CONTROL_PLANE_OIDC_CLIENT_ID:-}"
oidc_client_secret_name="${CONTROL_PLANE_OIDC_CLIENT_SECRET_NAME:-coyote-staging-oidc-client-secret}"
oidc_redirect_url="${CONTROL_PLANE_OIDC_REDIRECT_URL:-}"
public_url="${CONTROL_PLANE_PUBLIC_URL:-}"
oidc_scopes="${CONTROL_PLANE_OIDC_SCOPES:-openid email profile}"
session_secret_name="${CONTROL_PLANE_SESSION_SECRET_NAME:-coyote-staging-session-secret}"
timeout_seconds="${GKE_DEPLOY_TIMEOUT_SECONDS:-300}"
server_replicas="${CONTROL_PLANE_SERVER_REPLICAS:-1}"
frontend_replicas="${CONTROL_PLANE_FRONTEND_REPLICAS:-1}"
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
  local name="$1"
  local image="$2"
  [[ "$image" == *@sha256:* ]] || { echo "$name must be an image@sha256 digest reference" >&2; exit 1; }
}

require_replica_count() {
  local name="$1"
  local replicas="$2"
  [[ "$replicas" =~ ^[0-9]+$ ]] || { echo "$name must be a non-negative integer" >&2; exit 1; }
}

scale_deployment() {
  local deployment="$1"
  local replicas="$2"
  kubectl -n "$namespace" patch "deployment/$deployment" --subresource=scale --type merge --field-manager=coyote-raw-rollout --patch "{\"spec\":{\"replicas\":$replicas}}"
}

escape_sed() {
  printf '%s' "$1" | sed 's/[&|\\]/\\&/g'
}

render_manifest() {
  local source="$1"
  local destination="$2"
  sed \
    -e "s|__GCP_PROJECT__|$(escape_sed "$gcp_project")|g" \
    -e "s|__COYOTE_SERVER_GSA__|$(escape_sed "$server_gsa")|g" \
    -e "s|__COYOTE_MIGRATE_GSA__|$(escape_sed "$migrate_gsa")|g" \
    -e "s|__DATABASE_URL_SECRET__|$(escape_sed "$database_url_secret")|g" \
    -e "s|__WORKSPACE_HELPER_CAPABILITY_SECRET__|$(escape_sed "$capability_secret")|g" \
    -e "s|__CLOUD_SQL_INSTANCE__|$(escape_sed "$cloud_sql_instance")|g" \
    -e "s|__COYOTE_SERVER_IMAGE__|$(escape_sed "$server_image")|g" \
    -e "s|__COYOTE_FRONTEND_IMAGE__|$(escape_sed "$frontend_image")|g" \
    -e "s|__COYOTE_MIGRATE_IMAGE__|$(escape_sed "$migrate_image")|g" \
    -e "s|__ARTIFACT_GCS_BUCKET__|$(escape_sed "$artifact_bucket")|g" \
    -e "s|__ARTIFACT_GCS_PREFIX__|$(escape_sed "$artifact_prefix")|g" \
    -e "s|__CACHE_GCS_BUCKET__|$(escape_sed "$cache_bucket")|g" \
    -e "s|__CACHE_GCS_PREFIX__|$(escape_sed "$cache_prefix")|g" \
    -e "s|__WORKSPACE_REVISION_GCS_BUCKET__|$(escape_sed "$revision_bucket")|g" \
    -e "s|__WORKSPACE_REVISION_GCS_PREFIX__|$(escape_sed "$revision_prefix")|g" \
    -e "s|__AUTH_MODE__|$(escape_sed "$auth_mode")|g" \
    -e "s|__BOOTSTRAP_ADMIN_EMAILS__|$(escape_sed "$bootstrap_admin_emails")|g" \
    -e "s|__OIDC_ISSUER_URL__|$(escape_sed "$oidc_issuer_url")|g" \
    -e "s|__OIDC_CLIENT_ID__|$(escape_sed "$oidc_client_id")|g" \
    -e "s|__OIDC_CLIENT_SECRET__|$(escape_sed "$oidc_client_secret_name")|g" \
    -e "s|__OIDC_REDIRECT_URL__|$(escape_sed "$oidc_redirect_url")|g" \
    -e "s|__COYOTE_PUBLIC_URL__|$(escape_sed "$public_url")|g" \
    -e "s|__OIDC_SCOPES__|$(escape_sed "$oidc_scopes")|g" \
    -e "s|__SESSION_SECRET__|$(escape_sed "$session_secret_name")|g" \
    "$source" > "$destination"
}

require_command kubectl

[[ "$namespace" == "coyote-ci" ]] || { echo "GKE_NAMESPACE must be coyote-ci" >&2; exit 1; }
existing_manager="$(kubectl -n "$namespace" get deployment coyote-server -o jsonpath='{.metadata.labels.app\.kubernetes\.io/managed-by}' 2>/dev/null || true)"
[[ "$existing_manager" != "Helm" ]] || {
  echo "coyote-server is Helm-managed; use scripts/gke-helm-rollout.sh instead of the raw deployment path" >&2
  exit 1
}
[[ "$auth_mode" == "oidc" ]] || { echo "CONTROL_PLANE_AUTH_MODE must be oidc; disabled and header modes are not safe for this deployment" >&2; exit 1; }
for setting in server_gsa migrate_gsa artifact_bucket cache_bucket revision_bucket; do
  [[ -n "${!setting}" ]] || { echo "$setting must not be empty" >&2; exit 1; }
done
for setting in bootstrap_admin_emails oidc_issuer_url oidc_client_id oidc_client_secret_name oidc_redirect_url public_url session_secret_name; do
  [[ -n "${!setting}" ]] || { echo "$setting must not be empty for OIDC control-plane deployment" >&2; exit 1; }
done
[[ "$public_url" == "https://"* ]] || { echo "CONTROL_PLANE_PUBLIC_URL must use https" >&2; exit 1; }
[[ "$oidc_redirect_url" == "${public_url%/}/auth/callback" ]] || {
  echo "CONTROL_PLANE_OIDC_REDIRECT_URL must equal CONTROL_PLANE_PUBLIC_URL/auth/callback" >&2
  exit 1
}
require_digest_image COYOTE_SERVER_IMAGE "$server_image"
require_digest_image COYOTE_FRONTEND_IMAGE "$frontend_image"
require_digest_image COYOTE_MIGRATE_IMAGE "$migrate_image"
require_replica_count CONTROL_PLANE_SERVER_REPLICAS "$server_replicas"
require_replica_count CONTROL_PLANE_FRONTEND_REPLICAS "$frontend_replicas"

render_manifest "$repo_root/deploy/kubernetes/gke/control-plane.yaml" "$render_dir/control-plane.yaml"
render_manifest "$repo_root/deploy/kubernetes/gke/control-plane-migration.yaml" "$render_dir/migration.yaml"

kubectl apply --dry-run=server -f "$render_dir/control-plane.yaml"
kubectl apply -f "$render_dir/control-plane.yaml"
scale_deployment coyote-server 0
scale_deployment coyote-frontend 0

kubectl -n "$namespace" delete job/coyote-migrate --ignore-not-found --wait=true
kubectl apply --dry-run=server -f "$render_dir/migration.yaml"
kubectl apply -f "$render_dir/migration.yaml"
if ! kubectl -n "$namespace" wait --for=condition=complete job/coyote-migrate --timeout="${timeout_seconds}s"; then
  kubectl -n "$namespace" describe job/coyote-migrate >&2 || true
  kubectl -n "$namespace" logs job/coyote-migrate --all-containers=true >&2 || true
  exit 1
fi

kubectl apply --dry-run=server -f "$render_dir/control-plane.yaml"
kubectl apply -f "$render_dir/control-plane.yaml"
scale_deployment coyote-server "$server_replicas"
scale_deployment coyote-frontend "$frontend_replicas"
kubectl -n "$namespace" rollout status deployment/coyote-server --timeout="${timeout_seconds}s"
kubectl -n "$namespace" rollout status deployment/coyote-frontend --timeout="${timeout_seconds}s"

for deployment in coyote-server coyote-frontend; do
  image="$(kubectl -n "$namespace" get deployment "$deployment" -o jsonpath='{.spec.template.spec.containers[0].image}')"
  [[ "$image" == *@sha256:* ]] || { echo "$deployment is not digest-pinned: $image" >&2; exit 1; }
  printf '%s=%s\n' "$deployment" "$image"
done
