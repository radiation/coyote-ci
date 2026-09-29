#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT
mkdir "$temp_dir/bin" "$temp_dir/live"
log="$temp_dir/commands.log"

REAL_HELM="$(command -v helm)"
export REAL_HELM

cat >"$temp_dir/bin/helm" <<'EOF'
#!/usr/bin/env bash
printf 'helm %s\n' "$*" >>"$COMMAND_LOG"
case "$1" in
  template) exec "$REAL_HELM" "$@" ;;
  status) exit 1 ;;
  upgrade) [[ "${HELM_UPGRADE_FAIL:-false}" == "true" ]] && exit 1 ;;
  get) printf '%s\n' 'apiVersion: v1' 'kind: List' 'items: []' ;;
esac
EOF
cat >"$temp_dir/bin/kubectl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'kubectl %s\n' "$*" >>"$COMMAND_LOG"
args=("$@")
namespace=""
if [[ "${args[0]}" == "-n" ]]; then namespace="${args[1]}"; args=("${args[@]:2}"); fi
case "${args[0]}" in
  get)
    reference="${args[1]}"
    ruby -ryaml -e '
      paths = ENV.fetch("ADOPTION_RENDERS").split(":")
      documents = paths.flat_map { |path| YAML.load_stream(File.read(path)).compact }
      kind, name = ARGV.fetch(0).split("/", 2)
      namespace = ARGV.fetch(1)
      document = documents.find { |item| item["kind"].downcase == kind && item.dig("metadata", "name") == name && item.dig("metadata", "namespace").to_s == namespace }
      abort "missing #{kind}/#{name}" unless document
      case ENV["ADOPTION_SCENARIO"]
      when "conflict"
        document["metadata"]["annotations"] ||= {}
        document["metadata"]["annotations"]["meta.helm.sh/release-name"] = "other-release"
      when "drift"
        document.fetch("spec").delete("selector") if document["kind"] == "Deployment"
      end
      puts YAML.dump(document)
    ' "$reference" "$namespace"
    ;;
  patch)
    if [[ "${PATCH_FAILURE:-false}" == "true" ]]; then
      count_file="${COMMAND_LOG}.patches"
      count=0; [[ -f "$count_file" ]] && count="$(cat "$count_file")"
      count=$((count + 1)); printf '%s' "$count" >"$count_file"
      [[ "$count" -eq 2 ]] && exit 1
    fi
    ;;
esac
EOF
chmod +x "$temp_dir/bin/helm" "$temp_dir/bin/kubectl"

render_dir="$temp_dir/renders"
mkdir "$render_dir"
"$REAL_HELM" template coyote-ci "$repo_root/deploy/helm/coyote-ci" --namespace coyote-ci --values "$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" >"$render_dir/control.yaml"
"$REAL_HELM" template coyote-ci-worker "$repo_root/deploy/helm/coyote-ci-worker" --namespace coyote-ci-staging --values "$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" >"$render_dir/worker.yaml"

run_adopter() {
  env PATH="$temp_dir/bin:$PATH" COMMAND_LOG="$log" ADOPTION_RENDERS="$render_dir/control.yaml:$render_dir/worker.yaml" \
    GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
    GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" "$@"
}

run_adopter GKE_HELM_ADOPT_DRY_RUN=true bash "$repo_root/scripts/gke-helm-adopt.sh" >/dev/null
if grep -q '^kubectl .* patch ' "$log" || grep -q '^helm upgrade ' "$log"; then
  echo "dry-run performed a mutation" >&2
  exit 1
fi
if grep -Eq '^kubectl .* (namespace/|job/coyote-migrate)' "$log"; then
  echo "adoption must not include Namespace or migration Job" >&2
  exit 1
fi

: >"$log"
if run_adopter ADOPTION_SCENARIO=conflict GKE_HELM_ADOPT_DRY_RUN=true bash "$repo_root/scripts/gke-helm-adopt.sh" >/dev/null 2>&1; then
  echo "conflicting Helm ownership must fail" >&2
  exit 1
fi

: >"$log"
if run_adopter ADOPTION_SCENARIO=drift GKE_HELM_ADOPT_DRY_RUN=true bash "$repo_root/scripts/gke-helm-adopt.sh" >/dev/null 2>&1; then
  echo "missing live field must fail" >&2
  exit 1
fi

: >"$log"
if run_adopter PATCH_FAILURE=true GKE_HELM_ADOPT_DRY_RUN=false GKE_HELM_ADOPT_APPLY=true GKE_HELM_ADOPT_STATE_DIR="$temp_dir/state" bash "$repo_root/scripts/gke-helm-adopt.sh" >/dev/null 2>&1; then
  echo "partial patch failure must fail" >&2
  exit 1
fi
grep -q '^kubectl .* patch ' "$log" || { echo "partial patch failure did not patch resources" >&2; exit 1; }
! grep -q '^helm upgrade ' "$log" || { echo "partial patch failure established a release" >&2; exit 1; }

: >"$log"
if run_adopter HELM_UPGRADE_FAIL=true GKE_HELM_ADOPT_DRY_RUN=false GKE_HELM_ADOPT_APPLY=true GKE_HELM_ADOPT_STATE_DIR="$temp_dir/state-failure" bash "$repo_root/scripts/gke-helm-adopt.sh" >/dev/null 2>&1; then
  echo "Helm establishment failure must fail" >&2
  exit 1
fi
grep -q '^helm upgrade ' "$log" || { echo "expected Helm establishment attempt" >&2; exit 1; }
! grep -q '^kubectl .* delete ' "$log" || { echo "Helm establishment failure deleted a workload" >&2; exit 1; }

echo "GKE Helm adoption script tests passed"
