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

cat >"$temp_dir/invalid-mcp-image.yaml" <<'EOF'
mcp:
  enabled: true
images:
  mcp: example.invalid/coyote-mcp:mutable
serviceAccounts:
  mcp:
    name: coyote-mcp
    gsaEmail: coyote-gke-mcp@example-project.iam.gserviceaccount.com
EOF
expect_template_failure coyote-ci "$repo_root/deploy/helm/coyote-ci" coyote-ci "$control_values" "$temp_dir/invalid-mcp-image.yaml"

cat >"$temp_dir/invalid-mcp-auth.yaml" <<'EOF'
mcp:
  enabled: true
  auth:
    allowDisabledAuth: true
images:
  mcp: example.invalid/coyote-mcp@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
serviceAccounts:
  mcp:
    name: coyote-mcp
    gsaEmail: coyote-gke-mcp@example-project.iam.gserviceaccount.com
EOF
expect_template_failure coyote-ci "$repo_root/deploy/helm/coyote-ci" coyote-ci "$control_values" "$temp_dir/invalid-mcp-auth.yaml"

cat >"$temp_dir/disabled-auth-mcp.yaml" <<'EOF'
auth:
  mode: disabled
mcp:
  enabled: true
images:
  mcp: example.invalid/coyote-mcp@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
serviceAccounts:
  mcp:
    name: coyote-mcp
    gsaEmail: coyote-gke-mcp@example-project.iam.gserviceaccount.com
EOF
expect_template_failure coyote-ci "$repo_root/deploy/helm/coyote-ci" coyote-ci "$control_values" "$temp_dir/disabled-auth-mcp.yaml"

cat >"$temp_dir/mcp-enabled.yaml" <<'EOF'
mcp:
  enabled: true
  replicas: 2
  auth:
    allowedOrigins: ["https://mcp.example.invalid"]
images:
  mcp: example.invalid/coyote-mcp@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
serviceAccounts:
  mcp:
    name: coyote-mcp
    gsaEmail: coyote-gke-mcp@example-project.iam.gserviceaccount.com
EOF
helm template coyote-ci "$repo_root/deploy/helm/coyote-ci" --namespace coyote-ci --values "$control_values" --values "$temp_dir/mcp-enabled.yaml" >"$temp_dir/mcp-enabled-render.yaml"
ruby -ryaml - "$temp_dir/mcp-enabled-render.yaml" <<'RUBY'
documents = YAML.load_stream(File.read(ARGV.fetch(0))).compact
index = documents.each_with_object({}) { |document, result| result[[document.fetch("kind"), document.dig("metadata", "name")]] = document }
deployment = index.fetch(["Deployment", "coyote-mcp"])
service = index.fetch(["Service", "coyote-mcp"])
account = index.fetch(["ServiceAccount", "coyote-mcp"])
secrets = index.fetch(["SecretProviderClass", "coyote-mcp-secrets"])
abort "MCP replicas did not render" unless deployment.dig("spec", "replicas") == 2
abort "MCP image is not digest-pinned" unless deployment.dig("spec", "template", "spec", "containers", 0, "image")&.include?("@sha256:")
abort "MCP service must be ClusterIP without session affinity" unless service.dig("spec", "type") == "ClusterIP" && service.dig("spec", "sessionAffinity") == "None"
abort "MCP probes are missing" unless deployment.dig("spec", "template", "spec", "containers", 0, "livenessProbe", "httpGet", "path") == "/healthz" && deployment.dig("spec", "template", "spec", "containers", 0, "readinessProbe", "httpGet", "path") == "/readyz"
abort "MCP pod must mount its service-account token for Workload Identity" unless deployment.dig("spec", "template", "spec", "automountServiceAccountToken") == true
abort "MCP security context is incomplete" unless deployment.dig("spec", "template", "spec", "containers", 0, "securityContext", "readOnlyRootFilesystem") == true
abort "MCP service account was not rendered" unless account.dig("metadata", "annotations", "iam.gke.io/gcp-service-account")
abort "MCP secret provider must only mount the database URL" unless YAML.load(secrets.dig("spec", "parameters", "secrets")).map { |item| item["path"] } == ["database-url"]
abort "MCP must not render Kubernetes RBAC" if documents.any? { |document| ["Role", "RoleBinding", "ClusterRole", "ClusterRoleBinding"].include?(document["kind"]) && document.dig("metadata", "name")&.include?("mcp") }
RUBY

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
