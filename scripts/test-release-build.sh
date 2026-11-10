#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT

digest() { printf 'sha256:%064d' 0 | tr '0' "$1"; }
assert_contains() { [[ "$1" == *"$2"* ]] || { echo "expected output to contain: $2" >&2; exit 1; }; }
assert_not_contains() { [[ "$1" != *"$2"* ]] || { echo "unexpected output: $2" >&2; exit 1; }; }
canonical_path() { cd "$1" && pwd -P; }

cat >"$temp_dir/git" <<'EOF'
#!/usr/bin/env bash
if [[ "$*" == *"rev-parse HEAD"* ]]; then printf '%040d\n' 0 | tr '0' "${TEST_GIT_COMMIT_CHAR:-a}"; exit 0; fi
if [[ "$*" == *"status --porcelain"* ]]; then printf '%s' "${TEST_GIT_STATUS:-}"; exit 0; fi
exit 1
EOF
cat >"$temp_dir/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$TEST_DOCKER_LOG"
last="${!#}"
if [[ "$*" == *"buildx build"* && "${TEST_DOCKER_FAIL_BUILD:-}" == true ]]; then exit 1; fi
if [[ "$*" == *"imagetools create"* ]]; then
  count=0
  [[ -f "$TEST_PROMOTION_COUNT" ]] && count="$(cat "$TEST_PROMOTION_COUNT")"
  count=$((count + 1)); printf '%s' "$count" >"$TEST_PROMOTION_COUNT"
  if [[ "${TEST_DOCKER_FAIL_PROMOTION_AT:-0}" == "$count" ]]; then exit 1; fi
  printf '%s %s\n' "$5" "${last##*@}" >>"$TEST_TAG_STATE"
  exit 0
fi
if [[ "$*" != *"imagetools inspect"* ]]; then exit 0; fi
if [[ "$last" == *":${TEST_COLLISION_VERSION:-never}" ]]; then printf '%s\n' "${TEST_COLLISION_DIGEST:-sha256:$(printf '%064d' 0 | tr '0' f)}"; exit 0; fi
if [[ -f "$TEST_TAG_STATE" ]]; then
  tag_digest="$(awk -v tag="$last" '$1 == tag { print $2; exit }' "$TEST_TAG_STATE")"
  [[ -z "$tag_digest" ]] || { printf '%s\n' "$tag_digest"; exit 0; }
fi
if [[ "$last" == *"@sha256:"* ]]; then printf '%s\n' "${last##*@}"; exit 0; fi
if [[ "$last" == *":build-"* ]]; then
  case "$last" in
    *coyote-server*) printf 'sha256:%064d\n' 0 | tr '0' 'a' ;;
    *coyote-worker*) printf 'sha256:%064d\n' 0 | tr '0' 'b' ;;
    *coyote-frontend*) printf 'sha256:%064d\n' 0 | tr '0' 'c' ;;
    *coyote-mcp*) printf 'sha256:%064d\n' 0 | tr '0' 'd' ;;
    *coyote-migrate*) printf 'sha256:%064d\n' 0 | tr '0' 'e' ;;
  esac
  exit 0
fi
printf 'manifest unknown: %s\n' "$last" >&2
exit 1
EOF
cat >"$temp_dir/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$TEST_GO_LOG"
if [[ "$1" == build ]]; then
  output=""
  for ((i = 1; i <= $#; i++)); do
    if [[ "${!i}" == -o ]]; then
      next=$((i + 1))
      output="${!next}"
      break
    fi
  done
  [[ -n "$output" ]] || exit 1
  cp "$0" "$output"
  chmod +x "$output"
  exit 0
fi
command=""; manifest=""; release_source=""; release_version=""; output=""; channel=false
for argument in "$@"; do
  if [[ "$argument" == fetch || "$argument" == publish ]]; then
    command="$argument"
  fi
done
for ((i = 1; i <= $#; i++)); do
  if [[ "${!i}" == --manifest ]]; then
    next=$((i + 1))
    manifest="${!next}"
  fi
  if [[ "${!i}" == --version ]]; then
    next=$((i + 1))
    release_version="${!next}"
  fi
  if [[ "${!i}" == --output ]]; then
    next=$((i + 1))
    output="${!next}"
  fi
  if [[ "${!i}" == --release-source ]]; then
    next=$((i + 1))
    release_source="${!next}"
  fi
  if [[ "${!i}" == --channel ]]; then
    channel=true
  fi
done
[[ -n "$release_source" ]] || exit 1
store_root="${TEST_RELEASE_STORE_ROOT:-$release_source}"
if [[ "$command" == fetch ]]; then
  [[ -n "$release_version" && -n "$output" ]] || exit 1
  if [[ -n "${TEST_GO_FETCH_ERROR:-}" && ( -z "${TEST_GO_FETCH_ERROR_VERSION:-}" || "$release_version" == "$TEST_GO_FETCH_ERROR_VERSION" ) ]]; then
    case "$TEST_GO_FETCH_ERROR" in
      missing) echo "release document not found: $release_version" >&2; exit 3 ;;
      permission) echo "${TEST_GO_FETCH_ERROR_TEXT:-permission denied fetching release manifest: $release_version}" >&2 ;;
      transient) echo "temporary release store failure: $release_version" >&2 ;;
      *) exit 1 ;;
    esac
	  exit 1
  fi
  source_manifest="$store_root/releases/$release_version.json"
  [[ -f "$source_manifest" ]] || { echo "release document not found: $release_version" >&2; exit 3; }
  cp "$source_manifest" "$output.tmp"
  mv "$output.tmp" "$output"
  exit 0
