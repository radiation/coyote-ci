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

[[ "$dry_run" == "true" || "$dry_run" == "false" ]] || { echo "GKE_HELM_ADOPT_DRY_RUN must be true or false" >&2; exit 1; }
[[ "$apply_ownership" == "true" || "$apply_ownership" == "false" ]] || { echo "GKE_HELM_ADOPT_APPLY must be true or false" >&2; exit 1; }
[[ "$verify_only" == "true" || "$verify_only" == "false" ]] || { echo "GKE_HELM_ADOPT_VERIFY_ONLY must be true or false" >&2; exit 1; }

metadata_patch_from_snapshot() {
  local snapshot="$1"
  [[ -n "$snapshot" && -f "$snapshot" ]] || {
    echo "adoption snapshot is missing or unreadable: ${snapshot:-<empty>}" >&2
    exit 1
  }
  ruby -ryaml -rjson -e '
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
  ' "$snapshot"
}

release_namespace_from_snapshot() {
  case "$1" in
    "$control_release") printf '%s\n' "$control_namespace" ;;
    "$worker_release") printf '%s\n' "$worker_namespace" ;;
    *)
      echo "adoption snapshot names an unknown release: $1" >&2
      return 1
      ;;
  esac
}

live_ownership_state() {
  local namespace="$1"
  local reference="$2"
  local release="$3"
  local release_namespace="$4"
  local live
  if [[ "$namespace" == "-" ]]; then
    live="$(kubectl get "$reference" -o json)" || return 1
  else
    live="$(kubectl -n "$namespace" get "$reference" -o json)" || return 1
  fi
  ruby -rjson -e '
    metadata = JSON.parse(STDIN.read).fetch("metadata", {})
    actual = [
      metadata.fetch("labels", {})["app.kubernetes.io/managed-by"],
      metadata.fetch("annotations", {})["meta.helm.sh/release-name"],
      metadata.fetch("annotations", {})["meta.helm.sh/release-namespace"]
    ]
    expected = ["Helm", ARGV.fetch(0), ARGV.fetch(1)]
    puts actual == expected ? "adoption" : actual.all?(&:nil?) ? "absent" : "other"
  ' "$release" "$release_namespace" <<<"$live"
}

snapshot_matches_live_ownership() {
  local snapshot="$1"
  local namespace="$2"
  local reference="$3"
  local live
  if [[ "$namespace" == "-" ]]; then
    live="$(kubectl get "$reference" -o json)" || return 1
  else
    live="$(kubectl -n "$namespace" get "$reference" -o json)" || return 1
  fi
  ruby -ryaml -rjson -e '
    snapshot = YAML.load_file(ARGV.fetch(0)).fetch("metadata", {})
    live = JSON.parse(STDIN.read).fetch("metadata", {})
    keys = [
      ["labels", "app.kubernetes.io/managed-by"],
      ["annotations", "meta.helm.sh/release-name"],
      ["annotations", "meta.helm.sh/release-namespace"]
    ]
    expected = keys.map { |group, key| snapshot.fetch(group, {})[key] }
    actual = keys.map { |group, key| live.fetch(group, {})[key] }
    puts expected == actual ? "original" : "changed"
  ' "$snapshot" <<<"$live"
}

