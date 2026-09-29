#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
control_chart="$repo_root/deploy/helm/coyote-ci"
worker_chart="$repo_root/deploy/helm/coyote-ci-worker"
control_values="${GKE_HELM_CONTROL_PLANE_VALUES:-$repo_root/.local/helm/gke-staging-control-plane-values.yaml}"
worker_values="${GKE_HELM_WORKER_VALUES:-$repo_root/.local/helm/gke-staging-worker-values.yaml}"
control_namespace="${GKE_HELM_CONTROL_PLANE_NAMESPACE:-coyote-ci}"
worker_namespace="${GKE_HELM_WORKER_NAMESPACE:-coyote-ci-staging}"
control_release="${GKE_HELM_CONTROL_PLANE_RELEASE:-coyote-ci}"
worker_release="${GKE_HELM_WORKER_RELEASE:-coyote-ci-worker}"
dry_run="${GKE_HELM_ADOPT_DRY_RUN:-true}"
apply_ownership="${GKE_HELM_ADOPT_APPLY:-false}"
verify_only="${GKE_HELM_ADOPT_VERIFY_ONLY:-false}"
timeout_seconds="${GKE_DEPLOY_TIMEOUT_SECONDS:-600}"
state_dir="${GKE_HELM_ADOPT_STATE_DIR:-$repo_root/.gke-helm-adoption-state}"
rollback_state="${GKE_HELM_ADOPT_ROLLBACK_STATE:-}"

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

require_command helm
require_command kubectl
require_command ruby

[[ -f "$control_values" ]] || { echo "control-plane values file does not exist: $control_values" >&2; exit 1; }
[[ -f "$worker_values" ]] || { echo "worker values file does not exist: $worker_values" >&2; exit 1; }
[[ "$dry_run" == "true" || "$dry_run" == "false" ]] || { echo "GKE_HELM_ADOPT_DRY_RUN must be true or false" >&2; exit 1; }
[[ "$apply_ownership" == "true" || "$apply_ownership" == "false" ]] || { echo "GKE_HELM_ADOPT_APPLY must be true or false" >&2; exit 1; }
[[ "$verify_only" == "true" || "$verify_only" == "false" ]] || { echo "GKE_HELM_ADOPT_VERIFY_ONLY must be true or false" >&2; exit 1; }

if [[ -n "$rollback_state" ]]; then
  [[ -d "$rollback_state" && -f "$rollback_state/resources.tsv" ]] || {
    echo "GKE_HELM_ADOPT_ROLLBACK_STATE must name an adoption state directory" >&2
    exit 1
  }
  while IFS=$'\t' read -r release namespace reference original; do
    patch="$(ruby -ryaml -rjson -e '
      document = YAML.load_file(ARGV.fetch(0))
      metadata = document.fetch("metadata", {})
      values = {
        "app.kubernetes.io/managed-by" => metadata.fetch("labels", {})["app.kubernetes.io/managed-by"],
        "meta.helm.sh/release-name" => metadata.fetch("annotations", {})["meta.helm.sh/release-name"],
        "meta.helm.sh/release-namespace" => metadata.fetch("annotations", {})["meta.helm.sh/release-namespace"]
      }
      puts JSON.generate(metadata: {
        labels: { values.keys[0] => values.values[0] },
        annotations: Hash[values.to_a.drop(1)]
      })
    ' "$original")"
    printf '+ kubectl'
    [[ -n "$namespace" ]] && printf ' -n %q' "$namespace"
    printf ' patch %q --type merge --patch %q\n' "$reference" "$patch"
    if [[ -n "$namespace" ]]; then
      kubectl -n "$namespace" patch "$reference" --type merge --patch "$patch"
    else
      kubectl patch "$reference" --type merge --patch "$patch"
    fi
  done <"$rollback_state/resources.tsv"
  echo "Restored Helm ownership metadata from $rollback_state without deleting Kubernetes resources."
  exit 0
fi

if [[ "$verify_only" == "true" && "$apply_ownership" == "true" ]]; then
  echo "verification mode cannot apply ownership metadata" >&2
  exit 1
fi
if [[ "$dry_run" == "false" && "$apply_ownership" != "true" && "$verify_only" != "true" ]]; then
  echo "set GKE_HELM_ADOPT_APPLY=true to mutate; default mode is dry-run" >&2
  exit 1
fi

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT
control_render="$work_dir/control.yaml"
worker_render="$work_dir/worker.yaml"
inventory="$work_dir/inventory.tsv"

helm template "$control_release" "$control_chart" --namespace "$control_namespace" --values "$control_values" >"$control_render"
helm template "$worker_release" "$worker_chart" --namespace "$worker_namespace" --values "$worker_values" >"$worker_render"