fi
[[ "$command" == publish && -n "$manifest" ]] || exit 1
cp "$manifest" "$TEST_MANIFEST"
if [[ "$channel" == false ]]; then
  [[ "${TEST_GO_MANIFEST_FAIL:-}" != true ]] || exit 1
  mkdir -p "$store_root/releases"
  cp "$manifest" "$store_root/releases/0.0.10.json"
else
  [[ "${TEST_GO_CHANNEL_FAIL:-}" != true ]] || exit 1
  printf 'advanced\n' >>"$TEST_CHANNEL_LOG"
fi
EOF
chmod +x "$temp_dir/git" "$temp_dir/docker" "$temp_dir/go"

write_v2_previous() {
  local store="$1"
  mkdir -p "$store/releases"
  cat >"$store/releases/0.0.9.json" <<EOF
{"schema_version":2,"release":"0.0.9","source":{"commit":"$(printf '%040d' 0 | tr '0' a)","created_at":"2026-10-01T00:00:00Z"},"components":{"server":{"version":"1.0.0","image":"registry.old/coyote-server@$(digest a)"},"worker":{"version":"1.0.0","image":"registry.old/coyote-worker@$(digest b)"},"frontend":{"version":"1.0.0","image":"registry.old/coyote-frontend@$(digest c)"},"mcp":{"version":"1.0.0","image":"registry.old/coyote-mcp@$(digest d)"},"migrate":{"version":"1.0.0","image":"registry.old/coyote-migrate@$(digest e)"}}}
EOF
}

write_v1_previous() {
  local store="$1"
  mkdir -p "$store/releases"
  cat >"$store/releases/0.0.9.json" <<EOF
{"schema_version":1,"release":"0.0.9","source":{"commit":"$(printf '%040d' 0 | tr '0' a)","created_at":"2026-10-01T00:00:00Z"},"images":{"server":"registry.old/coyote-server@$(digest a)","worker":"registry.old/coyote-worker@$(digest b)","frontend":"registry.old/coyote-frontend@$(digest c)","migrate":"registry.old/coyote-migrate@$(digest e)"}}
EOF
}

