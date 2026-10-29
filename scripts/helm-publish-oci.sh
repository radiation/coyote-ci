#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helm_bin="${HELM_BIN:-helm}"
gcloud_bin="${GCLOUD_BIN:-gcloud}"
ruby_bin="${RUBY_BIN:-ruby}"
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

is_google_artifact_registry() {
  [[ "$oci_registry" =~ ^[a-z0-9-]+-docker\.pkg\.dev/[^/]+/[^/]+$ ]]
}

is_google_artifact_registry_host() {
  [[ "$oci_registry" =~ ^[a-z0-9-]+-docker\.pkg\.dev/ ]]
}

lookup_google_artifact_registry_version() {
  local chart_name="$1"
  local version="$2"
  local registry_host project repository location image expected_tag
  local repository_error="$package_dir/artifact-registry-repository-error"
  local tags_error="$package_dir/artifact-registry-tags-error"
  local tags_file="$package_dir/artifact-registry-tags.json"
  local parse_error="$package_dir/artifact-registry-tags-parse-error"
  local matches_file="$package_dir/artifact-registry-tag-matches"
  local repository_format

  registry_host="${oci_registry%%/*}"
  project="${oci_registry#*/}"
  project="${project%%/*}"
  repository="${oci_registry##*/}"
  location="${registry_host%-docker.pkg.dev}"
  image="$oci_registry/charts/$chart_name"
  expected_tag="${version//+/_}"

  require_command "$gcloud_bin"
  require_command "$ruby_bin"
  if ! repository_format="$("$gcloud_bin" artifacts repositories describe "$repository" --project "$project" --location "$location" --format='value(format)' 2>"$repository_error")"; then
    echo "unable to verify Artifact Registry repository $oci_registry" >&2
    sed 's/[[:cntrl:]]//g' "$repository_error" >&2
    return 1
  fi
  [[ "$repository_format" == "DOCKER" ]] || { echo "Artifact Registry repository $oci_registry must use DOCKER format" >&2; return 1; }
  if ! "$gcloud_bin" artifacts docker images list "$oci_registry" --include-tags --format=json >"$tags_file" 2>"$tags_error"; then
    echo "unable to list Artifact Registry chart images for $oci_registry" >&2
    sed 's/[[:cntrl:]]//g' "$tags_error" >&2
    return 1
  fi
  if ! "$ruby_bin" -rjson -e '
    image = ARGV.fetch(0)
    expected_tag = ARGV.fetch(1)
    images = JSON.parse(STDIN.read)
    abort "Artifact Registry image response must be an array" unless images.is_a?(Array)
    images.each do |entry|
      abort "Artifact Registry image entry has an unexpected shape" unless entry.is_a?(Hash) && entry["package"].is_a?(String)
      tags = entry.fetch("tags", [])
      abort "Artifact Registry image tags have an unexpected shape" unless tags.is_a?(Array) && tags.all? { |tag| tag.is_a?(String) }
      puts entry["package"] if entry["package"] == image && tags.include?(expected_tag)
    end
  ' "$image" "$expected_tag" <"$tags_file" >"$matches_file" 2>"$parse_error"; then
    echo "unable to parse Artifact Registry chart images for $oci_registry" >&2
    sed 's/[[:cntrl:]]//g' "$parse_error" >&2
    return 1
  fi
  if [[ -s "$matches_file" ]]; then
    echo "refusing to overwrite existing chart version: oci://$image:$version" >&2
    return 1
  fi
}

lookup_version() {
  local reference="$1"
  local chart_name="$2"
  local version="$3"
  local error_file="$package_dir/lookup-error"
  if is_google_artifact_registry; then
    lookup_google_artifact_registry_version "$chart_name" "$version"
    return
  fi
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
  lookup_version "$reference" "$name" "$version" || return 1
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
if is_google_artifact_registry_host && ! is_google_artifact_registry; then
  echo "OCI_REGISTRY for Artifact Registry must be <location>-docker.pkg.dev/<project>/<repository>; nested paths are not supported" >&2
  exit 2
fi
[[ "${OCI_IMMUTABLE_TAGS_CONFIRMED:-}" == "true" ]] || { echo "OCI_IMMUTABLE_TAGS_CONFIRMED=true is required after verifying registry immutable-tag policy" >&2; exit 2; }
case "$selection" in all|control-plane|worker) ;; *) echo "HELM_CHART_SELECTION must be all, control-plane, or worker" >&2; exit 2 ;; esac

bash "$validator"

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