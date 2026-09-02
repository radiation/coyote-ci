#!/usr/bin/env bash
set -euo pipefail

cluster_name="coyote-ci"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if ! kind get clusters | grep -Fxq "$cluster_name"; then
  echo "kind cluster $cluster_name does not exist; run make kind-up first" >&2
  exit 1
fi

docker build --tag coyote-ci-worker:kind --file "$repo_root/backend/Dockerfile" "$repo_root/backend"
kind load docker-image --name "$cluster_name" coyote-ci-worker:kind
kubectl --context "kind-$cluster_name" -n coyote-ci rollout restart deployment/coyote-kubernetes-worker
kubectl --context "kind-$cluster_name" -n coyote-ci rollout status deployment/coyote-kubernetes-worker --timeout=180s