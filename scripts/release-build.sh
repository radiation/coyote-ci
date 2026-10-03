#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="${VERSION:-}"
registry="${RELEASE_REGISTRY:-}"
release_source="${RELEASE_SOURCE:-}"
release_channel="${RELEASE_CHANNEL:-latest}"
commit="$(git -C "$repo_root" rev-parse HEAD)"
build_date="${RELEASE_BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
build_tag="build-${commit:0:12}-$(date -u +%Y%m%d%H%M%S)-$$-$RANDOM"

[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || { echo "VERSION must be an exact semantic version without build metadata" >&2; exit 2; }
[[ -n "$registry" && -n "$release_source" ]] || { echo "RELEASE_REGISTRY and RELEASE_SOURCE are required" >&2; exit 2; }
[[ -z "${RELEASE_COMMIT:-}" || "$RELEASE_COMMIT" == "$commit" ]] || { echo "RELEASE_COMMIT must match the checked-out HEAD" >&2; exit 2; }
[[ -z "$(git -C "$repo_root" status --porcelain --untracked-files=all)" ]] || { echo "release builds require a clean worktree, including untracked files" >&2; exit 2; }
command -v docker >/dev/null || { echo "docker is required" >&2; exit 1; }

build_image() {
  local component="$1"
  local target="$2"
  local repository="$registry/coyote-$component"
  local image="$repository:$build_tag"
  docker buildx build --platform linux/amd64,linux/arm64 --push --tag "$image" --file "$repo_root/${3}" --target "$target" \
    --build-arg "COYOTE_VERSION=$version" --build-arg "COYOTE_COMMIT=$commit" --build-arg "COYOTE_BUILD_DATE=$build_date" "$repo_root/${4}"
  local digest
  digest="$(docker buildx imagetools inspect --format '{{.Manifest.Digest}}' "$image")"
  [[ "$digest" == sha256:* ]] || { echo "could not resolve $component digest" >&2; exit 1; }
  printf '%s@%s' "$repository" "$digest"
}

promote_image() {
  local component="$1"
  local reference="$2"
  docker buildx imagetools create --tag "$registry/coyote-$component:$version" "$reference"
}

server="$(build_image server source-runtime backend/Dockerfile backend)"
worker="$(build_image worker source-runtime backend/Dockerfile backend)"
migrate="$(build_image migrate migrate-runtime backend/Dockerfile backend)"
frontend_repository="$registry/coyote-frontend"
frontend_image="$frontend_repository:$build_tag"
docker buildx build --platform linux/amd64,linux/arm64 --push --tag "$frontend_image" --file "$repo_root/frontend/Dockerfile" "$repo_root/frontend"
frontend="$frontend_repository@$(docker buildx imagetools inspect --format '{{.Manifest.Digest}}' "$frontend_image")"

manifest="$(mktemp)"
trap 'rm -f "$manifest"' EXIT
printf '{"schema_version":1,"release":"%s","source":{"commit":"%s","created_at":"%s"},"images":{"server":"%s","frontend":"%s","worker":"%s","migrate":"%s"}}\n' "$version" "$commit" "$build_date" "$server" "$frontend" "$worker" "$migrate" >"$manifest"
(
  cd "$repo_root/backend"
  go run ./cmd/coyote release publish --manifest "$manifest" --release-source "$release_source" --channel "$release_channel"
)
promote_image server "$server"
promote_image frontend "$frontend"
promote_image worker "$worker"
promote_image migrate "$migrate"