restore_metadata_from_state() {
  local path="$1"
  local records="$path/resources.normalized.tsv"
  local release namespace reference snapshot patch line=0 release_namespace state snapshot_state canonical_snapshot
  [[ -d "$path" && -f "$path/resources.tsv" ]] || {
    echo "adoption state directory must contain resources.tsv: $path" >&2
    return 1
  }
  ruby -e '
    source, destination = ARGV
    File.write(destination, "")
    File.foreach(source).with_index(1) do |line, number|
      fields = line.chomp.split("\t", -1)
      abort "invalid adoption snapshot entry at line #{number}" unless fields.length == 4
      release, namespace, reference, snapshot = fields
      abort "invalid adoption snapshot release at line #{number}" if release.empty?
      abort "invalid adoption snapshot reference at line #{number}" if reference.empty? || !reference.include?("/")
      abort "invalid adoption snapshot path at line #{number}" if snapshot.empty?
      namespace = "-" if namespace.empty?
      File.open(destination, "a") { |file| file.puts([release, namespace, reference, snapshot].join("\t")) }
    end
  ' "$path/resources.tsv" "$records" || {
    rm -f "$records"
    return 1
  }
  while IFS=$'\t' read -r release namespace reference snapshot; do
    line=$((line + 1))
    [[ "$namespace" == "-" || "$namespace" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || {
      echo "invalid adoption snapshot namespace at line $line: $namespace" >&2
      rm -f "$records"
      return 1
    }
    release_namespace="$(release_namespace_from_snapshot "$release")" || {
      rm -f "$records"
      return 1
    }
    snapshot_state="$(ruby -e '
      require "pathname"
      state, snapshot = ARGV
      root = File.realpath(File.join(state, "original"))
      abort "adoption snapshot root is not a directory: #{root}" unless File.directory?(root)
      parts = Pathname.new(snapshot).each_filename.to_a
      abort "adoption snapshot path contains traversal: #{snapshot}" if parts.include?("..")
      candidate = Pathname.new(snapshot).absolute? ? snapshot : File.expand_path(snapshot, state)
      if File.exist?(candidate)
        canonical = File.realpath(candidate)
        exists = "existing"
      else
        parent = File.realpath(File.dirname(candidate))
        canonical = File.join(parent, File.basename(candidate))
        exists = "missing"
      end
      abort "adoption snapshot path is outside #{root}: #{snapshot}" unless canonical.start_with?(root + File::SEPARATOR)
      puts [exists, canonical].join("\t")
    ' "$path" "$snapshot")" || {
      rm -f "$records"
      return 1
    }
    IFS=$'\t' read -r snapshot_state canonical_snapshot <<<"$snapshot_state"
    if [[ "$snapshot_state" == "missing" ]]; then
      state="$(live_ownership_state "$namespace" "$reference" "$release" "$release_namespace")" || {
        rm -f "$records"
        return 1
      }
      if [[ "$state" == "absent" ]]; then
        echo "Rollback entry already restored (snapshot missing): ${namespace:+$namespace/}$reference" >&2
        continue
      fi
      if [[ "$state" == "adoption" ]]; then
        echo "cannot restore ${namespace:+$namespace/}$reference: snapshot is missing and adoption ownership metadata remains" >&2
      else
        echo "cannot restore ${namespace:+$namespace/}$reference: snapshot is missing and ownership metadata is not absent" >&2
      fi
      rm -f "$records"
      return 1
    fi
    state="$(live_ownership_state "$namespace" "$reference" "$release" "$release_namespace")" || {
      rm -f "$records"
      return 1
    }
    if [[ "$state" != "adoption" ]]; then
      original_state="$(snapshot_matches_live_ownership "$canonical_snapshot" "$namespace" "$reference")" || {
        rm -f "$records"
        return 1
      }
      if [[ "$original_state" == "original" ]]; then
        echo "Rollback entry already restored: ${namespace:+$namespace/}$reference" >&2
        continue
      fi
      echo "cannot restore ${namespace:+$namespace/}$reference: current ownership metadata differs from both adoption and snapshot state" >&2
      rm -f "$records"
      return 1
    fi
    patch="$(metadata_patch_from_snapshot "$canonical_snapshot")" || {
      rm -f "$records"
      return 1
    }
    printf '+ kubectl'
    [[ "$namespace" != "-" ]] && printf ' -n %q' "$namespace"
    printf ' patch %q --type merge --patch %q\n' "$reference" "$patch"
    if [[ "$namespace" == "-" ]]; then
      kubectl patch "$reference" --type merge --patch "$patch" || {
        rm -f "$records"
        return 1
      }
    else
      kubectl -n "$namespace" patch "$reference" --type merge --patch "$patch" || {
        rm -f "$records"
        return 1
      }
    fi
  done <"$records"
  rm -f "$records"
}

