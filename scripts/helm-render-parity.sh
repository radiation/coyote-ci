#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
render_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$render_dir"
}
trap cleanup EXIT

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

require_command helm
require_command ruby

helm template coyote-ci "$repo_root/deploy/helm/coyote-ci" \
  --namespace coyote-ci \
  --values "$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
  >"$render_dir/control-plane.yaml"
helm template coyote-ci-worker "$repo_root/deploy/helm/coyote-ci-worker" \
  --namespace coyote-ci-staging \
  --values "$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
  >"$render_dir/worker.yaml"

ruby - "$repo_root" "$render_dir" <<'RUBY'
require "yaml"

repo_root, render_dir = ARGV
values = {
  "__GCP_PROJECT__" => "example-project",
  "__GOOGLE_CLOUD_PROJECT__" => "example-project",
  "__COYOTE_SERVER_GSA__" => "coyote-gke-server@example-project.iam.gserviceaccount.com",
  "__COYOTE_MIGRATE_GSA__" => "coyote-gke-migrate@example-project.iam.gserviceaccount.com",
  "__COYOTE_STAGING_WORKER_GSA__" => "coyote-gke-staging-worker@example-project.iam.gserviceaccount.com",
  "__DATABASE_URL_SECRET__" => "coyote-staging-database-url",
  "__WORKSPACE_HELPER_CAPABILITY_SECRET__" => "coyote-workspace-helper-capability-secret",
  "__OIDC_CLIENT_SECRET__" => "coyote-staging-oidc-client-secret",
  "__SESSION_SECRET__" => "coyote-staging-session-secret",
  "__CLOUD_SQL_INSTANCE__" => "example-project:us-central1:coyote-postgres",
  "__COYOTE_SERVER_IMAGE__" => "example.invalid/coyote-server@sha256:" + "a" * 64,
  "__COYOTE_FRONTEND_IMAGE__" => "example.invalid/coyote-frontend@sha256:" + "b" * 64,
  "__COYOTE_MIGRATE_IMAGE__" => "example.invalid/coyote-migrate@sha256:" + "c" * 64,
  "__COYOTE_STAGING_WORKER_IMAGE__" => "example.invalid/coyote-worker@sha256:" + "d" * 64,
  "__ARTIFACT_GCS_BUCKET__" => "coyote-artifacts",
  "__ARTIFACT_GCS_PREFIX__" => "builds",
  "__CACHE_GCS_BUCKET__" => "coyote-cache",
  "__CACHE_GCS_PREFIX__" => "coyote-ci/cache",
  "__WORKSPACE_REVISION_GCS_BUCKET__" => "coyote-workspace-revisions",
  "__WORKSPACE_REVISION_GCS_PREFIX__" => "workspace-revisions",
  "__AUTH_MODE__" => "oidc",
  "__BOOTSTRAP_ADMIN_EMAILS__" => "admin@example.invalid",
  "__OIDC_ISSUER_URL__" => "https://issuer.example.invalid",
  "__OIDC_CLIENT_ID__" => "coyote-staging",
  "__OIDC_REDIRECT_URL__" => "https://staging.example.invalid/auth/callback",
  "__COYOTE_PUBLIC_URL__" => "https://staging.example.invalid",
  "__OIDC_SCOPES__" => "openid email profile",
  "__CLOUD_BUILD_LOCATION__" => "us-central1",
  "__CLOUD_BUILD_RUNTIME_SERVICE_ACCOUNT__" => "coyote-cloud-build@example-project.iam.gserviceaccount.com",
  "__CLOUD_BUILD_ARTIFACT_REGISTRY_REPOSITORY__" => "us-central1-docker.pkg.dev/example-project/coyote-ci",
  "__CLOUD_BUILD_SOURCE_BUCKET__" => "coyote-sources",
  "__CLOUD_BUILD_SOURCE_PREFIX__" => "coyote-sources",
  "__WORKER_KUBERNETES_MAX_IN_FLIGHT_JOBS__" => "4",
  "__TEMPORARY_HOSTNAME__" => "staging.example.invalid",
  "__GATEWAY_ADDRESS_NAME__" => "coyote-staging-address",
  "__GATEWAY_CERTIFICATE_MAP__" => "coyote-staging-map"
}

def documents(path)
  YAML.load_stream(File.read(path)).compact
end

def parse(content)
  YAML.load_stream(content).compact
end

def substitute(path, values)
  values.reduce(File.read(path)) { |content, (key, value)| content.gsub(key, value) }
end

def index(documents)
  documents.each_with_object({}) do |document, result|
    metadata = document.fetch("metadata")
    key = [document.fetch("apiVersion"), document.fetch("kind"), metadata.fetch("namespace", ""), metadata.fetch("name")]
    result[key] = document
  end
end

control_raw = parse(substitute(File.join(repo_root, "deploy/kubernetes/gke/control-plane.yaml"), values))
control_raw.concat(parse(substitute(File.join(repo_root, "deploy/kubernetes/gke/temporary-gateway.yaml"), values)))
worker_raw = parse(substitute(File.join(repo_root, "deploy/kubernetes/gke/staging-worker.yaml"), values))

expected_control = index(control_raw)
expected_worker = index(worker_raw.reject { |document| document["kind"] == "Namespace" })
actual_control = index(documents(File.join(render_dir, "control-plane.yaml")))
actual_worker = index(documents(File.join(render_dir, "worker.yaml")))

failures = []
{ "control-plane" => [expected_control, actual_control], "worker" => [expected_worker, actual_worker] }.each do |name, (expected, actual)|
  failures << "#{name}: resource keys differ\nexpected: #{expected.keys.sort}\nactual: #{actual.keys.sort}" unless expected.keys.sort == actual.keys.sort
  (expected.keys & actual.keys).each do |key|
    failures << "#{name}: structural mismatch for #{key.inspect}" unless expected[key] == actual[key]
  end
end

abort failures.join("\n") unless failures.empty?
puts "Helm render parity passed: #{expected_control.length} control-plane and #{expected_worker.length} worker resources"
RUBY
