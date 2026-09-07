#!/usr/bin/env bash
set -euo pipefail

cluster_name="coyote-ci"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
kubeconfig_dir="/tmp/coyote-ci-kind"
kubeconfig_owner_path="${kubeconfig_dir}/.owner"
kubeconfig_path="${kubeconfig_dir}/kubeconfig"

remove_owned_kubeconfig() {
  local path="$1"
  if [[ ! -e "$path" ]]; then
    return
  fi
  if [[ ! -f "$path" ]]; then
    echo "preserving non-file at $path" >&2
    return
  fi
  if grep -Fq "kind-${cluster_name}" "$path"; then
    rm -f "$path"
  else
    echo "preserving non-kind kubeconfig at $path" >&2
  fi
}

if kind get clusters | grep -Fxq "$cluster_name"; then
  kind delete cluster --name "$cluster_name"
else
  echo "kind cluster $cluster_name does not exist"
fi

docker compose -f "$repo_root/docker-compose.yml" stop db migrate server
if [[ -d "$kubeconfig_dir" && -f "$kubeconfig_owner_path" ]] && grep -Fxq "coyote-ci-kind" "$kubeconfig_owner_path"; then
  remove_owned_kubeconfig "$kubeconfig_path"
  remove_owned_kubeconfig "${kubeconfig_path}.bak"
  rm -f "$kubeconfig_owner_path"
  rmdir "$kubeconfig_dir" 2>/dev/null || true
elif [[ -e "$kubeconfig_dir" ]]; then
  echo "preserving unrecognized harness directory $kubeconfig_dir" >&2
fi