if [[ -n "$rollback_state" ]]; then
  restore_metadata_from_state "$rollback_state" || exit 1
  echo "Restored Helm ownership metadata from $rollback_state without deleting Kubernetes resources."
  exit 0
fi

[[ -f "$control_values" ]] || { echo "control-plane values file does not exist: $control_values" >&2; exit 1; }
[[ -f "$worker_values" ]] || { echo "worker values file does not exist: $worker_values" >&2; exit 1; }

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
elif [[ "$worker_exists" == "true" && "$control_exists" != "true" ]]; then
  echo "worker Helm release exists without the control-plane release; refusing inconsistent partial adoption" >&2
  exit 1
elif [[ "$control_exists" == "true" && "$worker_exists" == "true" ]]; then
  echo "both Helm releases already exist; use GKE_HELM_ADOPT_VERIFY_ONLY=true to verify the established handoff" >&2
  exit 1
fi

compare_resource() {
  local expected="$1"
  local live="$2"
  local api_version="$3"
  local kind="$4"
  local name="$5"
  local namespace="$6"
  local replicas_managed_externally="$7"
  ruby -ryaml -e '
    expected_documents = YAML.load_stream(File.read(ARGV.fetch(0))).compact
    live = YAML.load_file(ARGV.fetch(1))
    api_version, kind, name, namespace, replicas_managed_externally = ARGV.drop(2)
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
    if kind == "Deployment" && !expected.fetch("spec", {}).key?("replicas") && live.fetch("spec", {}).key?("replicas")
      abort "#{kind}/#{name}: rendered replicas are absent but external replica management is not enabled" unless replicas_managed_externally == "true"
      live.fetch("spec").delete("replicas")
    end
    compare(expected, live)
  ' "$expected" "$live" "$api_version" "$kind" "$name" "$namespace" "$replicas_managed_externally"
}

replicas_managed_externally() {
  local release="$1"
  local name="$2"
  local values section
  case "$release/$name" in
    "$control_release/coyote-server") values="$control_values"; section="server" ;;
    "$control_release/coyote-frontend") values="$control_values"; section="frontend" ;;
    "$worker_release/coyote-kubernetes-worker-staging") values="$worker_values"; section="worker" ;;
    *) printf 'false\n'; return ;;
  esac
  ruby -ryaml -e 'puts YAML.load_file(ARGV.fetch(0)).fetch(ARGV.fetch(1)).fetch("replicasManagedExternally", false) == true' "$values" "$section"
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
declare -a control_resources=()
declare -a worker_resources=()
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
  external_replicas="$(replicas_managed_externally "$release" "$name")"
  compare_resource "$([[ "$release" == "$control_release" ]] && printf '%s' "$control_render" || printf '%s' "$worker_render")" "$live" "$api_version" "$kind" "$name" "$object_namespace" "$external_replicas" || {
    echo "material drift found; refusing Helm adoption for $reference" >&2
    exit 1
  }
  state="$(ownership_state "$live" "$release" "$release_namespace")"
  [[ "$state" != "conflict" ]] || {
    echo "conflicting or partial Helm ownership on ${object_namespace:+$object_namespace/}$reference; refusing to steal ownership" >&2
    exit 1
  }
  resource="$release"$'\t'"$release_namespace"$'\t'"${namespace}"$'\t'"$reference"$'\t'"$live"$'\t'"$state"
  resources+=("$resource")
  if [[ "$release" == "$control_release" ]]; then
    control_resources+=("$resource")
  else
    worker_resources+=("$resource")
  fi
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

validate_release_ownership() {
  local release_exists="$1"
  local release="$2"
  shift 2
  local resource resource_release release_namespace namespace reference live state expected
  expected="unowned"
  [[ "$release_exists" == "true" ]] && expected="owned"
  for resource in "$@"; do
    IFS=$'\t' read -r resource_release release_namespace namespace reference live state <<<"$resource"
    [[ "$state" == "$expected" ]] || {
      echo "inconsistent ownership for $release ${namespace:+$namespace/}$reference: expected $expected, got $state" >&2
      return 1
    }
  done
}

