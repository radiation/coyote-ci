#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT
mkdir -p "$temp_dir/bin" "$temp_dir/migrations"
log="$temp_dir/commands.log"

assert_read_only() {
  ! grep -Eq '(^| )(apply|create|delete|patch|replace|scale|upgrade|install|rollout|wait|exec)( |$)' "$log" || {
    cat "$log" >&2
    echo 'migration evidence reporter issued a mutation command' >&2
    exit 1
  }
}

cat >"$temp_dir/bin/kubectl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'kubectl %s\n' "$*" >>"$COMMAND_LOG"
if [[ "$*" == 'config current-context' ]]; then printf '%s\n' "${KUBECTL_CONTEXT:-production-context}"; exit 0; fi
if [[ "$*" == *'get namespace kube-system'* ]]; then printf '%s\n' "${KUBECTL_CLUSTER_UID:-production-uid}"; exit 0; fi
if [[ "$*" == *'get jobs -l app.kubernetes.io/name=coyote-migrate'* ]]; then
  case "${JOB_FIXTURE:-missing-lineage}" in
    missing-lineage) cat "$JOB_RESPONSE" ;;
    lineage-metadata) cat "$LINEAGE_METADATA_JOB_RESPONSE" ;;
    incomplete) cat "$INCOMPLETE_JOB_RESPONSE" ;;
    ambiguous) cat "$AMBIGUOUS_JOB_RESPONSE" ;;
    malformed) cat "$MALFORMED_JOB_RESPONSE" ;;
  esac
  exit 0
fi
exit 1
EOF

cat >"$temp_dir/bin/helm" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'helm %s\n' "$*" >>"$COMMAND_LOG"
[[ "$*" == *" status "* ]] || exit 1
printf '%s\n' '{"name":"coyote-ci","version":27,"info":{"status":"deployed"}}'
EOF
chmod +x "$temp_dir/bin/kubectl" "$temp_dir/bin/helm"

printf '%s\n' '-- migration fixture' >"$temp_dir/migrations/00001_init.sql"
printf '%s\n' '-- migration fixture' >"$temp_dir/migrations/00002_next.sql"

cat >"$temp_dir/profile.json" <<'EOF'
{"schema_version":1,"target":"production","kubernetes":{"context":"production-context","cluster_uid":"production-uid","control_plane_release":"coyote-ci"}}
EOF
cat >"$temp_dir/checkpoint.json" <<'EOF'
{"schema_version":1,"rollout_id":"coyote-9","target_environment":"staging","context":"production-context","cluster_uid":"production-uid","control_namespace":"coyote-ci","control_release":"coyote-ci","migration_job":"coyote-migrate-coyote-9","migration_status":"complete"}
EOF
cat >"$temp_dir/ledger.json" <<'EOF'
[{"id":1,"version_id":1,"is_applied":true,"tstamp":"2026-10-01T00:00:00Z"},{"id":2,"version_id":2,"is_applied":true,"tstamp":"2026-10-02T00:00:00Z"},{"id":3,"version_id":2,"is_applied":false,"tstamp":"2026-10-03T00:00:00Z"},{"id":4,"version_id":2,"is_applied":true,"tstamp":"2026-10-04T00:00:00Z"},{"id":5,"version_id":99,"is_applied":true,"tstamp":"2026-10-05T00:00:00Z"}]
EOF
cat >"$temp_dir/rollback-ledger.json" <<'EOF'
[{"id":1,"version_id":1,"is_applied":true,"tstamp":"2026-10-01T00:00:00Z"},{"id":2,"version_id":2,"is_applied":true,"tstamp":"2026-10-02T00:00:00Z"},{"id":3,"version_id":2,"is_applied":false,"tstamp":"2026-10-03T00:00:00Z"}]
EOF
cat >"$temp_dir/duplicate-id-ledger.json" <<'EOF'
[{"id":1,"version_id":1,"is_applied":true,"tstamp":"2026-10-01T00:00:00Z"},{"id":1,"version_id":2,"is_applied":true,"tstamp":"2026-10-02T00:00:00Z"}]
EOF
cat >"$temp_dir/missing-sequence-ledger.json" <<'EOF'
[{"version_id":1,"is_applied":true,"tstamp":"2026-10-01T00:00:00Z"}]
EOF

