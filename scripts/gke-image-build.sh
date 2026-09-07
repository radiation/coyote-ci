#!/usr/bin/env bash
set -euo pipefail

image="${GKE_WORKER_IMAGE:-us-central1-docker.pkg.dev/bryanchoate/coyote-ci/coyote-worker:gke-smoke}"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

require_command docker

docker buildx build \
  --platform linux/amd64,linux/arm64 \
  --tag "$image" \
  --file "$repo_root/backend/Dockerfile" \
  --push \
  "$repo_root/backend"

docker buildx imagetools inspect "$image"