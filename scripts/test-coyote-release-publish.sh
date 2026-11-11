#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT

commit_a="$(printf '%040d' 0 | tr '0' a)"
commit_b="$(printf '%040d' 0 | tr '0' b)"
digest() { printf 'sha256:%064d' 0 | tr '0' "$1"; }
assert_eq() { [[ "$1" == "$2" ]] || { echo "expected $2, got $1" >&2; exit 1; }; }
assert_empty() { [[ ! -s "$1" ]] || { echo "expected no mutation, found $(cat "$1")" >&2; exit 1; }; }
assert_not_contains() { [[ "$1" != *"$2"* ]] || { echo "unexpected output: $2" >&2; exit 1; }; }

cat >"$temp_dir/git" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
test_root="$(cd "$(dirname "$0")" && pwd)"
[[ -z "${GIT_DIR:-}" && -z "${GIT_WORK_TREE:-}" ]] || exit 99
if [[ "$1" == -C ]]; then shift 2; fi
case "$1" in
  status) cat "$test_root/git-status" ;;
  rev-parse) cat "$test_root/head-commit" ;;
  cat-file|merge-base) exit 0 ;;
  diff) cat "$test_root/changed-files" ;;
  *) exit 1 ;;
esac
EOF
cat >"$temp_dir/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
test_root="$(cd "$(dirname "$0")" && pwd)"
printf '%s\n' "$*" >>"$test_root/docker.log"
last="${!#}"
if [[ "$*" == *'buildx build'* ]]; then exit 0; fi
if [[ "$*" == *'imagetools create'* ]]; then
  count=0
  [[ -f "$test_root/promotion-count" ]] && count="$(cat "$test_root/promotion-count")"
  count=$((count + 1))
  printf '%s' "$count" >"$test_root/promotion-count"
  [[ "$(cat "$test_root/fail-promotion-at")" != "$count" ]] || exit 1
  printf '%s %s\n' "$5" "${last##*@}" >>"$test_root/tag-state"
  exit 0
fi
if [[ "$*" == *'imagetools inspect'* ]]; then
  if [[ "$last" == *'@sha256:'* ]]; then printf '%s\n' "${last##*@}"; exit 0; fi
  if [[ "$last" == *':build-'* ]]; then printf '%s\n' "$(printf 'sha256:%064d' 0 | tr '0' c)"; exit 0; fi
  tag_digest="$(awk -v tag="$last" '$1 == tag { print $2; exit }' "$test_root/tag-state")"
  [[ -z "$tag_digest" ]] || { printf '%s\n' "$tag_digest"; exit 0; }
  printf 'manifest unknown\n' >&2
  exit 1
fi
exit 1
EOF
cat >"$temp_dir/coyote" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
test_root="$(cd "$(dirname "$0")" && pwd)"
printf '%s\n' "$*" >>"$test_root/coyote.log"
command="$2"
output=""; channel=""; version=""; manifest=""; source=""
for ((index = 1; index <= $#; index++)); do
  case "${!index}" in
    --output) next=$((index + 1)); output="${!next}" ;;
    --channel) next=$((index + 1)); channel="${!next}" ;;
    --version) next=$((index + 1)); version="${!next}" ;;
    --manifest) next=$((index + 1)); manifest="${!next}" ;;
    --release-source) next=$((index + 1)); source="${!next}" ;;
  esac
done
if [[ "$command" == resolve ]]; then
  printf 'release:\n  version: %s\n' "$(cat "$test_root/predecessor-version")" >"$output"
  exit 0
fi
if [[ "$command" == fetch ]]; then
  if [[ -f "$(cat "$test_root/store-root")/releases/$version.json" ]]; then cp "$(cat "$test_root/store-root")/releases/$version.json" "$output"; exit 0; fi
  if [[ "$version" == "$(cat "$test_root/predecessor-version")" ]]; then cat "$test_root/predecessor-manifest" >"$output"; exit 0; fi
  echo "release document not found: $version" >&2
  exit 3
fi
if [[ "$command" == publish ]]; then
  release="$(jq -r '.release' "$manifest")"
  printf '%s\n' "$*" >>"$test_root/publish.log"
  mkdir -p "$(cat "$test_root/store-root")/releases"
  cp "$manifest" "$(cat "$test_root/store-root")/releases/$release.json"
  exit 0
fi
exit 1
EOF
chmod +x "$temp_dir/git" "$temp_dir/docker" "$temp_dir/coyote"

write_profile() {
  jq -n --arg source "$1" '{schema_version: 1, target: "production", release_source: $source, registry: "registry.example/coyote"}' >"$temp_dir/profile.json"
}