ruby -ryaml -e '
  renders = ARGV.each_slice(3).map { |release, namespace, path| [release, namespace, YAML.load_stream(File.read(path)).compact] }
  seen = {}
  renders.each do |release, namespace, documents|
    documents.each do |document|
      kind = document.fetch("kind")
      metadata = document.fetch("metadata")
      abort "Namespace must not be adopted" if kind == "Namespace"
      abort "migration Job must not be adopted" if kind == "Job"
      name = metadata.fetch("name")
      object_namespace = metadata.fetch("namespace", "")
      cluster_scoped = %w[ClusterRole ClusterRoleBinding].include?(kind)
      abort "#{kind}/#{name} unexpectedly lacks a namespace" if !cluster_scoped && object_namespace.empty?
      reference = "#{kind.downcase}/#{name}"
      key = [release, object_namespace, reference]
      abort "duplicate rendered resource #{key.join("/")}" if seen[key]
      seen[key] = true
      puts [release, namespace, object_namespace.empty? ? "-" : object_namespace, document.fetch("apiVersion"), kind, name, reference, cluster_scoped ? "cluster" : "namespaced"].join("\t")
    end
  end
' "$control_release" "$control_namespace" "$control_render" "$worker_release" "$worker_namespace" "$worker_render" >"$inventory"

release_exists() {
  helm status "$1" --namespace "$2" >/dev/null 2>&1
}

control_exists=false
worker_exists=false
release_exists "$control_release" "$control_namespace" && control_exists=true
release_exists "$worker_release" "$worker_namespace" && worker_exists=true

if [[ "$verify_only" == "true" ]]; then
  [[ "$control_exists" == "true" && "$worker_exists" == "true" ]] || {
    echo "verification requires both Helm releases to exist" >&2
    exit 1
  }
elif [[ "$control_exists" == "true" || "$worker_exists" == "true" ]]; then
  echo "adoption requires both Helm releases to be absent; use GKE_HELM_ADOPT_VERIFY_ONLY=true to verify an established handoff" >&2
  exit 1
fi

compare_resource() {
  local expected="$1"
  local live="$2"
  local api_version="$3"
  local kind="$4"
  local name="$5"
  local namespace="$6"
  ruby -ryaml -e '
    expected_documents = YAML.load_stream(File.read(ARGV.fetch(0))).compact
    live = YAML.load_file(ARGV.fetch(1))
    api_version, kind, name, namespace = ARGV.drop(2)
    expected = expected_documents.find do |document|
      metadata = document.fetch("metadata", {})
      document["apiVersion"] == api_version && document["kind"] == kind &&
        metadata["name"] == name && metadata.fetch("namespace", "") == namespace
    end
    abort "rendered object not found: #{api_version} #{kind}/#{name}" unless expected
    def compare(expected, actual, path = "$")
      case expected
      when Hash
        expected.each do |key, value|
          next if path == "$.metadata" && key == "labels"
          unless actual.is_a?(Hash) && actual.key?(key)
            abort "#{path}.#{key}: missing from live resource"
          end
          compare(value, actual[key], "#{path}.#{key}")
        end
      when Array
        abort "#{path}: live value is not an array" unless actual.is_a?(Array)
        expected.each_with_index do |value, index|
          abort "#{path}[#{index}]: missing from live resource" if actual.length <= index
          compare(value, actual[index], "#{path}[#{index}]")
        end
      else
        abort "#{path}: rendered=#{expected.inspect} live=#{actual.inspect}" unless expected == actual
      end
    end
    compare(expected, live)
  ' "$expected" "$live" "$api_version" "$kind" "$name" "$namespace"
}

ownership_state() {
  ruby -ryaml -e '
    metadata = YAML.load_file(ARGV.fetch(0)).fetch("metadata", {})
    label = metadata.fetch("labels", {})["app.kubernetes.io/managed-by"]
    release = metadata.fetch("annotations", {})["meta.helm.sh/release-name"]
    namespace = metadata.fetch("annotations", {})["meta.helm.sh/release-namespace"]
    desired = ARGV.drop(1)
    if [label, release, namespace].all?(&:nil?)
      puts "unowned"
    elsif [label, release, namespace] == ["Helm", *desired]
      puts "owned"
    else
      puts "conflict"
    end
  ' "$1" "$2" "$3"
}

echo "Rendered resources to inspect:"
cat "$inventory"

declare -a resources=()
while IFS=$'\t' read -r release release_namespace namespace api_version kind name reference scope; do
  object_namespace="$namespace"
  [[ "$object_namespace" == "-" ]] && object_namespace=""
  live="$work_dir/${release}-${kind}-${name}.yaml"
  if [[ "$scope" == "cluster" ]]; then
    kubectl get "$reference" -o yaml >"$live" || {
      echo "required live resource is missing: $reference" >&2
      exit 1
    }
  else
    kubectl -n "$object_namespace" get "$reference" -o yaml >"$live" || {
      echo "required live resource is missing: $object_namespace/$reference" >&2
      exit 1
    }
  fi
  compare_resource "$([[ "$release" == "$control_release" ]] && printf '%s' "$control_render" || printf '%s' "$worker_render")" "$live" "$api_version" "$kind" "$name" "$object_namespace" || {
    echo "material drift found; refusing Helm adoption for $reference" >&2
    exit 1
  }
  state="$(ownership_state "$live" "$release" "$release_namespace")"
  [[ "$state" != "conflict" ]] || {
    echo "conflicting or partial Helm ownership on ${object_namespace:+$object_namespace/}$reference; refusing to steal ownership" >&2
    exit 1
  }
  resources+=("$release"$'\t'"$release_namespace"$'\t'"${namespace}"$'\t'"$reference"$'\t'"$live"$'\t'"$state")
