#!/usr/bin/env bash
set -euo pipefail

cluster_name="coyote-ci"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if kind get clusters | grep -Fxq "$cluster_name"; then
  kind delete cluster --name "$cluster_name"
else
  echo "kind cluster $cluster_name does not exist"
fi

docker compose -f "$repo_root/docker-compose.yml" stop db migrate server