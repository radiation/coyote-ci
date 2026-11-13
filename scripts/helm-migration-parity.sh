#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
values="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml"
render_dir="$(mktemp -d)"
trap 'rm -rf "$render_dir"' EXIT

command -v helm >/dev/null 2>&1 || { echo "helm is required" >&2; exit 1; }
command -v ruby >/dev/null 2>&1 || { echo "ruby is required" >&2; exit 1; }

helm template coyote-ci "$repo_root/deploy/helm/coyote-ci" --namespace coyote-ci --values "$values" >"$render_dir/ordinary.yaml"
helm template coyote-ci "$repo_root/deploy/helm/coyote-ci" --namespace coyote-ci --values "$values" \
  --show-only templates/migration-job.yaml --is-upgrade >"$render_dir/migration.yaml"

ruby - "$render_dir/ordinary.yaml" "$render_dir/migration.yaml" <<'RUBY'
require "yaml"

documents = YAML.load_stream(File.read(ARGV[0])).compact
job = documents.find { |doc| doc["kind"] == "Job" } or abort "automatic Job missing"
upgrade = YAML.load_stream(File.read(ARGV[1])).compact.fetch(0)
abort "install and upgrade migration specifications differ" unless job["spec"] == upgrade["spec"]
weights = [["ServiceAccount", "coyote-migrate", "-30"], ["SecretProviderClass", "coyote-migrate-secrets", "-20"], ["Job", job.dig("metadata", "name"), "-10"]]
weights.each do |kind, name, weight|
  doc = documents.find { |item| item["kind"] == kind && item.dig("metadata", "name") == name } or abort "missing #{kind}/#{name}"
  annotations = doc.dig("metadata", "annotations")
  abort "incorrect hook lifecycle" unless annotations["helm.sh/hook"] == "pre-install,pre-upgrade" && annotations["helm.sh/hook-weight"] == weight
  abort "hook must retain failure evidence and prerequisites" unless annotations["helm.sh/hook-delete-policy"] == "before-hook-creation"
end
abort "deadline missing" unless job.dig("spec", "activeDeadlineSeconds") == 600
abort "automatic retry must be disabled" unless job.dig("spec", "backoffLimit") == 0
proxy = job.dig("spec", "template", "spec", "initContainers").first
abort "native proxy sidecar/startup gate missing" unless proxy["restartPolicy"] == "Always" && proxy.dig("startupProbe", "exec", "command").include?("wait") && proxy["args"].include?("--health-check")
documents.select { |doc| doc["kind"] == "Deployment" }.each do |deployment|
  abort "application must remain ordinary" if deployment.dig("metadata", "annotations", "helm.sh/hook")
end
puts "Helm automatic migration lifecycle parity passed"
RUBY