done <"$inventory"

if [[ "$verify_only" == "true" ]]; then
  for resource in "${resources[@]}"; do
    IFS=$'\t' read -r release release_namespace namespace reference live state <<<"$resource"
    [[ "$namespace" == "-" ]] && namespace=""
    [[ "$state" == "owned" ]] || {
      echo "expected release ownership is missing from ${namespace:+$namespace/}$reference" >&2
      exit 1
    }
  done
  helm status "$control_release" --namespace "$control_namespace"
  helm status "$worker_release" --namespace "$worker_namespace"
  helm get manifest "$control_release" --namespace "$control_namespace" >/dev/null
  helm get manifest "$worker_release" --namespace "$worker_namespace" >/dev/null
  echo "Helm adoption ownership and release presence verified."
  exit 0
fi

echo "Resources eligible for Helm ownership adoption:"
printf '%s\n' "${resources[@]}" | while IFS=$'\t' read -r release release_namespace namespace reference _ state; do
  printf '%s\t%s\t%s\tcurrent=%s\n' "$release" "${namespace:--}" "$reference" "$state"
done

if [[ "$dry_run" == "true" ]]; then
  echo "Dry run complete: no ownership metadata or Helm releases were modified."
  exit 0
fi

state_path="$state_dir/$(date -u +%Y%m%dT%H%M%SZ)-${control_release}-${worker_release}"
mkdir -p "$state_path/original"
: >"$state_path/resources.tsv"
modified=()

rollback_metadata() {
  local release namespace reference original patch
  echo "Restoring Helm ownership metadata from $state_path; Kubernetes resources will not be deleted." >&2
  while IFS=$'\t' read -r release namespace reference original; do
    patch="$(ruby -ryaml -rjson -e '
      document = YAML.load_file(ARGV.fetch(0))
      metadata = document.fetch("metadata", {})
      labels = metadata.fetch("labels", {})
      annotations = metadata.fetch("annotations", {})
      puts JSON.generate(metadata: {
        labels: {"app.kubernetes.io/managed-by" => labels["app.kubernetes.io/managed-by"]},
        annotations: {
          "meta.helm.sh/release-name" => annotations["meta.helm.sh/release-name"],
          "meta.helm.sh/release-namespace" => annotations["meta.helm.sh/release-namespace"]
        }
      })
    ' "$original")"
    if [[ -n "$namespace" ]]; then
      kubectl -n "$namespace" patch "$reference" --type merge --patch "$patch" >/dev/null
    else
      kubectl patch "$reference" --type merge --patch "$patch" >/dev/null
    fi
  done <"$state_path/resources.tsv"
}

for resource in "${resources[@]}"; do
  IFS=$'\t' read -r release release_namespace namespace reference live state <<<"$resource"
  [[ "$namespace" == "-" ]] && namespace=""
  original="$state_path/original/$(basename "$live")"
  cp "$live" "$original"
  printf '%s\t%s\t%s\t%s\n' "$release" "$namespace" "$reference" "$original" >>"$state_path/resources.tsv"
  [[ "$state" == "owned" ]] && continue
  patch="$(ruby -rjson -e '
    release, namespace = ARGV
    puts JSON.generate(metadata: {
      labels: {"app.kubernetes.io/managed-by" => "Helm"},
      annotations: {
        "meta.helm.sh/release-name" => release,
        "meta.helm.sh/release-namespace" => namespace
      }
    })
  ' "$release" "$release_namespace")"
  if [[ -n "$namespace" ]]; then
    patch_command=(kubectl -n "$namespace" patch "$reference" --type merge --patch "$patch")
  else
    patch_command=(kubectl patch "$reference" --type merge --patch "$patch")
  fi
  if ! "${patch_command[@]}"; then
    echo "ownership patch failed for ${namespace:+$namespace/}$reference; modified resources: ${modified[*]:-none}" >&2
    rollback_metadata
    exit 1
  fi
  modified+=("${namespace:+$namespace/}$reference")
done

if ! helm upgrade --install "$control_release" "$control_chart" --namespace "$control_namespace" --values "$control_values" --wait --timeout "${timeout_seconds}s"; then
  echo "control-plane Helm release establishment failed; no Kubernetes resources will be deleted." >&2
  rollback_metadata
  exit 1
fi
if ! helm upgrade --install "$worker_release" "$worker_chart" --namespace "$worker_namespace" --values "$worker_values" --wait --timeout "${timeout_seconds}s"; then
  echo "worker Helm release establishment failed; no Kubernetes resources will be deleted." >&2
  rollback_metadata
  exit 1
fi

echo "Helm ownership adoption completed. State retained at $state_path for an explicit metadata-only rollback."