valid_image="registry.example/coyote-migrate@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
cat >"$temp_dir/jobs.json" <<EOF
{"items":[{"metadata":{"name":"coyote-migrate-old","uid":"job-uid","namespace":"coyote-ci","labels":{"app.kubernetes.io/name":"coyote-migrate"},"annotations":{}},"spec":{"template":{"spec":{"containers":[{"name":"migrate","image":"$valid_image"}]}}},"status":{"completionTime":"2026-10-01T00:00:00Z","conditions":[{"type":"Complete","status":"True"}]}}]}
EOF
jq '.items[0].metadata.annotations["evidence.example/change"] = "changed"' "$temp_dir/jobs.json" >"$temp_dir/changed-jobs.json"
jq '.items[0].metadata.annotations["coyote.dev/rollout-id"] = "historical"' "$temp_dir/jobs.json" >"$temp_dir/lineage-metadata-jobs.json"
cat >"$temp_dir/incomplete-jobs.json" <<EOF
{"items":[{"metadata":{"name":"coyote-migrate-running","uid":"running-uid"},"spec":{"template":{"spec":{"containers":[{"name":"migrate","image":"$valid_image"}]}}},"status":{"conditions":[]}}]}
EOF
cat >"$temp_dir/ambiguous-jobs.json" <<EOF
{"items":[{"metadata":{"name":"coyote-migrate-duplicate","uid":"duplicate-uid"},"spec":{"template":{"spec":{"containers":[{"name":"migrate","image":"$valid_image"}]}}},"status":{"completionTime":"2026-10-01T00:00:00Z","conditions":[{"type":"Complete","status":"True"}]}},{"metadata":{"name":"coyote-migrate-duplicate","uid":"other-uid"},"spec":{"template":{"spec":{"containers":[{"name":"migrate","image":"$valid_image"}]}}},"status":{"completionTime":"2026-10-01T00:00:00Z","conditions":[{"type":"Complete","status":"True"}]}}]}
EOF
cat >"$temp_dir/malformed-jobs.json" <<'EOF'
{"items":[{"metadata":{"name":"coyote-migrate-bad","uid":"bad-uid"},"spec":{"template":{"spec":{"containers":[{"name":"migrate","image":"latest"}]}}},"status":{"completionTime":"2026-10-01T00:00:00Z","conditions":[{"type":"Complete","status":"True"}]}}]}
EOF

run_report() {
  : >"$log"
  local job_response="${TEST_JOB_RESPONSE:-$temp_dir/jobs.json}"
  local ledger="${TEST_LEDGER_FILE:-$temp_dir/ledger.json}"
  JOB_RESPONSE="$job_response" LINEAGE_METADATA_JOB_RESPONSE="$temp_dir/lineage-metadata-jobs.json" INCOMPLETE_JOB_RESPONSE="$temp_dir/incomplete-jobs.json" AMBIGUOUS_JOB_RESPONSE="$temp_dir/ambiguous-jobs.json" MALFORMED_JOB_RESPONSE="$temp_dir/malformed-jobs.json" \
    PATH="$temp_dir/bin:$PATH" COMMAND_LOG="$log" \
    bash "$repo_root/scripts/gke-migration-evidence.sh" \
      --profile "$temp_dir/profile.json" \
      --checkpoint "$temp_dir/checkpoint.json" \
      --goose-ledger-file "$ledger" \
      --migrations-dir "$temp_dir/migrations"
}

output="$(run_report)"
jq -e '.read_only == true and .environment_identity.status == "verified" and .topology.scope == "control-plane-only" and .topology.worker_release == null and .checkpoint_identity.status == "identity-mismatch" and .migration_jobs[0].status == "complete-missing-lineage" and (.migration_jobs[0].fingerprint | test("^sha256:[0-9a-f]{64}$")) and .goose_ledger.status == "available" and .goose_ledger.applied_versions_missing_from_source == [99] and .goose_ledger.source_versions_not_applied == [] and (.goose_ledger.migration_states[] | select(.version_id == 2) | .is_applied == true and .sequence_id == 4)' <<<"$output" >/dev/null
grep -q -- '--context production-context -n coyote-ci get jobs' "$log"
grep -q -- '--kube-context production-context -n coyote-ci status coyote-ci' "$log"
assert_read_only