validate_release_ownership "$control_exists" "$control_release" "${control_resources[@]}"
validate_release_ownership "$worker_exists" "$worker_release" "${worker_resources[@]}"

echo "Resources eligible for Helm ownership adoption:"
printf '%s\n' "${resources[@]}" | while IFS=$'\t' read -r release release_namespace namespace reference _ state; do
  printf '%s\t%s\t%s\tcurrent=%s\n' "$release" "${namespace:--}" "$reference" "$state"
done

if [[ "$dry_run" == "true" ]]; then
  [[ "$control_exists" == "true" ]] && echo "Control-plane release already established; dry-run will continue with worker adoption after verified ownership."
  echo "Dry run complete: no ownership metadata or Helm releases were modified."
  exit 0
fi

helm upgrade --help | grep -q -- '--force-conflicts' || {
  echo "the installed Helm does not support --force-conflicts required for one-time server-side-apply adoption" >&2
  exit 1
}

rollback_metadata() {
  local path="$1"
  echo "Restoring Helm ownership metadata from $path; Kubernetes resources will not be deleted." >&2
  restore_metadata_from_state "$path"
}

adopt_release() {
  local release="$1"
  local release_namespace="$2"
  local chart="$3"
  local values="$4"
  local path="$5"
  shift 5
  local resource resource_release resource_release_namespace namespace reference live state original patch snapshot_namespace
  local -a modified=()
  mkdir -p "$path/original"
  : >"$path/resources.tsv"

  for resource in "$@"; do
    IFS=$'\t' read -r resource_release resource_release_namespace namespace reference live state <<<"$resource"
    original="$path/original/$(basename "$live")"
    cp "$live" "$original"
    printf '%s\t%s\t%s\t%s\n' "$resource_release" "$namespace" "$reference" "$original" >>"$path/resources.tsv"
    patch="$(ruby -rjson -e '
      release, namespace = ARGV
      puts JSON.generate(metadata: {
        labels: {"app.kubernetes.io/managed-by" => "Helm"},
        annotations: {
          "meta.helm.sh/release-name" => release,
          "meta.helm.sh/release-namespace" => namespace
        }
      })
    ' "$resource_release" "$resource_release_namespace")"
    if [[ "$namespace" == "-" ]]; then
      patch_command=(kubectl patch "$reference" --type merge --patch "$patch")
    else
      patch_command=(kubectl -n "$namespace" patch "$reference" --type merge --patch "$patch")
    fi
    if ! "${patch_command[@]}"; then
      echo "ownership patch failed for ${namespace:+$namespace/}$reference; modified resources: ${modified[*]:-none}" >&2
      rollback_metadata "$path"
      return 1
    fi
    modified+=("${namespace:+$namespace/}$reference")
  done

  if ! helm upgrade --install "$release" "$chart" --namespace "$release_namespace" --values "$values" --server-side=true --force-conflicts --wait --timeout "${timeout_seconds}s"; then
    echo "$release Helm release establishment failed; no Kubernetes resources will be deleted." >&2
    rollback_metadata "$path"
    return 1
  fi
}

state_path="$state_dir/$(date -u +%Y%m%dT%H%M%SZ)-${control_release}-${worker_release}"
if [[ "$control_exists" != "true" ]]; then
  adopt_release "$control_release" "$control_namespace" "$control_chart" "$control_values" "$state_path/control-plane" "${control_resources[@]}" || exit 1
fi
if [[ "$worker_exists" != "true" ]]; then
  adopt_release "$worker_release" "$worker_namespace" "$worker_chart" "$worker_values" "$state_path/worker" "${worker_resources[@]}" || exit 1
fi

echo "Helm ownership adoption completed. State retained under $state_path for explicit metadata-only rollback."