write_manifest() {
  jq -n --arg commit "$commit_a" --arg a "registry.example/coyote-server@$(digest a)" --arg b "registry.example/coyote-worker@$(digest b)" --arg c "registry.example/coyote-frontend@$(digest c)" --arg d "registry.example/coyote-mcp@$(digest d)" --arg e "registry.example/coyote-migrate@$(digest e)" '
    {schema_version: 2, release: "0.0.10", source: {commit: $commit, created_at: "2026-11-11T00:00:00Z"}, components: {
      server: {version: "0.0.10", image: $a}, worker: {version: "0.0.10", image: $b}, frontend: {version: "0.0.10", image: $c}, mcp: {version: "0.0.10", image: $d}, migrate: {version: "0.0.10", image: $e}
    }}'
}

reset_logs() {
  : >"$temp_dir/docker.log"
  : >"$temp_dir/coyote.log"
  : >"$temp_dir/publish.log"
  : >"$temp_dir/tag-state"
  : >"$temp_dir/promotion-count"
  printf '0' >"$temp_dir/fail-promotion-at"
}

reset_attempt_logs() {
  : >"$temp_dir/docker.log"
  : >"$temp_dir/coyote.log"
  : >"$temp_dir/publish.log"
}

run_facade() {
  printf '%s\n' "${TEST_HEAD_COMMIT:-$commit_b}" >"$temp_dir/head-commit"
  printf '%s\n' "${TEST_GIT_STATUS:-}" >"$temp_dir/git-status"
  printf '%s\n' "${TEST_CHANGED_FILES:-frontend/src/App.tsx}" >"$temp_dir/changed-files"
  printf '%s\n' "$TEST_PREDECESSOR_MANIFEST" >"$temp_dir/predecessor-manifest"
  printf '%s\n' "${TEST_PREDECESSOR_VERSION:-0.0.10}" >"$temp_dir/predecessor-version"
  printf '%s\n' "$temp_dir/store" >"$temp_dir/store-root"
  printf '%s\n' "${TEST_DOCKER_FAIL_PROMOTION_AT:-0}" >"$temp_dir/fail-promotion-at"
  PATH="$temp_dir:$PATH" COYOTE_RELEASE_CLI="$temp_dir/coyote" \
    bash "$repo_root/scripts/coyote-release" "$@"
}

write_profile "file://$temp_dir/store"
TEST_PREDECESSOR_MANIFEST="$(write_manifest)"
TEST_CHANGED_FILES='frontend/src/App.tsx'
reset_logs
run_facade plan --release 0.0.11 --from latest --target production --profile "$temp_dir/profile.json" --build-date 2026-11-11T12:00:00Z --output "$temp_dir/approved.json" >/dev/null
approved_hash="$(jq -r '.plan_sha256' "$temp_dir/approved.json")"
reset_logs
run_facade publish --plan "$temp_dir/approved.json" --expected-plan-sha256 "$approved_hash" --profile "$temp_dir/profile.json" >/dev/null
[[ -f "$temp_dir/store/releases/0.0.11.json" ]] || { echo 'expected delegated manifest publication' >&2; exit 1; }
[[ -s "$temp_dir/docker.log" ]] || { echo 'expected delegated Docker activity' >&2; exit 1; }
assert_not_contains "$(cat "$temp_dir/publish.log")" --channel

reset_attempt_logs
RELEASE_PLAN=true RELEASE_CHANNEL=stable RELEASE_BUILD_DATE=1999-01-01T00:00:00Z RELEASE_COMMIT="$commit_a" RELEASE_SOURCE=gs://wrong-source RELEASE_REGISTRY=wrong.registry/coyote PREVIOUS_RELEASE=9.9.9 COMPONENT_FRONTEND_ACTION=reuse COMPONENT_FRONTEND_VERSION=9.9.9 COMPONENT_FRONTEND_IMAGE="wrong.registry/coyote-frontend@$(digest f)" GIT_DIR=/wrong-git-dir GIT_WORK_TREE=/wrong-worktree run_facade publish --plan "$temp_dir/approved.json" --expected-plan-sha256 "$approved_hash" --profile "$temp_dir/profile.json" >/dev/null
assert_not_contains "$(cat "$temp_dir/publish.log")" --channel
assert_eq "$(jq -r '.source.commit' "$temp_dir/store/releases/0.0.11.json")" "$commit_b"
assert_eq "$(jq -r '.source.created_at' "$temp_dir/store/releases/0.0.11.json")" 2026-11-11T12:00:00Z
assert_eq "$(jq -r '.components.frontend.version' "$temp_dir/store/releases/0.0.11.json")" 0.0.11
assert_not_contains "$(cat "$temp_dir/coyote.log")" wrong-source
assert_not_contains "$(cat "$temp_dir/docker.log")" 'buildx build'

