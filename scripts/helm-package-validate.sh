#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helm_bin="${HELM_BIN:-helm}"
control_chart="${CONTROL_CHART_DIR:-$repo_root/deploy/helm/coyote-ci}"
worker_chart="${WORKER_CHART_DIR:-$repo_root/deploy/helm/coyote-ci-worker}"
control_values="${CONTROL_VALUES_FILE:-$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml}"
worker_values="${WORKER_VALUES_FILE:-$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml}"
package_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$package_dir"
}
trap cleanup EXIT

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

chart_value() {
  local chart_dir="$1"
  local key="$2"
  awk -F ': *' -v key="$key" '$1 == key { value=$2; sub(/[[:space:]]+#.*$/, "", value); gsub(/^"|"$|^'"'"'|'"'"'$/, "", value); print value; exit }' "$chart_dir/Chart.yaml"
}

validate_semver() {
  [[ "$1" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$ ]]
}

validate_chart() {
  local expected_name="$1"
  local chart_dir="$2"
  local actual_name version
  actual_name="$(chart_value "$chart_dir" name)"
  version="$(chart_value "$chart_dir" version)"
  [[ "$actual_name" == "$expected_name" ]] || { echo "expected chart name $expected_name, got ${actual_name:-<empty>}" >&2; exit 1; }
  validate_semver "$version" || { echo "chart $expected_name has invalid SemVer version ${version:-<empty>}" >&2; exit 1; }
}

validate_archive_contents() {
  local archive="$1"
  local chart_name="$2"
  local archive_path filename
  while IFS= read -r archive_path; do
    case "$archive_path" in
      "$chart_name/Chart.yaml"|"$chart_name/values.yaml"|"$chart_name/values.schema.json"|"$chart_name/.helmignore"|"$chart_name/templates/"*) ;;
      *) echo "unexpected package path in $archive: $archive_path" >&2; exit 1 ;;
    esac
    filename="${archive_path##*/}"
    if [[ "$archive_path" != "$chart_name/templates/secrets.yaml" && "$filename" =~ (secrets?|credentials?|passwords?|tokens?|private[._-]?key)([._-].*|$) || "$filename" =~ \.env($|[._-].*) || "$filename" =~ \.local\.(yaml|yml)$ ]]; then
      echo "secret-like or operator-local package path in $archive: $archive_path" >&2
      exit 1
    fi
  done < <(tar -tzf "$archive")
}

normalize_manifest() {
  sed -E '/^# Source: /d'
}

render_and_compare() {
  local chart_name="$1"
  local chart_dir="$2"
  local values_file="$3"
  local archive="$4"
  local namespace="$5"
  shift 5
  local source_render="$package_dir/$chart_name-source.yaml"
  local package_render="$package_dir/$chart_name-package.yaml"

  "$helm_bin" template "$chart_name" "$chart_dir" --namespace "$namespace" --values "$values_file" "$@" | normalize_manifest >"$source_render"
  "$helm_bin" template "$chart_name" "$archive" --namespace "$namespace" --values "$values_file" "$@" | normalize_manifest >"$package_render"
  diff -u "$source_render" "$package_render"
}

require_command "$helm_bin"
require_command tar
require_command diff

if [[ "${HELM_PACKAGE_SKIP_EXISTING_TESTS:-false}" != "true" ]]; then
  bash "$repo_root/scripts/helm-render-parity.sh"
  bash "$repo_root/scripts/helm-schema-test.sh"
  bash "$repo_root/scripts/helm-migration-parity.sh"
  bash "$repo_root/scripts/helm-first-install-test.sh"
fi

validate_chart coyote-ci "$control_chart"

"$helm_bin" package "$control_chart" --destination "$package_dir" >/dev/null

control_version="$(chart_value "$control_chart" version)"
control_archive="$package_dir/coyote-ci-$control_version.tgz"

[[ -f "$control_archive" ]] || { echo "control-plane chart archive was not created" >&2; exit 1; }

validate_archive_contents "$control_archive" coyote-ci
render_and_compare coyote-ci "$control_chart" "$control_values" "$control_archive" coyote-ci
render_and_compare coyote-ci "$control_chart" "$control_values" "$control_archive" coyote-ci --is-upgrade
render_and_compare coyote-ci "$control_chart" "$repo_root/deploy/helm/examples/generic-control-plane-values.yaml" "$control_archive" portable-control
render_and_compare coyote-ci "$control_chart" "$repo_root/deploy/helm/examples/generic-control-plane-values.yaml" "$control_archive" portable-control --is-upgrade
render_and_compare coyote-ci "$control_chart" "$repo_root/deploy/helm/examples/generic-values.yaml" "$control_archive" portable-control
render_and_compare coyote-ci "$control_chart" "$repo_root/deploy/helm/examples/generic-values.yaml" "$control_archive" portable-control --is-upgrade
for profile in staging production; do
  render_and_compare coyote-ci "$control_chart" "$repo_root/deploy/helm/examples/gke-$profile-control-plane-values.yaml" "$control_archive" coyote-ci --values "$repo_root/deploy/helm/examples/gke-$profile-unified-worker-values.yaml"
done

echo "Unified Helm package validation passed: coyote-ci $control_version"

if [[ "${HELM_PACKAGE_INCLUDE_LEGACY_WORKER:-false}" == true ]]; then
  validate_chart coyote-ci-worker "$worker_chart"
  "$helm_bin" package "$worker_chart" --destination "$package_dir" >/dev/null
  worker_version="$(chart_value "$worker_chart" version)"
  worker_archive="$package_dir/coyote-ci-worker-$worker_version.tgz"
  [[ -f "$worker_archive" ]] || { echo "legacy worker chart archive was not created" >&2; exit 1; }
  validate_archive_contents "$worker_archive" coyote-ci-worker
  render_and_compare coyote-ci-worker "$worker_chart" "$worker_values" "$worker_archive" coyote-ci-staging
  render_and_compare coyote-ci-worker "$worker_chart" "$repo_root/deploy/helm/examples/generic-worker-values.yaml" "$worker_archive" portable-execution
  echo "Legacy worker package compatibility passed: coyote-ci-worker $worker_version"
fi