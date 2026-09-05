#!/usr/bin/env bash
set -euo pipefail

cluster_name="coyote-ci"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
kubeconfig_path="/tmp/coyote-kind-kubeconfig"

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

require_command docker
require_command kind
require_command kubectl

if kind get clusters | grep -Fxq "$cluster_name"; then
  echo "kind cluster $cluster_name already exists"
else
  kind create cluster --name "$cluster_name" --config "$repo_root/dev/kind/cluster.yaml"
fi

rm -f "$kubeconfig_path"
kind export kubeconfig --name "$cluster_name" --kubeconfig "$kubeconfig_path"
sed -i.bak -e 's#https://127\.0\.0\.1:#https://host.docker.internal:#' -e 's#https://0\.0\.0\.0:#https://host.docker.internal:#' -e 's#^    certificate-authority-data:.*#    insecure-skip-tls-verify: true#' "$kubeconfig_path"
rm -f "$kubeconfig_path.bak"

AUTH_MODE=disabled docker compose -f "$repo_root/docker-compose.yml" up -d db migrate server
kubectl --context "kind-$cluster_name" apply -f "$repo_root/dev/kind/worker.yaml"
echo "kind environment is ready; run make kind-load, then make kind-smoke"