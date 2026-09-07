#!/usr/bin/env bash
set -euo pipefail

cluster_name="coyote-ci"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
kubeconfig_dir="/tmp/coyote-ci-kind"
kubeconfig_owner_path="${kubeconfig_dir}/.owner"
kubeconfig_path="${kubeconfig_dir}/kubeconfig"
kubeconfig_backup_path="${kubeconfig_path}.bak"

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

require_command docker
require_command kind
require_command kubectl

ensure_kubeconfig_dir() {
  if [[ -e "$kubeconfig_dir" && ! -d "$kubeconfig_dir" ]]; then
    echo "expected harness directory at $kubeconfig_dir, but found a non-directory; move or remove it before running make kind-up" >&2
    exit 1
  fi
  if [[ -d "$kubeconfig_dir" && ! -e "$kubeconfig_owner_path" ]] && [[ -n "$(find "$kubeconfig_dir" -mindepth 1 -maxdepth 1 -print -quit)" ]]; then
    echo "refusing to use unrecognized non-empty harness directory $kubeconfig_dir; move or remove it before running make kind-up" >&2
    exit 1
  fi
  if [[ -d "$kubeconfig_dir" && -e "$kubeconfig_owner_path" ]] && ! grep -Fxq "coyote-ci-kind" "$kubeconfig_owner_path"; then
    echo "refusing to use unrecognized harness directory $kubeconfig_dir; move or remove it before running make kind-up" >&2
    exit 1
  fi
  mkdir -p "$kubeconfig_dir"
  printf '%s\n' "coyote-ci-kind" >"$kubeconfig_owner_path"
}

remove_owned_kubeconfig() {
  local path="$1"
  if [[ ! -e "$path" ]]; then
    return
  fi
  if [[ ! -f "$path" ]]; then
    echo "expected generated kubeconfig file at $path, but found a non-file; move or remove it before running make kind-up" >&2
    exit 1
  fi
  if ! grep -Fq "kind-${cluster_name}" "$path"; then
    echo "refusing to replace non-kind kubeconfig at $path; move or remove it before running make kind-up" >&2
    exit 1
  fi
  rm -f "$path"
}

if kind get clusters | grep -Fxq "$cluster_name"; then
  echo "kind cluster $cluster_name already exists"
else
  kind create cluster --name "$cluster_name" --config "$repo_root/dev/kind/cluster.yaml"
fi

ensure_kubeconfig_dir
remove_owned_kubeconfig "$kubeconfig_path"
remove_owned_kubeconfig "$kubeconfig_backup_path"
kind export kubeconfig --name "$cluster_name" --kubeconfig "$kubeconfig_path"
sed -i.bak -e 's#https://127\.0\.0\.1:#https://host.docker.internal:#' -e 's#https://0\.0\.0\.0:#https://host.docker.internal:#' -e 's#^    certificate-authority-data:.*#    insecure-skip-tls-verify: true#' "$kubeconfig_path"
remove_owned_kubeconfig "$kubeconfig_backup_path"

AUTH_MODE=disabled COYOTE_WORKSPACE_HELPER_ENABLED=true docker compose -f "$repo_root/docker-compose.yml" up -d db migrate server
kubectl --context "kind-$cluster_name" apply -f "$repo_root/dev/kind/worker.yaml"
echo "kind environment is ready; run make kind-load, then make kind-smoke"