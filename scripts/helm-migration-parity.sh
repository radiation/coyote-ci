#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
values="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml"
render_dir="$(mktemp -d)"
trap 'rm -rf "$render_dir"' EXIT

command -v helm >/dev/null 2>&1 || { echo "helm is required" >&2; exit 1; }
command -v ruby >/dev/null 2>&1 || { echo "ruby is required" >&2; exit 1; }

helm template coyote-ci "$repo_root/deploy/helm/coyote-ci" --namespace coyote-ci --values "$values" >"$render_dir/ordinary.yaml"
if grep -q '^kind: Job$' "$render_dir/ordinary.yaml"; then
  echo "ordinary control-plane render must not contain a migration Job" >&2
  exit 1
fi
helm template coyote-ci "$repo_root/deploy/helm/coyote-ci" --namespace coyote-ci --values "$values" \
  --show-only templates/migration-job.yaml --set migration.render=true --set migration.runID=parity-run >"$render_dir/migration.yaml"

ruby - "$repo_root/deploy/kubernetes/gke/control-plane-migration.yaml" "$render_dir/migration.yaml" <<'RUBY'
require "yaml"

raw_path, rendered_path = ARGV
replacements = {
  "__COYOTE_MIGRATE_IMAGE__" => "example.invalid/coyote-migrate@sha256:" + "c" * 64,
  "__CLOUD_SQL_INSTANCE__" => "example-project:us-central1:coyote-postgres"
}
raw = replacements.reduce(File.read(raw_path)) { |content, (key, value)| content.gsub(key, value) }
expected = YAML.load(raw)
expected["metadata"]["name"] = "coyote-migrate-parity-run"
actual = YAML.load_stream(File.read(rendered_path)).compact.fetch(0)
abort "migration render differs from the current manifest" unless expected == actual
puts "Helm migration render parity passed"
RUBY
