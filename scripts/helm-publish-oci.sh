#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helm_bin="${HELM_BIN:-helm}"
validator="${HELM_PACKAGE_VALIDATOR:-$repo_root/scripts/helm-package-validate.sh}"
oci_registry="${OCI_REGISTRY:-}"
selection="${HELM_CHART_SELECTION:-all}"
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

lookup_version() {
  local reference="$1"
  local version="$2"
  local error_file="$package_dir/lookup-error"
  if "$helm_bin" show chart "$reference" --version "$version" >/dev/null 2>"$error_file"; then
    echo "refusing to overwrite existing chart version: $reference:$version" >&2
    return 1
  fi
  if grep -Eqi '(^|[[:space:]:])manifest unknown([[:space:]:]|$)' "$error_file" && ! grep -Eqi 'unauthorized|authentication required|denied|forbidden|name unknown|repository.*not known|dial tcp|connection refused|timeout|tls|x509' "$error_file"; then
    return 0
  fi
  echo "unable to determine whether $reference:$version already exists" >&2
  sed 's/[[:cntrl:]]//g' "$error_file" >&2
  return 1
}

pull_and_render() {
  local name="$1"
  local version="$2"
  local reference="$3"
  local values_file="$4"
  local namespace="$5"
  local expected_digest="$6"
  local pull_dir="$package_dir/pulled-$name"
  local pull_output pulled_digest
  mkdir -p "$pull_dir"
  if ! pull_output="$("$helm_bin" pull "$reference" --version "$version" --destination "$pull_dir" 2>&1)"; then
    printf '%s\n' "$pull_output" >&2
    return 1
  fi
  pulled_digest="$(printf '%s\n' "$pull_output" | awk '/Digest: sha256:[a-f0-9]{64}/ { print $2; exit }')"
  [[ "$pulled_digest" =~ ^sha256:[a-f0-9]{64}$ ]] || { echo "pull completed without an OCI digest for $name" >&2; return 1; }
  [[ "$pulled_digest" == "$expected_digest" ]] || { echo "pulled OCI digest does not match pushed digest for $name" >&2; return 1; }
  "$helm_bin" template "$name" "$pull_dir/$name-$version.tgz" --namespace "$namespace" --values "$values_file" >/dev/null
}

publish_chart() {
  local label="$1"
  local expected_name="$2"
  local chart_dir="$3"
  local values_file="$4"
  local namespace="$5"
  local name version archive reference push_output digest
  name="$(chart_value "$chart_dir" name)"
  version="$(chart_value "$chart_dir" version)"
  [[ "$name" == "$expected_name" ]] || { echo "$label chart name is invalid: ${name:-<empty>}" >&2; return 1; }
  validate_semver "$version" || { echo "$label chart version is invalid: ${version:-<empty>}" >&2; return 1; }
  reference="oci://$oci_registry/charts/$name"
  lookup_version "$reference" "$version" || return 1
  "$helm_bin" package "$chart_dir" --destination "$package_dir" >/dev/null
  archive="$package_dir/$name-$version.tgz"
  [[ -f "$archive" ]] || { echo "package was not created for $name" >&2; return 1; }
  if ! push_output="$("$helm_bin" push "$archive" "oci://$oci_registry/charts" 2>&1)"; then
    printf '%s\n' "$push_output" >&2
    return 1
  fi
  digest="$(printf '%s\n' "$push_output" | awk '/Digest: sha256:[a-f0-9]{64}/ { print $2; exit }')"
  [[ "$digest" =~ ^sha256:[a-f0-9]{64}$ ]] || { echo "push completed without an OCI digest for $name" >&2; return 1; }
  pull_and_render "$name" "$version" "$reference" "$values_file" "$namespace" "$digest" || return 1
  printf 'published chart=%s version=%s reference=%s digest=%s verified=true\n' "$name" "$version" "$reference" "$digest"
}

require_command "$helm_bin"
[[ -n "$oci_registry" ]] || { echo "OCI_REGISTRY is required" >&2; exit 2; }
[[ "$oci_registry" != oci://* ]] || { echo "OCI_REGISTRY must not include the oci:// prefix" >&2; exit 2; }
[[ "${OCI_IMMUTABLE_TAGS_CONFIRMED:-}" == "true" ]] || { echo "OCI_IMMUTABLE_TAGS_CONFIRMED=true is required after verifying registry immutable-tag policy" >&2; exit 2; }
case "$selection" in all|control-plane|worker) ;; *) echo "HELM_CHART_SELECTION must be all, control-plane, or worker" >&2; exit 2 ;; esac

"$validator"

declare -a succeeded=()
declare -a failed=()
publish_selected() {
  local label="$1"
  shift
  if publish_chart "$label" "$@"; then
    succeeded+=("$label")
  else
    failed+=("$label")
    return 1
  fi
}

if [[ "$selection" == "all" || "$selection" == "control-plane" ]]; then
  publish_selected control-plane coyote-ci "$repo_root/deploy/helm/coyote-ci" "$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" coyote-ci || true
fi
if [[ "$selection" == "all" || "$selection" == "worker" ]] && [[ ${#failed[@]} -eq 0 ]]; then
  publish_selected worker coyote-ci-worker "$repo_root/deploy/helm/coyote-ci-worker" "$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" coyote-ci-staging || true
fi

if [[ ${#failed[@]} -gt 0 ]]; then
  printf 'OCI chart publication incomplete: succeeded=%s failed=%s\n' "${succeeded[*]:-none}" "${failed[*]}" >&2
  exit 1
fi

echo "OCI chart publication completed: ${succeeded[*]}"