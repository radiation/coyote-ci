#!/usr/bin/env bash
set -euo pipefail

namespace="${GKE_NAMESPACE:-coyote-ci}"
service_account="${GKE_VERIFIER_SERVICE_ACCOUNT:-coyote-workspace-verifier}"
token_secret="${GKE_VERIFIER_TOKEN_SECRET:-coyote-workspace-verifier-token}"
output_path="${GKE_VERIFIER_KUBECONFIG_PATH:-/opt/coyote-ci/gke-verifier-kubeconfig}"

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

require_command base64
require_command kubectl

if [[ "$namespace" != "coyote-ci" ]]; then
  echo "GKE_NAMESPACE must be coyote-ci because the verifier RBAC is namespace-specific" >&2
  exit 1
fi

kubectl -n "$namespace" get serviceaccount "$service_account" >/dev/null
kubectl -n "$namespace" get secret "$token_secret" >/dev/null

server="$(kubectl config view --minify --raw -o jsonpath='{.clusters[0].cluster.server}')"
certificate_authority_data="$(kubectl config view --minify --raw -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')"
token_data="$(kubectl -n "$namespace" get secret "$token_secret" -o jsonpath='{.data.token}')"

if [[ -z "$server" || -z "$certificate_authority_data" || -z "$token_data" ]]; then
  echo "current kubectl context must provide cluster server, CA data, and verifier token Secret data" >&2
  exit 1
fi

token="$(printf '%s' "$token_data" | base64 -D)"
if [[ -z "$token" ]]; then
  echo "verifier token Secret contains an empty token" >&2
  exit 1
fi

output_dir="$(dirname "$output_path")"
umask 077
mkdir -p "$output_dir"
cat >"$output_path" <<EOF
apiVersion: v1
kind: Config
clusters:
  - name: coyote-ci-autopilot
    cluster:
      server: ${server}
      certificate-authority-data: ${certificate_authority_data}
contexts:
  - name: coyote-workspace-verifier
    context:
      cluster: coyote-ci-autopilot
      namespace: ${namespace}
      user: coyote-workspace-verifier
current-context: coyote-workspace-verifier
users:
  - name: coyote-workspace-verifier
    user:
      token: ${token}
EOF
chmod 600 "$output_path"
unset token token_data

echo "wrote verifier kubeconfig to $output_path"