run_release() {
  local store="$1" manifest="$2" docker_log="$3" go_log="$4" state_name tag_state promotion_count channel_log store_root release_channel=latest release_source="$1" argument
  local -a extra_env=()
	local -a env_command=()
  shift 4
  for argument in "$@"; do
    case "$argument" in
      RELEASE_CHANNEL=*) release_channel="${argument#RELEASE_CHANNEL=}" ;;
      RELEASE_SOURCE=*) release_source="${argument#RELEASE_SOURCE=}" ;;
      *) extra_env+=("$argument") ;;
    esac
  done
  state_name="${TEST_STATE_NAME:-default}"
  tag_state="$temp_dir/tag-state-$state_name"
  promotion_count="$temp_dir/promotion-count-$state_name"
  channel_log="$temp_dir/channel-log-$state_name"
  : >"$docker_log"
  : >"$go_log"
  if [[ "${TEST_PRESERVE_STATE:-false}" != true ]]; then
    : >"$tag_state"
    : >"$promotion_count"
    : >"$channel_log"
  fi
  store_root="${TEST_RELEASE_STORE_ROOT:-$store}"
  env_command=(env PATH="$temp_dir:$PATH" TEST_MANIFEST="$manifest" TEST_DOCKER_LOG="$docker_log" TEST_GO_LOG="$go_log" \
    TEST_TAG_STATE="$tag_state" TEST_PROMOTION_COUNT="$promotion_count" TEST_CHANNEL_LOG="$channel_log" \
    TEST_RELEASE_STORE_ROOT="$store_root" \
    VERSION=0.0.10 RELEASE_REGISTRY=registry.example/coyote RELEASE_SOURCE="$release_source" PREVIOUS_RELEASE=0.0.9 \
    RELEASE_BUILD_DATE=2026-11-05T00:00:00Z RELEASE_CHANNEL="$release_channel" \
    COMPONENT_SERVER_ACTION=build COMPONENT_SERVER_VERSION=1.1.0 \
    COMPONENT_WORKER_ACTION=build COMPONENT_WORKER_VERSION=1.1.1 \
    COMPONENT_FRONTEND_ACTION=build COMPONENT_FRONTEND_VERSION=2.0.0 \
    COMPONENT_MCP_ACTION=build COMPONENT_MCP_VERSION=0.2.0 \
    COMPONENT_MIGRATE_ACTION=build COMPONENT_MIGRATE_VERSION=3.0.0)
  if [[ ${#extra_env[@]} -gt 0 ]]; then
    env_command+=("${extra_env[@]}")
  fi
  env_command+=(bash "$repo_root/scripts/release-build.sh")
  "${env_command[@]}"
}

# All components built: v2 manifest, MCP, separate server/worker build arguments, and component tags.
store="$temp_dir/all-built-store"; write_v2_previous "$store"
manifest="$temp_dir/all-built.json"; docker_log="$temp_dir/all-built.docker"; go_log="$temp_dir/all-built.go"
run_release "$store" "$manifest" "$docker_log" "$go_log"
contents="$(cat "$manifest")"; log="$(cat "$docker_log")"
assert_contains "$contents" '"schema_version": 2'; assert_contains "$contents" '"mcp"'; assert_contains "$contents" '"version": "1.1.0"'
assert_contains "$log" '--target source-runtime'; assert_contains "$log" 'COYOTE_SERVER_VERSION=1.1.0'; assert_contains "$log" 'COYOTE_WORKER_VERSION=1.1.1'; assert_contains "$log" 'COYOTE_APPLICATION_VERSION=0.2.0'
assert_contains "$log" 'imagetools create --tag registry.example/coyote/coyote-mcp:0.2.0'
go_calls="$(cat "$go_log")"; store_location="$(canonical_path "$store")"
assert_contains "$go_calls" 'build -o '
assert_contains "$go_calls" "release fetch --version 0.0.9 --release-source $store_location"
assert_contains "$go_calls" "release fetch --version 0.0.10 --release-source $store_location"
assert_not_contains "$go_calls" 'run ./cmd/coyote'
[[ "$(grep -c '^build -o ' "$go_log")" == 1 ]] || { echo 'expected one default release CLI build' >&2; exit 1; }

# A caller-supplied compiled CLI avoids the temporary build.
store="$temp_dir/compiled-cli-store"; write_v2_previous "$store"
manifest="$temp_dir/compiled-cli.json"; docker_log="$temp_dir/compiled-cli.docker"; go_log="$temp_dir/compiled-cli.go"
run_release "$store" "$manifest" "$docker_log" "$go_log" COYOTE_RELEASE_CLI="$temp_dir/go"
fetch_log="$(grep 'release fetch' "$go_log")"
assert_contains "$fetch_log" 'release fetch --version 0.0.9'
assert_not_contains "$fetch_log" 'run ./cmd/coyote'
assert_not_contains "$(cat "$go_log")" 'build -o '

# Relative local and file:// release sources are resolved from the caller's directory for both CLI paths.
store="$temp_dir/relative-source-store"; write_v2_previous "$store"
caller_dir="$temp_dir/relative-source-caller"; mkdir -p "$caller_dir"
manifest="$temp_dir/relative-source.json"; docker_log="$temp_dir/relative-source.docker"; go_log="$temp_dir/relative-source.go"
pushd "$caller_dir" >/dev/null
run_release "$store" "$manifest" "$docker_log" "$go_log" RELEASE_SOURCE=../relative-source-store
popd >/dev/null
assert_contains "$(cat "$go_log")" "--release-source $(canonical_path "$store")"

store="$temp_dir/file-url-source-store"; write_v2_previous "$store"
manifest="$temp_dir/file-url-source.json"; docker_log="$temp_dir/file-url-source.docker"; go_log="$temp_dir/file-url-source.go"
pushd "$caller_dir" >/dev/null
run_release "$store" "$manifest" "$docker_log" "$go_log" COYOTE_RELEASE_CLI="$temp_dir/go" RELEASE_SOURCE=file://../file-url-source-store
popd >/dev/null
assert_contains "$(cat "$go_log")" "--release-source file://$(canonical_path "$store")"

# Mixed release: server/worker reuse while the other components build; reused components never build, push, or tag.
store="$temp_dir/mixed-store"; write_v2_previous "$store"
manifest="$temp_dir/mixed.json"; docker_log="$temp_dir/mixed.docker"; go_log="$temp_dir/mixed.go"
run_release "$store" "$manifest" "$docker_log" "$go_log" \
  COMPONENT_SERVER_ACTION=reuse COMPONENT_SERVER_VERSION=1.0.0 \
  COMPONENT_WORKER_ACTION=reuse COMPONENT_WORKER_VERSION=1.0.0
log="$(cat "$docker_log")"; contents="$(cat "$manifest")"
assert_contains "$contents" "registry.old/coyote-server@$(digest a)"; assert_contains "$contents" "registry.old/coyote-worker@$(digest b)"
assert_not_contains "$log" 'coyote-server:build-'; assert_not_contains "$log" 'coyote-worker:build-'; assert_not_contains "$log" 'coyote-server:1.0.0'; assert_not_contains "$log" 'coyote-worker:1.0.0'

# All reuse produces a new platform manifest without any mutating Docker commands and preserves old digests.
store="$temp_dir/reuse-store"; write_v2_previous "$store"
manifest="$temp_dir/reuse.json"; docker_log="$temp_dir/reuse.docker"; go_log="$temp_dir/reuse.go"
run_release "$store" "$manifest" "$docker_log" "$go_log" \
  COMPONENT_SERVER_ACTION=reuse COMPONENT_SERVER_VERSION=1.0.0 \
  COMPONENT_WORKER_ACTION=reuse COMPONENT_WORKER_VERSION=1.0.0 \
  COMPONENT_FRONTEND_ACTION=reuse COMPONENT_FRONTEND_VERSION=1.0.0 \
  COMPONENT_MCP_ACTION=reuse COMPONENT_MCP_VERSION=1.0.0 \
  COMPONENT_MIGRATE_ACTION=reuse COMPONENT_MIGRATE_VERSION=1.0.0
log="$(cat "$docker_log")"; assert_not_contains "$log" 'buildx build'; assert_not_contains "$log" 'imagetools create'; assert_contains "$(cat "$manifest")" "registry.old/coyote-mcp@$(digest d)"

# Missing action, coupled-decision mismatch, and missing predecessor are configuration failures.
store="$temp_dir/rejections-store"; write_v2_previous "$store"
if run_release "$store" "$temp_dir/reject.json" "$temp_dir/reject.docker" "$temp_dir/reject.go" COMPONENT_MCP_ACTION=; then echo 'expected missing action rejection' >&2; exit 1; fi
if run_release "$store" "$temp_dir/reject.json" "$temp_dir/reject.docker" "$temp_dir/reject.go" COMPONENT_SERVER_ACTION=reuse; then echo 'expected server/worker coupling rejection' >&2; exit 1; fi
if run_release "$temp_dir/no-store" "$temp_dir/reject.json" "$temp_dir/reject.docker" "$temp_dir/reject.go"; then echo 'expected missing predecessor rejection' >&2; exit 1; fi

# v1-to-v2 reuse requires explicit initial component versions and an explicit MCP bootstrap image.
store="$temp_dir/v1-store"; write_v1_previous "$store"
manifest="$temp_dir/v1.json"; docker_log="$temp_dir/v1.docker"; go_log="$temp_dir/v1.go"
run_release "$store" "$manifest" "$docker_log" "$go_log" \
  COMPONENT_SERVER_ACTION=reuse COMPONENT_SERVER_VERSION=1.0.0 \
  COMPONENT_WORKER_ACTION=reuse COMPONENT_WORKER_VERSION=1.0.0 \
  COMPONENT_FRONTEND_ACTION=reuse COMPONENT_FRONTEND_VERSION=1.0.0 \
  COMPONENT_MIGRATE_ACTION=reuse COMPONENT_MIGRATE_VERSION=1.0.0 \
  COMPONENT_MCP_ACTION=reuse COMPONENT_MCP_VERSION=0.1.0 COMPONENT_MCP_IMAGE="registry.bootstrap/coyote-mcp@$(digest d)"
assert_contains "$(cat "$manifest")" '"schema_version": 2'; assert_contains "$(cat "$manifest")" 'registry.bootstrap/coyote-mcp@sha256:'
assert_contains "$(cat "$go_log")" "release fetch --version 0.0.9 --release-source $(canonical_path "$store")"

# A semantic component tag resolving to different content fails before the platform manifest publishes.
store="$temp_dir/collision-store"; write_v2_previous "$store"
if run_release "$store" "$temp_dir/collision.json" "$temp_dir/collision.docker" "$temp_dir/collision.go" TEST_COLLISION_VERSION=2.0.0 TEST_COLLISION_DIGEST="$(digest f)"; then echo 'expected tag collision rejection' >&2; exit 1; fi
[[ ! -f "$temp_dir/collision.json" ]] || { echo 'manifest published after collision' >&2; exit 1; }

# Plan mode resolves and validates, but does not build, push, tag, or publish.
store="$temp_dir/plan-store"; write_v2_previous "$store"
manifest="$temp_dir/plan.json"; docker_log="$temp_dir/plan.docker"; go_log="$temp_dir/plan.go"
plan="$(run_release "$store" "$manifest" "$docker_log" "$go_log" RELEASE_PLAN=true)"
assert_contains "$plan" 'Release plan'; assert_contains "$plan" 'platform_version=0.0.10'; assert_contains "$plan" 'constraint=server-worker actions are coupled'
[[ ! -f "$manifest" ]] || { echo 'plan published a manifest' >&2; exit 1; }
assert_contains "$(cat "$go_log")" 'release fetch --version 0.0.9'
assert_contains "$(cat "$go_log")" 'release fetch --version 0.0.10'
assert_not_contains "$(cat "$go_log")" 'release publish'
log="$(cat "$docker_log")"; assert_not_contains "$log" 'buildx build'; assert_not_contains "$log" 'imagetools create'

# Build or publication failure never reaches a platform manifest or component tag promotion.
store="$temp_dir/failure-store"; write_v2_previous "$store"
if run_release "$store" "$temp_dir/build-failure.json" "$temp_dir/build-failure.docker" "$temp_dir/build-failure.go" TEST_DOCKER_FAIL_BUILD=true; then echo 'expected build failure' >&2; exit 1; fi
[[ ! -f "$temp_dir/build-failure.json" ]] || { echo 'manifest published after build failure' >&2; exit 1; }
if run_release "$store" "$temp_dir/publish-failure.json" "$temp_dir/publish-failure.docker" "$temp_dir/publish-failure.go" TEST_GO_MANIFEST_FAIL=true; then echo 'expected publish failure' >&2; exit 1; fi
assert_not_contains "$(cat "$temp_dir/publish-failure.docker")" 'imagetools create'

# Publishing builds requires a caller-owned stable timestamp, while planning can report it as a blocker without mutations.
store="$temp_dir/date-store"; write_v2_previous "$store"
if run_release "$store" "$temp_dir/date.json" "$temp_dir/date.docker" "$temp_dir/date.go" RELEASE_BUILD_DATE=; then echo 'expected missing build date rejection' >&2; exit 1; fi
plan="$(run_release "$store" "$temp_dir/date-plan.json" "$temp_dir/date-plan.docker" "$temp_dir/date-plan.go" RELEASE_PLAN=true RELEASE_BUILD_DATE=)"
assert_contains "$plan" 'blocker=RELEASE_BUILD_DATE is required'; assert_not_contains "$(cat "$temp_dir/date-plan.docker")" 'buildx build'

# A v2 predecessor image cannot be reused under a new component version.
store="$temp_dir/v2-version-store"; write_v2_previous "$store"
if run_release "$store" "$temp_dir/v2-version.json" "$temp_dir/v2-version.docker" "$temp_dir/v2-version.go" COMPONENT_SERVER_ACTION=reuse COMPONENT_SERVER_VERSION=1.0.1 COMPONENT_WORKER_ACTION=reuse COMPONENT_WORKER_VERSION=1.0.0; then echo 'expected v2 reuse version rejection' >&2; exit 1; fi

# A failed second tag promotion leaves a manifest but no channel; retry resumes from its immutable records without rebuilding.
store="$temp_dir/promotion-retry-store"; write_v2_previous "$store"
if TEST_STATE_NAME=promotion-retry run_release "$store" "$temp_dir/promotion-first.json" "$temp_dir/promotion-first.docker" "$temp_dir/promotion-first.go" TEST_DOCKER_FAIL_PROMOTION_AT=2; then echo 'expected second promotion failure' >&2; exit 1; fi
[[ -f "$store/releases/0.0.10.json" ]] || { echo 'expected manifest before promotion retry' >&2; exit 1; }
TEST_STATE_NAME=promotion-retry TEST_PRESERVE_STATE=true run_release "$store" "$temp_dir/promotion-retry.json" "$temp_dir/promotion-retry.docker" "$temp_dir/promotion-retry.go"
assert_not_contains "$(cat "$temp_dir/promotion-retry.docker")" 'buildx build'; assert_contains "$(cat "$temp_dir/channel-log-promotion-retry")" 'advanced'

# Channel failure occurs only after tags are promoted; retry advances the channel without rebuilding or changing the manifest.
store="$temp_dir/channel-retry-store"; write_v2_previous "$store"
if TEST_STATE_NAME=channel-retry run_release "$store" "$temp_dir/channel-first.json" "$temp_dir/channel-first.docker" "$temp_dir/channel-first.go" TEST_GO_CHANNEL_FAIL=true; then echo 'expected channel failure' >&2; exit 1; fi
[[ -f "$store/releases/0.0.10.json" ]] || { echo 'expected manifest before channel retry' >&2; exit 1; }
assert_contains "$(cat "$temp_dir/channel-first.docker")" 'imagetools create --tag registry.example/coyote/coyote-migrate:3.0.0'
TEST_STATE_NAME=channel-retry TEST_PRESERVE_STATE=true run_release "$store" "$temp_dir/channel-retry.json" "$temp_dir/channel-retry.docker" "$temp_dir/channel-retry.go"
assert_not_contains "$(cat "$temp_dir/channel-retry.docker")" 'buildx build'; assert_contains "$(cat "$temp_dir/channel-log-channel-retry")" 'advanced'

# Existing immutable manifests reject a conflicting requested component configuration or source commit before any rebuild.
store="$temp_dir/existing-conflict-store"; write_v2_previous "$store"
TEST_STATE_NAME=existing-conflict run_release "$store" "$temp_dir/existing-first.json" "$temp_dir/existing-first.docker" "$temp_dir/existing-first.go"
if TEST_STATE_NAME=existing-conflict TEST_PRESERVE_STATE=true run_release "$store" "$temp_dir/existing-conflict.json" "$temp_dir/existing-conflict.docker" "$temp_dir/existing-conflict.go" COMPONENT_FRONTEND_VERSION=2.0.1; then echo 'expected existing manifest configuration conflict' >&2; exit 1; fi
assert_not_contains "$(cat "$temp_dir/existing-conflict.docker")" 'buildx build'
if TEST_STATE_NAME=existing-conflict TEST_PRESERVE_STATE=true run_release "$store" "$temp_dir/existing-commit.json" "$temp_dir/existing-commit.docker" "$temp_dir/existing-commit.go" TEST_GIT_COMMIT_CHAR=b; then echo 'expected existing manifest commit conflict' >&2; exit 1; fi
assert_not_contains "$(cat "$temp_dir/existing-commit.docker")" 'buildx build'

# A new all-reuse release may generate its timestamp; retries adopt it when omitted and reject explicit conflicts.
store="$temp_dir/all-reuse-date-store"; write_v2_previous "$store"
TEST_STATE_NAME=all-reuse-date run_release "$store" "$temp_dir/all-reuse-first.json" "$temp_dir/all-reuse-first.docker" "$temp_dir/all-reuse-first.go" \
  RELEASE_BUILD_DATE= \
  COMPONENT_SERVER_ACTION=reuse COMPONENT_SERVER_VERSION=1.0.0 \
  COMPONENT_WORKER_ACTION=reuse COMPONENT_WORKER_VERSION=1.0.0 \
  COMPONENT_FRONTEND_ACTION=reuse COMPONENT_FRONTEND_VERSION=1.0.0 \
  COMPONENT_MCP_ACTION=reuse COMPONENT_MCP_VERSION=1.0.0 \
  COMPONENT_MIGRATE_ACTION=reuse COMPONENT_MIGRATE_VERSION=1.0.0
existing_date="$(jq -er '.source.created_at' "$store/releases/0.0.10.json")"
TEST_STATE_NAME=all-reuse-date TEST_PRESERVE_STATE=true run_release "$store" "$temp_dir/all-reuse-retry.json" "$temp_dir/all-reuse-retry.docker" "$temp_dir/all-reuse-retry.go" \
  RELEASE_BUILD_DATE= \
  COMPONENT_SERVER_ACTION=reuse COMPONENT_SERVER_VERSION=1.0.0 \
  COMPONENT_WORKER_ACTION=reuse COMPONENT_WORKER_VERSION=1.0.0 \
  COMPONENT_FRONTEND_ACTION=reuse COMPONENT_FRONTEND_VERSION=1.0.0 \
  COMPONENT_MCP_ACTION=reuse COMPONENT_MCP_VERSION=1.0.0 \
  COMPONENT_MIGRATE_ACTION=reuse COMPONENT_MIGRATE_VERSION=1.0.0
assert_not_contains "$(cat "$temp_dir/all-reuse-retry.docker")" 'buildx build'
channel_before="$(wc -l <"$temp_dir/channel-log-all-reuse-date")"
tags_before="$(wc -l <"$temp_dir/tag-state-all-reuse-date")"
if TEST_STATE_NAME=all-reuse-date TEST_PRESERVE_STATE=true run_release "$store" "$temp_dir/all-reuse-conflict.json" "$temp_dir/all-reuse-conflict.docker" "$temp_dir/all-reuse-conflict.go" \
  RELEASE_BUILD_DATE=2026-11-06T00:00:00Z \
  COMPONENT_SERVER_ACTION=reuse COMPONENT_SERVER_VERSION=1.0.0 \
  COMPONENT_WORKER_ACTION=reuse COMPONENT_WORKER_VERSION=1.0.0 \
  COMPONENT_FRONTEND_ACTION=reuse COMPONENT_FRONTEND_VERSION=1.0.0 \
  COMPONENT_MCP_ACTION=reuse COMPONENT_MCP_VERSION=1.0.0 \
  COMPONENT_MIGRATE_ACTION=reuse COMPONENT_MIGRATE_VERSION=1.0.0; then echo 'expected all-reuse timestamp conflict' >&2; exit 1; fi
[[ "$channel_before" == "$(wc -l <"$temp_dir/channel-log-all-reuse-date")" ]] || { echo 'channel advanced after timestamp conflict' >&2; exit 1; }
[[ "$tags_before" == "$(wc -l <"$temp_dir/tag-state-all-reuse-date")" ]] || { echo 'component tag advanced after timestamp conflict' >&2; exit 1; }
TEST_STATE_NAME=all-reuse-date TEST_PRESERVE_STATE=true run_release "$store" "$temp_dir/all-reuse-matching.json" "$temp_dir/all-reuse-matching.docker" "$temp_dir/all-reuse-matching.go" \
  RELEASE_BUILD_DATE="$existing_date" \
  COMPONENT_SERVER_ACTION=reuse COMPONENT_SERVER_VERSION=1.0.0 \
  COMPONENT_WORKER_ACTION=reuse COMPONENT_WORKER_VERSION=1.0.0 \
  COMPONENT_FRONTEND_ACTION=reuse COMPONENT_FRONTEND_VERSION=1.0.0 \
  COMPONENT_MCP_ACTION=reuse COMPONENT_MCP_VERSION=1.0.0 \
  COMPONENT_MIGRATE_ACTION=reuse COMPONENT_MIGRATE_VERSION=1.0.0
assert_not_contains "$(cat "$temp_dir/all-reuse-matching.docker")" 'buildx build'

# GCS locations use the same CLI fetch contract; plans perform only reads against the provider-neutral source.
gcs_store="$temp_dir/gcs-store"; gcs_source="gs://release-bucket/release-metadata"; write_v2_previous "$gcs_store"
plan="$(TEST_RELEASE_STORE_ROOT="$gcs_store" run_release "$gcs_source" "$temp_dir/gcs-reuse.json" "$temp_dir/gcs-reuse.docker" "$temp_dir/gcs-reuse.go" RELEASE_PLAN=true \
  COMPONENT_SERVER_ACTION=reuse COMPONENT_SERVER_VERSION=1.0.0 \
  COMPONENT_WORKER_ACTION=reuse COMPONENT_WORKER_VERSION=1.0.0 \
  COMPONENT_FRONTEND_ACTION=reuse COMPONENT_FRONTEND_VERSION=1.0.0 \
  COMPONENT_MCP_ACTION=reuse COMPONENT_MCP_VERSION=1.0.0 \
  COMPONENT_MIGRATE_ACTION=reuse COMPONENT_MIGRATE_VERSION=1.0.0)"
assert_contains "$plan" 'Release plan'; assert_contains "$(cat "$temp_dir/gcs-reuse.go")" "release fetch --version 0.0.9 --release-source $gcs_source"
[[ ! -f "$temp_dir/gcs-reuse.json" ]] || { echo 'GCS plan published a manifest' >&2; exit 1; }

gcs_store="$temp_dir/gcs-mixed-store"; gcs_source="gs://release-bucket/mixed"; write_v2_previous "$gcs_store"
plan="$(TEST_RELEASE_STORE_ROOT="$gcs_store" run_release "$gcs_source" "$temp_dir/gcs-mixed.json" "$temp_dir/gcs-mixed.docker" "$temp_dir/gcs-mixed.go" RELEASE_PLAN=true \
  COMPONENT_SERVER_ACTION=reuse COMPONENT_SERVER_VERSION=1.0.0 \
  COMPONENT_WORKER_ACTION=reuse COMPONENT_WORKER_VERSION=1.0.0)"
assert_contains "$plan" 'component=server action=reuse'; assert_not_contains "$(cat "$temp_dir/gcs-mixed.docker")" 'buildx build'

# An immutable v2 GCS manifest makes retries use the stored records without rebuilding.
gcs_store="$temp_dir/gcs-retry-store"; gcs_source="gs://release-bucket/retry"; write_v2_previous "$gcs_store"
TEST_STATE_NAME=gcs-retry TEST_RELEASE_STORE_ROOT="$gcs_store" run_release "$gcs_source" "$temp_dir/gcs-first.json" "$temp_dir/gcs-first.docker" "$temp_dir/gcs-first.go"
TEST_STATE_NAME=gcs-retry TEST_PRESERVE_STATE=true TEST_RELEASE_STORE_ROOT="$gcs_store" run_release "$gcs_source" "$temp_dir/gcs-retry.json" "$temp_dir/gcs-retry.docker" "$temp_dir/gcs-retry.go"
assert_contains "$(cat "$temp_dir/gcs-retry.go")" "release fetch --version 0.0.10 --release-source $gcs_source"
assert_not_contains "$(cat "$temp_dir/gcs-retry.docker")" 'buildx build'

# Only a documented missing response permits a new release. Permission and transient fetch failures stop before publication.
store="$temp_dir/fetch-failure-store"; write_v2_previous "$store"
if TEST_GO_FETCH_ERROR=permission run_release "$store" "$temp_dir/fetch-permission.json" "$temp_dir/fetch-permission.docker" "$temp_dir/fetch-permission.go"; then echo 'expected permission fetch failure' >&2; exit 1; fi
[[ ! -f "$temp_dir/fetch-permission.json" ]] || { echo 'manifest published after permission failure' >&2; exit 1; }
if TEST_GO_FETCH_ERROR=transient run_release "$store" "$temp_dir/fetch-transient.json" "$temp_dir/fetch-transient.docker" "$temp_dir/fetch-transient.go"; then echo 'expected transient fetch failure' >&2; exit 1; fi
[[ ! -f "$temp_dir/fetch-transient.json" ]] || { echo 'manifest published after transient failure' >&2; exit 1; }
if TEST_GO_FETCH_ERROR=permission TEST_GO_FETCH_ERROR_VERSION=0.0.10 run_release "$store" "$temp_dir/fetch-existing-permission.json" "$temp_dir/fetch-existing-permission.docker" "$temp_dir/fetch-existing-permission.go"; then echo 'expected existing manifest permission failure' >&2; exit 1; fi
[[ ! -f "$temp_dir/fetch-existing-permission.json" ]] || { echo 'manifest published after existing manifest permission failure' >&2; exit 1; }
if TEST_GO_FETCH_ERROR=missing TEST_GO_FETCH_ERROR_VERSION=0.0.9 run_release "$store" "$temp_dir/fetch-missing.json" "$temp_dir/fetch-missing.docker" "$temp_dir/fetch-missing.go"; then echo 'expected predecessor missing failure' >&2; exit 1; fi
if TEST_GO_FETCH_ERROR=permission TEST_GO_FETCH_ERROR_VERSION=0.0.10 run_release "$store" "$temp_dir/fetch-diagnostic.json" "$temp_dir/fetch-diagnostic.docker" "$temp_dir/fetch-diagnostic.go" TEST_GO_FETCH_ERROR_TEXT='release document not found in permission detail'; then echo 'expected diagnostic permission fetch failure' >&2; exit 1; fi
[[ ! -f "$temp_dir/fetch-diagnostic.json" ]] || { echo 'manifest published after diagnostic permission failure' >&2; exit 1; }

# Disabled channels publish only the immutable manifest.
store="$temp_dir/no-channel-store"; write_v2_previous "$store"
run_release "$store" "$temp_dir/no-channel.json" "$temp_dir/no-channel.docker" "$temp_dir/no-channel.go" RELEASE_CHANNEL=
assert_not_contains "$(cat "$temp_dir/no-channel.go")" --channel
[[ ! -s "$temp_dir/channel-log-default" ]] || { echo 'disabled channel advanced' >&2; exit 1; }

echo 'Release build script tests passed'