rejects_without_mutation() {
  local name="$1"
  shift
  reset_logs
  if run_facade publish "$@" --profile "$temp_dir/profile.json" >/dev/null 2>&1; then
    echo "expected $name rejection" >&2
    exit 1
  fi
  assert_empty "$temp_dir/docker.log"
  assert_empty "$temp_dir/publish.log"
}

rejects_without_mutation expected-hash --plan "$temp_dir/approved.json" --expected-plan-sha256 "sha256:$(printf '%064d' 0 | tr '0' f)"
jq '.release = "0.0.12"' "$temp_dir/approved.json" >"$temp_dir/tampered.json"
rejects_without_mutation embedded-hash --plan "$temp_dir/tampered.json" --expected-plan-sha256 "$approved_hash"
reset_logs
jq 'del(.plan_sha256) | .components.frontend.action = "manual_review"' "$temp_dir/approved.json" >"$temp_dir/manual-review-base.json"
manual_hash="sha256:$(jq -S -c . "$temp_dir/manual-review-base.json" | shasum -a 256 | awk '{print $1}')"
jq --arg hash "$manual_hash" '. + {plan_sha256: $hash}' "$temp_dir/manual-review-base.json" >"$temp_dir/manual-review.json"
if run_facade publish --plan "$temp_dir/manual-review.json" --expected-plan-sha256 "$manual_hash" --profile "$temp_dir/profile.json" >/dev/null 2>&1; then
  echo 'expected manual review plan rejection' >&2
  exit 1
fi
assert_empty "$temp_dir/docker.log"
assert_empty "$temp_dir/publish.log"

write_profile "file://$temp_dir/other-store"
rejects_without_mutation profile-drift --plan "$temp_dir/approved.json" --expected-plan-sha256 "$approved_hash"
write_profile "file://$temp_dir/store"
TEST_CHANGED_FILES='backend/internal/service/build/lifecycle.go'
rejects_without_mutation source-classification-drift --plan "$temp_dir/approved.json" --expected-plan-sha256 "$approved_hash"
TEST_CHANGED_FILES='frontend/src/App.tsx'
TEST_PREDECESSOR_MANIFEST="$(write_manifest | jq '.source.created_at = "2026-11-12T00:00:00Z"')"
rejects_without_mutation predecessor-drift --plan "$temp_dir/approved.json" --expected-plan-sha256 "$approved_hash"

TEST_PREDECESSOR_MANIFEST="$(write_manifest)"
reset_logs
run_facade plan --release 0.0.11 --from latest --target production --profile "$temp_dir/profile.json" --output "$temp_dir/no-date.json" >/dev/null
no_date_hash="$(jq -r '.plan_sha256' "$temp_dir/no-date.json")"
rejects_without_mutation missing-build-date --plan "$temp_dir/no-date.json" --expected-plan-sha256 "$no_date_hash"

TEST_GIT_STATUS=' M scripts/coyote-release'
rejects_without_mutation dirty-worktree --plan "$temp_dir/approved.json" --expected-plan-sha256 "$approved_hash"
unset TEST_GIT_STATUS
TEST_HEAD_COMMIT="$(printf '%040d' 0 | tr '0' c)"
rejects_without_mutation changed-head --plan "$temp_dir/approved.json" --expected-plan-sha256 "$approved_hash"
unset TEST_HEAD_COMMIT

reset_logs
run_facade plan --release 0.0.12 --from latest --target production --profile "$temp_dir/profile.json" --build-date 2026-11-11T12:00:00Z --output "$temp_dir/retry-plan.json" >/dev/null
retry_hash="$(jq -r '.plan_sha256' "$temp_dir/retry-plan.json")"
export TEST_DOCKER_FAIL_PROMOTION_AT=1
if run_facade publish --plan "$temp_dir/retry-plan.json" --expected-plan-sha256 "$retry_hash" --profile "$temp_dir/profile.json" >/dev/null 2>&1; then
  echo 'expected post-manifest tag promotion failure' >&2
  exit 1
fi
unset TEST_DOCKER_FAIL_PROMOTION_AT
[[ -f "$temp_dir/store/releases/0.0.12.json" ]] || { echo 'expected manifest before approved-plan retry' >&2; exit 1; }
reset_attempt_logs
run_facade publish --plan "$temp_dir/retry-plan.json" --expected-plan-sha256 "$retry_hash" --profile "$temp_dir/profile.json" >/dev/null
assert_not_contains "$(cat "$temp_dir/docker.log")" 'buildx build'
assert_not_contains "$(cat "$temp_dir/publish.log")" --channel

echo 'Coyote release publish tests passed'