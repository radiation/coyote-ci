#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
registry="${ARTIFACT_REGISTRY_REPOSITORY:-us-central1-docker.pkg.dev/bryanchoate/coyote-ci}"
tag="${GKE_CONTROL_PLANE_TAG:-$(git -C "$repo_root" rev-parse --short HEAD)}"
server_image="${COYOTE_SERVER_IMAGE:-$registry/coyote-server:$tag}"
frontend_image="${COYOTE_FRONTEND_IMAGE:-$registry/coyote-frontend:$tag}"
migrate_image="${COYOTE_MIGRATE_IMAGE:-$registry/coyote-migrate:$tag}"

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

resolve_digest() {
  local image="$1"
  local digest
  digest="$(docker buildx imagetools inspect --format '{{.Manifest.Digest}}' "$image")"
  [[ "$digest" == sha256:* ]] || { echo "could not resolve an immutable digest for $image" >&2; exit 1; }
  printf '%s@%s\n' "$image" "$digest"
}

require_command docker

docker buildx build --platform linux/amd64,linux/arm64 --push \
  --tag "$server_image" --file "$repo_root/backend/Dockerfile" --target source-runtime \
  "$repo_root/backend"
docker buildx build --platform linux/amd64,linux/arm64 --push \
  --tag "$migrate_image" --file "$repo_root/backend/Dockerfile" --target migrate-runtime \
  "$repo_root/backend"
docker buildx build --platform linux/amd64,linux/arm64 --push \
  --tag "$frontend_image" --file "$repo_root/frontend/Dockerfile" \
  "$repo_root/frontend"

printf 'COYOTE_SERVER_IMAGE=%s\n' "$(resolve_digest "$server_image")"
printf 'COYOTE_MIGRATE_IMAGE=%s\n' "$(resolve_digest "$migrate_image")"
printf 'COYOTE_FRONTEND_IMAGE=%s\n' "$(resolve_digest "$frontend_image")"
