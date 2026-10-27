#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
validator="$repo_root/scripts/helm-package-validate.sh"
temp_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$temp_dir"
}
trap cleanup EXIT

expect_failure() {
  local description="$1"
  shift
  if "$@" >"$temp_dir/output" 2>&1; then
    echo "expected failure: $description" >&2
    exit 1
  fi
}

HELM_PACKAGE_SKIP_EXISTING_TESTS=true bash "$validator"
helm package "$repo_root/deploy/helm/coyote-ci-worker" --destination "$temp_dir" >/dev/null
[[ -f "$temp_dir/coyote-ci-worker-0.1.0.tgz" ]] || { echo "worker chart did not package independently" >&2; exit 1; }

invalid_chart="$temp_dir/invalid-chart"
cp -R "$repo_root/deploy/helm/coyote-ci" "$invalid_chart"
sed -i.bak 's/^version: .*/version: not-semver/' "$invalid_chart/Chart.yaml"
rm "$invalid_chart/Chart.yaml.bak"
expect_failure "invalid chart version" env HELM_PACKAGE_SKIP_EXISTING_TESTS=true CONTROL_CHART_DIR="$invalid_chart" bash "$validator"

secret_chart="$temp_dir/secret-chart"
cp -R "$repo_root/deploy/helm/coyote-ci" "$secret_chart"
touch "$secret_chart/operator-secret.yaml"
expect_failure "secret-like archive path" env HELM_PACKAGE_SKIP_EXISTING_TESTS=true CONTROL_CHART_DIR="$secret_chart" bash "$validator"

nested_secret_chart="$temp_dir/nested-secret-chart"
cp -R "$repo_root/deploy/helm/coyote-ci" "$nested_secret_chart"
touch "$nested_secret_chart/templates/_private-key.pem"
expect_failure "nested secret-like template path" env HELM_PACKAGE_SKIP_EXISTING_TESTS=true CONTROL_CHART_DIR="$nested_secret_chart" bash "$validator"

echo "Helm package validation tests passed"