first_fingerprint="$(jq -r '.migration_jobs[0].fingerprint' <<<"$output")"
second_fingerprint="$(jq -r '.migration_jobs[0].fingerprint' <<<"$(run_report)")"
[[ "$first_fingerprint" == "$second_fingerprint" ]] || { echo 'canonical Job fingerprint is not stable' >&2; exit 1; }
changed_fingerprint="$(jq -r '.migration_jobs[0].fingerprint' <<<"$(TEST_JOB_RESPONSE="$temp_dir/changed-jobs.json" run_report)")"
[[ "$first_fingerprint" != "$changed_fingerprint" ]] || { echo 'relevant Job evidence did not change fingerprint' >&2; exit 1; }

output="$(JOB_FIXTURE=lineage-metadata run_report)"
jq -e '.migration_jobs[0].status == "complete-unverified-lineage" and .migration_jobs[0].evidence.lineage_status == "metadata-present-unverified"' <<<"$output" >/dev/null
assert_read_only

output="$(TEST_LEDGER_FILE="$temp_dir/rollback-ledger.json" run_report)"
jq -e '.goose_ledger.applied_versions == [1] and .goose_ledger.source_versions_not_applied == [2] and (.goose_ledger.migration_states[] | select(.version_id == 2) | .is_applied == false and .sequence_id == 3)' <<<"$output" >/dev/null
assert_read_only

output="$(TEST_LEDGER_FILE="$temp_dir/duplicate-id-ledger.json" run_report)"
jq -e '.goose_ledger.status == "malformed" and (.goose_ledger.errors | join(" ") | contains("duplicate sequence IDs"))' <<<"$output" >/dev/null
assert_read_only

output="$(TEST_LEDGER_FILE="$temp_dir/missing-sequence-ledger.json" run_report)"
jq -e '.goose_ledger.status == "malformed" and (.goose_ledger.errors | join(" ") | contains("positive integer id"))' <<<"$output" >/dev/null
assert_read_only

output="$(JOB_FIXTURE=incomplete run_report)"
jq -e '.migration_jobs[0].status == "incomplete"' <<<"$output" >/dev/null
assert_read_only

output="$(JOB_FIXTURE=ambiguous run_report)"
jq -e 'all(.migration_jobs[]; .status == "ambiguous" and (.errors | index("duplicate Job name in response")))' <<<"$output" >/dev/null
assert_read_only

output="$(JOB_FIXTURE=malformed run_report)"
jq -e '.migration_jobs[0].status == "malformed" and (.migration_jobs[0].errors | index("migration image must be an immutable SHA-256 reference"))' <<<"$output" >/dev/null
assert_read_only

if KUBECTL_CONTEXT=wrong-context run_report >/dev/null 2>&1; then
  echo 'expected context mismatch rejection' >&2
  exit 1
fi
grep -q 'kubectl config current-context' "$log"
! grep -Eq 'get namespace|get jobs|helm ' "$log" || { cat "$log" >&2; echo 'context mismatch performed evidence queries' >&2; exit 1; }

if KUBECTL_CLUSTER_UID=wrong-uid run_report >/dev/null 2>&1; then
  echo 'expected cluster UID mismatch rejection' >&2
  exit 1
fi
grep -q -- 'kubectl --context production-context get namespace kube-system' "$log"
! grep -Eq 'get jobs|helm ' "$log" || { cat "$log" >&2; echo 'cluster UID mismatch performed evidence queries' >&2; exit 1; }

: >"$log"
output="$(JOB_RESPONSE="$temp_dir/jobs.json" INCOMPLETE_JOB_RESPONSE="$temp_dir/incomplete-jobs.json" AMBIGUOUS_JOB_RESPONSE="$temp_dir/ambiguous-jobs.json" MALFORMED_JOB_RESPONSE="$temp_dir/malformed-jobs.json" PATH="$temp_dir/bin:$PATH" COMMAND_LOG="$log" bash "$repo_root/scripts/gke-migration-evidence.sh" --profile "$temp_dir/profile.json" --migrations-dir "$temp_dir/migrations")"
jq -e '.goose_ledger.status == "unavailable"' <<<"$output" >/dev/null
assert_read_only

echo 'GKE migration evidence tests passed'