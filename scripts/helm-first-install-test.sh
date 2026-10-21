#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT

command -v helm >/dev/null 2>&1 || { echo "helm is required" >&2; exit 1; }
command -v ruby >/dev/null 2>&1 || { echo "ruby is required" >&2; exit 1; }

control_chart="$repo_root/deploy/helm/coyote-ci"
worker_chart="$repo_root/deploy/helm/coyote-ci-worker"
control_values="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml"
worker_values="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml"

cat >"$temp_dir/fresh-install-values.yaml" <<'EOF'
namespace: install-control
executionNamespace: install-execution
rbac:
  tokenReviewNameOverride: ""
server:
  replicasManagedExternally: false
  replicas: 2
frontend:
  replicasManagedExternally: false
  replicas: 3
migration:
  render: false
mcp:
  enabled: true
  replicasManagedExternally: false
  replicas: 4
  auth:
    allowedOrigins: ["https://mcp.example.invalid"]
images:
  mcp: example.invalid/coyote-mcp@sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
serviceAccounts:
  mcp:
    name: coyote-mcp
    gsaEmail: coyote-gke-mcp@example-project.iam.gserviceaccount.com
EOF

cat >"$temp_dir/phase-one-overrides.yaml" <<'EOF'
server:
  replicas: 0
frontend:
  replicas: 0
migration:
  render: true
  runID: first-install-001
mcp:
  replicas: 0
EOF

helm template install-one "$control_chart" --namespace install-control \
  --values "$control_values" --values "$temp_dir/fresh-install-values.yaml" \
  --values "$temp_dir/phase-one-overrides.yaml" >"$temp_dir/phase-one-render.yaml"
helm template install-one "$control_chart" --namespace install-control \
  --values "$control_values" --values "$temp_dir/fresh-install-values.yaml" >"$temp_dir/phase-two-render.yaml"
helm template install-one "$worker_chart" --namespace install-execution \
  --values "$worker_values" \
  --set namespace=install-execution \
  --set controlPlaneNamespace=install-control >"$temp_dir/worker-render.yaml"
helm template install-two "$control_chart" --namespace install-control \
  --values "$control_values" --values "$temp_dir/fresh-install-values.yaml" >"$temp_dir/install-two-render.yaml"
helm template install-one "$control_chart" --namespace another-control \
  --values "$control_values" --values "$temp_dir/fresh-install-values.yaml" \
  --set namespace=another-control --set executionNamespace=another-execution >"$temp_dir/another-namespace-render.yaml"
helm template coyote-ci "$control_chart" --namespace coyote-ci \
  --values "$control_values" >"$temp_dir/legacy-render.yaml"

long_release_prefix="install-$(printf 'a%.0s' {1..44})"
long_release_one="${long_release_prefix}a"
long_release_two="${long_release_prefix}b"
long_namespace_prefix="namespace-$(printf 'n%.0s' {1..52})"
long_namespace_one="${long_namespace_prefix}a"
long_namespace_two="${long_namespace_prefix}b"
helm template "$long_release_one" "$control_chart" --namespace "$long_namespace_one" \
  --values "$control_values" --values "$temp_dir/fresh-install-values.yaml" \
  --set namespace="$long_namespace_one" --set executionNamespace=long-execution-one >"$temp_dir/long-one-render.yaml"
helm template "$long_release_one" "$control_chart" --namespace "$long_namespace_one" \
  --values "$control_values" --values "$temp_dir/fresh-install-values.yaml" \
  --set namespace="$long_namespace_one" --set executionNamespace=long-execution-one >"$temp_dir/long-one-repeat-render.yaml"
helm template "$long_release_two" "$control_chart" --namespace "$long_namespace_two" \
  --values "$control_values" --values "$temp_dir/fresh-install-values.yaml" \
  --set namespace="$long_namespace_two" --set executionNamespace=long-execution-two >"$temp_dir/long-two-render.yaml"

if helm template install-one "$control_chart" --namespace install-control \
  --values "$control_values" --set images.server=example.invalid/coyote-server:mutable >/dev/null 2>&1; then
  echo "mutable server image must be rejected" >&2
  exit 1
fi

ruby - "$temp_dir" <<'RUBY'
require "yaml"

root = ARGV.fetch(0)

def documents(root, name)
  YAML.load_stream(File.read(File.join(root, name))).compact
end

def index(documents)
  documents.each_with_object({}) do |document, result|
    result[[document.fetch("kind"), document.dig("metadata", "name")]] = document
  end
end

def container(document, name)
  document.dig("spec", "template", "spec", "containers").find { |entry| entry["name"] == name }
end

