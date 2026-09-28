#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT
log="$temp_dir/commands.log"
mkdir "$temp_dir/bin"

cat >"$temp_dir/bin/helm" <<'EOF'
#!/usr/bin/env bash
printf 'helm %s\n' "$*" >>"$COMMAND_LOG"
if [[ "$1" == "template" && "$*" == *"templates/gateway.yaml"* ]]; then
  exit 0
fi
if [[ "$1" == "template" && "$*" == *"templates/migration-job.yaml"* ]]; then
  printf '%s\n' 'apiVersion: batch/v1' 'kind: Job' 'metadata: {name: coyote-migrate-test-run}'
fi
EOF
cat >"$temp_dir/bin/kubectl" <<'EOF'
#!/usr/bin/env bash
printf 'kubectl %s\n' "$*" >>"$COMMAND_LOG"
if [[ "${KUBECTL_FAIL_WAIT:-false}" == "true" && "$*" == *"wait --for=condition=complete"* ]]; then
  exit 1
fi
EOF
chmod +x "$temp_dir/bin/helm" "$temp_dir/bin/kubectl"

PATH="$temp_dir/bin:$PATH" \
COMMAND_LOG="$log" \
HELM_ROLLOUT_ID=test-run \
GKE_HELM_DRY_RUN=false \
"$repo_root/scripts/gke-helm-rollout.sh" >/dev/null

line_number() {
  grep -n -m1 -- "$1" "$log" | cut -d: -f1
}

control_zero="$(line_number 'helm upgrade coyote-ci .*server.replicas=0')"
worker_zero="$(line_number 'helm upgrade coyote-ci-worker .*worker.replicas=0')"
migration_apply="$(line_number 'kubectl -n coyote-ci apply -f -')"
migration_wait="$(line_number 'kubectl -n coyote-ci wait --for=condition=complete job/coyote-migrate-test-run')"
control_restore="$(grep -n 'helm upgrade coyote-ci ' "$log" | tail -n1 | cut -d: -f1)"
worker_restore="$(grep -n 'helm upgrade coyote-ci-worker ' "$log" | tail -n1 | cut -d: -f1)"

[[ "$control_zero" -lt "$worker_zero" && "$worker_zero" -lt "$migration_apply" && "$migration_apply" -lt "$migration_wait" && "$migration_wait" -lt "$control_restore" && "$control_restore" -lt "$worker_restore" ]] || {
  echo "unexpected Helm rollout command sequence" >&2
  cat "$log" >&2
  exit 1
}

failure_log="$temp_dir/failure-commands.log"
if PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$failure_log" \
  HELM_ROLLOUT_ID=test-failure \
  KUBECTL_FAIL_WAIT=true \
  GKE_HELM_DRY_RUN=false \
  "$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>&1; then
  echo "migration wait failure must stop the rollout" >&2
  exit 1
fi
if grep -q 'helm upgrade coyote-ci .*--wait' "$failure_log" && grep -q 'helm upgrade coyote-ci-worker .*--wait' "$failure_log"; then
  restore_count="$(grep -c '^helm upgrade ' "$failure_log")"
  [[ "$restore_count" -eq 2 ]] || {
    echo "migration failure started a later rollout phase" >&2
    cat "$failure_log" >&2
    exit 1
  }
else
  echo "migration failure did not reach the expected wait phase" >&2
  cat "$failure_log" >&2
  exit 1
fi

echo "GKE Helm rollout sequencing test passed"
