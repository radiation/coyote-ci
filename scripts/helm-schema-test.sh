#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$temp_dir"
}
trap cleanup EXIT

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

expect_template_failure() {
  local name="$1"
  local chart="$2"
  local namespace="$3"
  local values="$4"
  local override="$5"

  if helm template "$name" "$chart" --namespace "$namespace" --values "$values" --values "$override" >/dev/null 2>&1; then
    echo "expected Helm template validation failure for $name" >&2
    exit 1
  fi
}

require_command helm

control_values="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml"
worker_values="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml"

cat >"$temp_dir/missing-namespace.yaml" <<'EOF'
namespace: ""
EOF
expect_template_failure coyote-ci "$repo_root/deploy/helm/coyote-ci" coyote-ci "$control_values" "$temp_dir/missing-namespace.yaml"

cat >"$temp_dir/invalid-image.yaml" <<'EOF'
images:
  server: example.invalid/coyote-server:mutable
EOF
expect_template_failure coyote-ci "$repo_root/deploy/helm/coyote-ci" coyote-ci "$control_values" "$temp_dir/invalid-image.yaml"

cat >"$temp_dir/incomplete-oidc.yaml" <<'EOF'
auth:
  oidc:
    issuerURL: ""
EOF
expect_template_failure coyote-ci "$repo_root/deploy/helm/coyote-ci" coyote-ci "$control_values" "$temp_dir/incomplete-oidc.yaml"

cat >"$temp_dir/incomplete-gateway.yaml" <<'EOF'
gateway:
  enabled: true
  addressName: ""
EOF
expect_template_failure coyote-ci "$repo_root/deploy/helm/coyote-ci" coyote-ci "$control_values" "$temp_dir/incomplete-gateway.yaml"

cat >"$temp_dir/invalid-concurrency.yaml" <<'EOF'
worker:
  maxInFlightJobs: 0
EOF
expect_template_failure coyote-ci-worker "$repo_root/deploy/helm/coyote-ci-worker" coyote-ci-staging "$worker_values" "$temp_dir/invalid-concurrency.yaml"

cat >"$temp_dir/release-overlay.yaml" <<'EOF'
release:
  version: 2.5.1
  channel: stable
  manifestDigest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
images:
  server: example.invalid/coyote-server@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  frontend: example.invalid/coyote-frontend@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
  worker: example.invalid/coyote-worker@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
  migrate: example.invalid/coyote-migrate@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
EOF
helm template coyote-ci "$repo_root/deploy/helm/coyote-ci" --namespace coyote-ci --values "$control_values" --values "$temp_dir/release-overlay.yaml" >/dev/null
helm template coyote-ci-worker "$repo_root/deploy/helm/coyote-ci-worker" --namespace coyote-ci-staging --values "$worker_values" --values "$temp_dir/release-overlay.yaml" >/dev/null

echo "Helm schema-negative tests passed"
