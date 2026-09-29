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
  status)
    [[ -f "$MOCK_HELM_STATE/$2" ]] && exit 0
    exit 1
    ;;
  upgrade)
    [[ "${2:-}" == "--help" ]] && { printf '%s\n' '      --force-conflicts                            if set server-side apply will force changes against conflicts'; exit 0; }
    release="${3:-}"
    [[ "${HELM_UPGRADE_FAIL:-false}" == "true" || "${HELM_UPGRADE_FAIL_ON:-}" == "$release" ]] && exit 1
    mkdir -p "$MOCK_HELM_STATE"
    : >"$MOCK_HELM_STATE/$release"
    ;;
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
emit_state() {
  local file="$1"
  if [[ " ${args[*]} " == *" -o json "* ]]; then
    ruby -ryaml -rjson -e 'puts JSON.generate(YAML.load_file(ARGV.fetch(0)))' "$file"
  else
    cat "$file"
  fi
}
case "${args[0]}" in
  get)
    reference="${args[1]}"
    state_file="$MOCK_STATE_DIR/${namespace:-cluster}--${reference//\//_}.yaml"
    fixture_file="${ROLLBACK_FIXTURE_DIR:-}/$(basename "$state_file")"
    mkdir -p "$MOCK_STATE_DIR"
    if [[ -f "$state_file" ]]; then
      emit_state "$state_file"
      exit 0
    fi
    if [[ -n "${ROLLBACK_FIXTURE_DIR:-}" && -f "$fixture_file" ]]; then
      cp "$fixture_file" "$state_file"
      emit_state "$state_file"
      exit 0
    fi
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
    ' "$reference" "$namespace" >"$state_file"
    emit_state "$state_file"
    ;;
  patch)
    if [[ "${PATCH_FAILURE:-false}" == "true" ]]; then
      count_file="${COMMAND_LOG}.patches"
      count=0; [[ -f "$count_file" ]] && count="$(cat "$count_file")"
      count=$((count + 1)); printf '%s' "$count" >"$count_file"
      [[ "$count" -eq 2 ]] && exit 1
    fi
    reference="${args[1]}"
    state_file="$MOCK_STATE_DIR/${namespace:-cluster}--${reference//\//_}.yaml"
    patch=""
    for ((index = 0; index < ${#args[@]}; index++)); do
      if [[ "${args[$index]}" == "--patch" ]]; then
        patch="${args[$((index + 1))]}"
        break
      fi
    done
    [[ -f "$state_file" && -n "$patch" ]] || { echo "patch has no retrieved state: $reference" >&2; exit 1; }
    ruby -ryaml -rjson -e '
      document = YAML.load_file(ARGV.fetch(0))
      patch = JSON.parse(ARGV.fetch(1)).fetch("metadata", {})
      metadata = document["metadata"] ||= {}
      patch.each do |group, fields|
        target = metadata[group] ||= {}
        fields.each do |key, value|
          value.nil? ? target.delete(key) : target[key] = value
        end
      end
      puts YAML.dump(document)
    ' "$state_file" "$patch" >"$state_file.updated"
    mv "$state_file.updated" "$state_file"
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
    MOCK_STATE_DIR="$temp_dir/mock-state" \
    MOCK_HELM_STATE="$temp_dir/mock-helm-state" \
    GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
    GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" "$@"
}

reset_mock_state() {
  rm -rf "$temp_dir/mock-state"
  rm -rf "$temp_dir/mock-helm-state"
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
reset_mock_state
if run_adopter ADOPTION_SCENARIO=conflict GKE_HELM_ADOPT_DRY_RUN=true bash "$repo_root/scripts/gke-helm-adopt.sh" >/dev/null 2>&1; then
  echo "conflicting Helm ownership must fail" >&2
  exit 1
fi

: >"$log"
reset_mock_state
if run_adopter ADOPTION_SCENARIO=drift GKE_HELM_ADOPT_DRY_RUN=true bash "$repo_root/scripts/gke-helm-adopt.sh" >/dev/null 2>&1; then
  echo "missing live field must fail" >&2
  exit 1
fi

: >"$log"
reset_mock_state
if run_adopter PATCH_FAILURE=true GKE_HELM_ADOPT_DRY_RUN=false GKE_HELM_ADOPT_APPLY=true GKE_HELM_ADOPT_STATE_DIR="$temp_dir/state" bash "$repo_root/scripts/gke-helm-adopt.sh" >/dev/null 2>&1; then
  echo "partial patch failure must fail" >&2
  exit 1
fi
grep -q '^kubectl .* patch ' "$log" || { echo "partial patch failure did not patch resources" >&2; exit 1; }
! grep -q '^helm upgrade --install ' "$log" || { echo "partial patch failure established a release" >&2; exit 1; }

: >"$log"
reset_mock_state
if run_adopter HELM_UPGRADE_FAIL=true GKE_HELM_ADOPT_DRY_RUN=false GKE_HELM_ADOPT_APPLY=true GKE_HELM_ADOPT_STATE_DIR="$temp_dir/state-failure" bash "$repo_root/scripts/gke-helm-adopt.sh" >/dev/null 2>&1; then
  echo "Helm establishment failure must fail" >&2
  exit 1
fi
grep -q '^helm upgrade --install ' "$log" || { echo "expected Helm establishment attempt" >&2; exit 1; }
! grep -q '^kubectl .* delete ' "$log" || { echo "Helm establishment failure deleted a workload" >&2; exit 1; }
! grep -q '^helm uninstall ' "$log" || { echo "Helm establishment failure invoked helm uninstall" >&2; exit 1; }
grep -q '^helm upgrade --install coyote-ci .*--server-side=true --force-conflicts' "$log" || {
  echo "adoption release establishment must use Helm 4 server-side conflict transfer" >&2
  exit 1
}
rollback_patches="$(grep -c '\"app.kubernetes.io/managed-by\":null' "$log" || true)"
[[ "$rollback_patches" -eq 19 ]] || {
  echo "control-plane establishment failure must restore ownership metadata for 19 resources; got $rollback_patches" >&2
  cat "$log" >&2
  exit 1
}
grep -q '^kubectl -n coyote-ci patch role/coyote-server-workspace-verifier ' "$log" || {
  echo "namespaced ownership metadata was not restored" >&2
  exit 1
}
grep -q '^kubectl patch clusterrole/coyote-server-tokenreview ' "$log" || {
  echo "cluster-scoped ownership metadata was not restored" >&2
  exit 1
}

 : >"$log"
reset_mock_state
if run_adopter HELM_UPGRADE_FAIL_ON=coyote-ci-worker GKE_HELM_ADOPT_DRY_RUN=false GKE_HELM_ADOPT_APPLY=true GKE_HELM_ADOPT_STATE_DIR="$temp_dir/state-worker-failure" bash "$repo_root/scripts/gke-helm-adopt.sh" >/dev/null 2>&1; then
  echo "worker establishment failure must fail" >&2
  exit 1
fi
grep -q '^helm upgrade --install coyote-ci ' "$log" || {
  echo "control-plane release was not established before worker failure" >&2
  exit 1
}
grep -q '^helm upgrade --install coyote-ci-worker ' "$log" || {
  echo "worker establishment was not attempted" >&2
  exit 1
}
worker_rollback_patches="$(grep -c '\"app.kubernetes.io/managed-by\":null' "$log" || true)"
[[ "$worker_rollback_patches" -eq 11 ]] || {
  echo "worker failure must restore only 11 worker resources; got $worker_rollback_patches" >&2
  cat "$log" >&2
  exit 1
}
! grep -q '^kubectl -n coyote-ci patch .*\"app.kubernetes.io/managed-by\":null' "$log" || {
  echo "worker failure rolled back established control-plane ownership" >&2
  exit 1
}

: >"$log"
run_adopter GKE_HELM_ADOPT_DRY_RUN=false GKE_HELM_ADOPT_APPLY=true GKE_HELM_ADOPT_STATE_DIR="$temp_dir/state-worker-retry" bash "$repo_root/scripts/gke-helm-adopt.sh" >/dev/null
! grep -q '^helm upgrade --install coyote-ci ' "$log" || {
  echo "control-plane-only retry must not re-establish the existing control-plane release" >&2
  cat "$log" >&2
  exit 1
}
grep -q '^helm upgrade --install coyote-ci-worker ' "$log" || {
  echo "control-plane-only retry did not establish worker release" >&2
  exit 1
}

fixture_dir="$temp_dir/rollback-fixtures"
mkdir "$fixture_dir"

write_fixture() {
  local namespace="$1"
  local reference="$2"
  local ownership="$3"
  local file="$fixture_dir/${namespace:-cluster}--${reference//\//_}.yaml"
  cat >"$file" <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: ${reference#*/}
EOF
  if [[ "$ownership" == "adoption" ]]; then
    cat >>"$file" <<'EOF'
  labels:
    app.kubernetes.io/managed-by: Helm
  annotations:
    meta.helm.sh/release-name: coyote-ci
    meta.helm.sh/release-namespace: coyote-ci
EOF
  fi
}

restore_state="$temp_dir/restore-state"
mkdir -p "$restore_state/original"
cat >"$restore_state/original/namespaced.yaml" <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: example
EOF
cat >"$restore_state/original/cluster.yaml" <<'EOF'
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: example
EOF
write_fixture coyote-ci configmap/example adoption
write_fixture '' clusterrole/example adoption
write_fixture coyote-ci configmap/already-restored absent
printf '%s\n' \
  "coyote-ci	coyote-ci	configmap/example	$restore_state/original/namespaced.yaml" \
  "coyote-ci		clusterrole/example	$restore_state/original/cluster.yaml" \
  "coyote-ci	coyote-ci	configmap/already-restored	$restore_state/original/missing.yaml" \
  >"$restore_state/resources.tsv"
: >"$log"
reset_mock_state
run_adopter ROLLBACK_FIXTURE_DIR="$fixture_dir" GKE_HELM_ADOPT_ROLLBACK_STATE="$restore_state" bash "$repo_root/scripts/gke-helm-adopt.sh" >/dev/null
grep -q '^kubectl -n coyote-ci patch configmap/example ' "$log" || {
  echo "namespaced external rollback was not applied" >&2
  exit 1
}
grep -q '^kubectl patch clusterrole/example ' "$log" || {
  echo "legacy empty namespace external rollback was not applied as cluster-scoped" >&2
  exit 1
}
! grep -q 'patch .*  --type' "$log" || {
  echo "rollback passed an empty resource reference" >&2
  exit 1
}
grep -q '\"app.kubernetes.io/managed-by\":null' "$log" || {
  echo "mixed rollback did not restore ownership metadata" >&2
  exit 1
}
: >"$log"
run_adopter ROLLBACK_FIXTURE_DIR="$fixture_dir" GKE_HELM_ADOPT_ROLLBACK_STATE="$restore_state" bash "$repo_root/scripts/gke-helm-adopt.sh" >/dev/null
! grep -q '^kubectl .* patch ' "$log" || {
  echo "second rollback invocation must be idempotent" >&2
  cat "$log" >&2
  exit 1
}

missing_owned_state="$temp_dir/missing-owned-state"
mkdir -p "$missing_owned_state/original"
write_fixture coyote-ci configmap/missing-owned adoption
printf '%s\n' "coyote-ci	coyote-ci	configmap/missing-owned	$missing_owned_state/original/missing.yaml" >"$missing_owned_state/resources.tsv"
: >"$log"
reset_mock_state
if run_adopter ROLLBACK_FIXTURE_DIR="$fixture_dir" GKE_HELM_ADOPT_ROLLBACK_STATE="$missing_owned_state" bash "$repo_root/scripts/gke-helm-adopt.sh" >/dev/null 2>"$temp_dir/missing-owned.err"; then
  echo "missing snapshot with adoption ownership must fail" >&2
  exit 1
fi
grep -q 'snapshot is missing and adoption ownership metadata remains' "$temp_dir/missing-owned.err" || {
  echo "missing owned snapshot did not produce a controlled error" >&2
  cat "$temp_dir/missing-owned.err" >&2
  exit 1
}
! grep -q '^kubectl .* patch ' "$log" || {
  echo "missing owned snapshot attempted a metadata patch" >&2
  exit 1
}

corrupt_state="$temp_dir/corrupt-state"
mkdir -p "$corrupt_state/original"
printf '%s\n' "coyote-ci	coyote-ci	configmap/example	/tmp/outside.yaml" >"$corrupt_state/resources.tsv"
: >"$log"
if run_adopter ROLLBACK_FIXTURE_DIR="$fixture_dir" GKE_HELM_ADOPT_ROLLBACK_STATE="$corrupt_state" bash "$repo_root/scripts/gke-helm-adopt.sh" >/dev/null 2>"$temp_dir/corrupt.err"; then
  echo "outside snapshot path must fail" >&2
  exit 1
fi
grep -q 'adoption snapshot path is outside' "$temp_dir/corrupt.err" || {
  echo "outside snapshot did not produce a controlled error" >&2
  cat "$temp_dir/corrupt.err" >&2
  exit 1
}

relative_state="$temp_dir/relative-state"
mkdir -p "$relative_state/original"
cat >"$relative_state/original/relative.yaml" <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: relative
EOF
write_fixture coyote-ci configmap/relative adoption
printf '%s\n' "coyote-ci	coyote-ci	configmap/relative	original/relative.yaml" >"$relative_state/resources.tsv"
: >"$log"
reset_mock_state
run_adopter ROLLBACK_FIXTURE_DIR="$fixture_dir" GKE_HELM_ADOPT_ROLLBACK_STATE="$relative_state" bash "$repo_root/scripts/gke-helm-adopt.sh" >/dev/null
grep -q '^kubectl -n coyote-ci patch configmap/relative ' "$log" || {
  echo "relative snapshot beneath original was not restored" >&2
  exit 1
}

sibling_state="$temp_dir/sibling-state"
mkdir -p "$sibling_state/original" "$sibling_state/original-evil"
cat >"$sibling_state/original-evil/escape.yaml" <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: escape
EOF
printf '%s\n' "coyote-ci	coyote-ci	configmap/example	$sibling_state/original-evil/escape.yaml" >"$sibling_state/resources.tsv"
if run_adopter GKE_HELM_ADOPT_ROLLBACK_STATE="$sibling_state" bash "$repo_root/scripts/gke-helm-adopt.sh" >/dev/null 2>"$temp_dir/sibling.err"; then
  echo "sibling-prefix snapshot path must fail" >&2
  exit 1
fi
grep -q 'adoption snapshot path is outside' "$temp_dir/sibling.err" || {
  echo "sibling-prefix path did not produce a controlled error" >&2
  exit 1
}

traversal_state="$temp_dir/traversal-state"
mkdir -p "$traversal_state/original"
printf '%s\n' "coyote-ci	coyote-ci	configmap/example	original/../escape.yaml" >"$traversal_state/resources.tsv"
if run_adopter GKE_HELM_ADOPT_ROLLBACK_STATE="$traversal_state" bash "$repo_root/scripts/gke-helm-adopt.sh" >/dev/null 2>"$temp_dir/traversal.err"; then
  echo "traversal snapshot path must fail" >&2
  exit 1
fi
grep -q 'adoption snapshot path contains traversal' "$temp_dir/traversal.err" || {
  echo "traversal path did not produce a controlled error" >&2
  exit 1
}

symlink_state="$temp_dir/symlink-state"
mkdir -p "$symlink_state/original" "$temp_dir/outside"
cat >"$temp_dir/outside/escape.yaml" <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: escape
EOF
ln -s "$temp_dir/outside/escape.yaml" "$symlink_state/original/escape.yaml"
printf '%s\n' "coyote-ci	coyote-ci	configmap/example	$symlink_state/original/escape.yaml" >"$symlink_state/resources.tsv"
if run_adopter GKE_HELM_ADOPT_ROLLBACK_STATE="$symlink_state" bash "$repo_root/scripts/gke-helm-adopt.sh" >/dev/null 2>"$temp_dir/symlink.err"; then
  echo "symlink escape snapshot path must fail" >&2
  exit 1
fi
grep -q 'adoption snapshot path is outside' "$temp_dir/symlink.err" || {
  echo "symlink escape did not produce a controlled error" >&2
  exit 1
}

echo "GKE Helm adoption script tests passed"