phase_one = index(documents(root, "phase-one-render.yaml"))
phase_two = index(documents(root, "phase-two-render.yaml"))
worker = index(documents(root, "worker-render.yaml"))
legacy = index(documents(root, "legacy-render.yaml"))
install_two = index(documents(root, "install-two-render.yaml"))
another_namespace = index(documents(root, "another-namespace-render.yaml"))
long_one = index(documents(root, "long-one-render.yaml"))
long_one_repeat = index(documents(root, "long-one-repeat-render.yaml"))
long_two = index(documents(root, "long-two-render.yaml"))

%w[coyote-server coyote-frontend coyote-mcp].each do |name|
  deployment = phase_one.fetch(["Deployment", name])
  abort "#{name} must render with zero replicas during first-install migration" unless deployment.dig("spec", "replicas") == 0
end

migration = phase_one.fetch(["Job", "coyote-migrate-first-install-001"])
abort "migration Job image must be digest-pinned" unless container(migration, "migrate").fetch("image").match?(/@sha256:[a-f0-9]{64}\z/)
abort "migration Job must mount its database URL" unless container(migration, "migrate").fetch("volumeMounts").any? { |mount| mount["name"] == "database-url" }
proxy = migration.dig("spec", "template", "spec", "initContainers").find { |entry| entry["name"] == "cloud-sql-proxy" }
abort "migration Job must run the Cloud SQL Auth Proxy" unless proxy
abort "migration ServiceAccount is missing" unless phase_one.key?(["ServiceAccount", "coyote-migrate"])
abort "migration SecretProviderClass is missing" unless phase_one.key?(["SecretProviderClass", "coyote-migrate-secrets"])

abort "phase two must omit the migration Job" if phase_two.keys.any? { |kind, _name| kind == "Job" }
{"coyote-server" => 2, "coyote-frontend" => 3, "coyote-mcp" => 4}.each do |name, replicas|
  abort "#{name} did not restore its configured replicas" unless phase_two.fetch(["Deployment", name]).dig("spec", "replicas") == replicas
end

abort "worker chart must remain independently installable" unless worker.key?(["Deployment", "coyote-kubernetes-worker-staging"])
worker_image = container(worker.fetch(["Deployment", "coyote-kubernetes-worker-staging"]), "worker").fetch("image")
abort "worker image must be digest-pinned" unless worker_image.match?(/@sha256:[a-f0-9]{64}\z/)
abort "MCP must be disabled by default" if legacy.key?(["Deployment", "coyote-mcp"])

legacy_name = legacy.fetch(["ClusterRole", "coyote-server-tokenreview"]).dig("metadata", "name")
abort "legacy GKE values must preserve the TokenReview RBAC name" unless legacy_name == "coyote-server-tokenreview"

first_name = phase_two.keys.find { |kind, _name| kind == "ClusterRole" }&.last
second_name = install_two.keys.find { |kind, _name| kind == "ClusterRole" }&.last
third_name = another_namespace.keys.find { |kind, _name| kind == "ClusterRole" }&.last
abort "release-specific TokenReview names must differ" if first_name == second_name || first_name == third_name
abort "TokenReview RBAC name must be a DNS label at most 63 characters" unless first_name.match?(/\A[a-z0-9]([-a-z0-9]*[a-z0-9])?\z/) && first_name.length <= 63

[phase_two, install_two, another_namespace].each do |render|
  role = render.find { |(kind, _name), _document| kind == "ClusterRole" }
  binding = render.find { |(kind, _name), _document| kind == "ClusterRoleBinding" }
  abort "TokenReview role and binding names must match" unless role.first.last == binding.first.last
  abort "TokenReview roleRef must match the resolved role name" unless binding.last.dig("roleRef", "name") == role.first.last
end

long_one_name = long_one.find { |(kind, _name), _document| kind == "ClusterRole" }.first.last
long_one_repeat_name = long_one_repeat.find { |(kind, _name), _document| kind == "ClusterRole" }.first.last
long_two_name = long_two.find { |(kind, _name), _document| kind == "ClusterRole" }.first.last
abort "long TokenReview name must remain stable" unless long_one_name == long_one_repeat_name
abort "different long release and namespace identities must remain distinct" if long_one_name == long_two_name
abort "long TokenReview name must preserve the hash suffix" unless long_one_name.match?(/-[a-f0-9]{8}-tokenreview\z/)
abort "long TokenReview name must be within Kubernetes limits" unless long_one_name.length <= 63

[long_one, long_two].each do |render|
  role = render.find { |(kind, _name), _document| kind == "ClusterRole" }
  binding = render.find { |(kind, _name), _document| kind == "ClusterRoleBinding" }
  abort "long TokenReview role and binding names must match" unless role.first.last == binding.first.last
  abort "long TokenReview roleRef must match the resolved role name" unless binding.last.dig("roleRef", "name") == role.first.last
end

puts "Helm first-install render tests passed"
RUBY
