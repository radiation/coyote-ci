#!/usr/bin/env bash
set -euo pipefail

namespace="${GKE_NAMESPACE:-coyote-ci}"
hostname="${GKE_TEMPORARY_HOSTNAME:-}"
address_name="${GKE_GATEWAY_ADDRESS_NAME:-}"
certificate_map="${GKE_GATEWAY_CERTIFICATE_MAP:-}"
timeout_seconds="${GKE_DEPLOY_TIMEOUT_SECONDS:-600}"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
render_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$render_dir"
}
trap cleanup EXIT

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

escape_sed() {
  printf '%s' "$1" | sed 's/[&|\\]/\\&/g'
}

require_command kubectl
require_command jq

[[ "$namespace" == "coyote-ci" ]] || { echo "GKE_NAMESPACE must be coyote-ci" >&2; exit 1; }
[[ -n "$hostname" && "$hostname" != *"://"* && "$hostname" != */* ]] || { echo "GKE_TEMPORARY_HOSTNAME must be a hostname" >&2; exit 1; }
[[ "$hostname" != "coyote-ci.bryanchoate.com" ]] || { echo "GKE_TEMPORARY_HOSTNAME must not be the production hostname" >&2; exit 1; }
[[ -n "$address_name" ]] || { echo "GKE_GATEWAY_ADDRESS_NAME must name a reserved global address" >&2; exit 1; }
[[ -n "$certificate_map" ]] || { echo "GKE_GATEWAY_CERTIFICATE_MAP must name a Certificate Manager certificate map" >&2; exit 1; }

rendered_manifest="$render_dir/temporary-gateway.yaml"
sed \
  -e "s|__TEMPORARY_HOSTNAME__|$(escape_sed "$hostname")|g" \
  -e "s|__GATEWAY_ADDRESS_NAME__|$(escape_sed "$address_name")|g" \
  -e "s|__GATEWAY_CERTIFICATE_MAP__|$(escape_sed "$certificate_map")|g" \
  "$repo_root/deploy/kubernetes/gke/temporary-gateway.yaml" > "$rendered_manifest"

kubectl apply --dry-run=server -f "$rendered_manifest"
kubectl apply -f "$rendered_manifest"

gateway_ready() {
  jq -e '
    def condition_true($type):
      any(.status.conditions[]?; .type == $type and .status == "True");
    condition_true("Accepted") and condition_true("Programmed")
  ' >/dev/null
}

http_route_ready() {
  jq -e '
    def condition_true($type):
      any(.conditions[]?; .type == $type and .status == "True");
    any(
      .status.parents[]?;
      condition_true("Accepted") and condition_true("ResolvedRefs")
    )
  ' >/dev/null
}

deadline=$(( $(date +%s) + timeout_seconds ))
while (( $(date +%s) < deadline )); do
  gateway="$(kubectl -n "$namespace" get gateway coyote-temporary-public -o json)"
  route="$(kubectl -n "$namespace" get httproute coyote-temporary-public -o json)"
  redirect_route="$(kubectl -n "$namespace" get httproute coyote-temporary-http-redirect -o json)"
  if gateway_ready <<<"$gateway" &&
    http_route_ready <<<"$route" &&
    http_route_ready <<<"$redirect_route"; then
    kubectl -n "$namespace" get gateway coyote-temporary-public -o wide
    kubectl -n "$namespace" get httproute coyote-temporary-public coyote-temporary-http-redirect -o wide
    exit 0
  fi
  sleep 5
done

echo "Gateway or HTTPRoute did not become ready within ${timeout_seconds}s" >&2
echo "Gateway conditions:" >&2
jq '.status.conditions // []' <<<"$gateway" >&2 || true
echo "HTTPRoute conditions:" >&2
jq '[.status.parents[]? | {parentRef, conditions: (.conditions // [])}]' <<<"$route" >&2 || true
echo "HTTP redirect HTTPRoute conditions:" >&2
jq '[.status.parents[]? | {parentRef, conditions: (.conditions // [])}]' <<<"$redirect_route" >&2 || true
kubectl -n "$namespace" describe gateway coyote-temporary-public >&2 || true
kubectl -n "$namespace" describe httproute coyote-temporary-public coyote-temporary-http-redirect >&2 || true
kubectl -n "$namespace" get events --sort-by=.lastTimestamp >&2 || true
exit 1
