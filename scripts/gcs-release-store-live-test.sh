#!/usr/bin/env bash
set -euo pipefail

expected_009_digest='sha256:353b3599e99fc07c84a86fe89555b7d4b2f96a4ad213efca3085269138a3504c'
platform_component_version='0.0.0-platform.0.0.9'
mcp_component_version='0.0.0-mcp.1f45ec7.r4'

fail() {
  echo "gcs release-store live test: $*" >&2
  exit 2
}

require_env() {
  local name="$1" value="${!1:-}"
  [[ -n "$value" ]] || fail "$name is required"
}

expect_exit() {
  local expected="$1"
  shift
  local actual
  if "$@"; then
    actual=0
  else
    actual=$?
  fi
  [[ "$actual" == "$expected" ]] || fail "expected exit $expected, got $actual: $*"
}

immutable_image() {
  [[ "$1" =~ ^[^@]+/[^@/:]+@sha256:[a-f0-9]{64}$ ]]
}

if [[ "${COYOTE_GCS_LIVE_TEST:-}" != '1' ]]; then
  fail 'set COYOTE_GCS_LIVE_TEST=1 to permit isolated-prefix remote writes'
fi

require_env GCS_RELEASE_TEST_BUCKET
require_env GCS_RELEASE_TEST_MANIFEST_009
require_env GCS_RELEASE_TEST_MCP_IMAGE
require_env GCS_RELEASE_TEST_ARTIFACT_REGISTRY

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest_009="$GCS_RELEASE_TEST_MANIFEST_009"
bucket="$GCS_RELEASE_TEST_BUCKET"
mcp_image="$GCS_RELEASE_TEST_MCP_IMAGE"
registry="$GCS_RELEASE_TEST_ARTIFACT_REGISTRY"

[[ -f "$manifest_009" ]] || fail "GCS_RELEASE_TEST_MANIFEST_009 is not a regular file"
[[ "$bucket" != gs://* && "$bucket" != */* ]] || fail 'GCS_RELEASE_TEST_BUCKET must be a bucket name without a path'
[[ "$registry" != *[[:space:]]* ]] || fail 'GCS_RELEASE_TEST_ARTIFACT_REGISTRY must not contain whitespace'
immutable_image "$mcp_image" || fail 'GCS_RELEASE_TEST_MCP_IMAGE must be digest-pinned'

for command in go jq shasum cmp docker gcloud git; do
  command -v "$command" >/dev/null || fail "$command is required"
done

jq -e '
  .schema_version == 1 and
  .release == "0.0.9" and
  (.source.commit | type == "string") and
  (.source.created_at | type == "string") and
  (.images | type == "object")
' "$manifest_009" >/dev/null || fail 'GCS_RELEASE_TEST_MANIFEST_009 must be the recovered schema-v1 0.0.9 manifest'

for component in server worker frontend migrate; do
  image="$(jq -er --arg component "$component" '.images[$component]' "$manifest_009")" || fail "0.0.9 is missing $component image"
  immutable_image "$image" || fail "0.0.9 $component image is not digest-pinned"
done

actual_009_digest="sha256:$(shasum -a 256 "$manifest_009" | awk '{print $1}')"
[[ "$actual_009_digest" == "$expected_009_digest" ]] || fail "0.0.9 digest mismatch: $actual_009_digest"

temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT
run_id="gcs-release-preflight-$(date -u +%Y%m%d%H%M%S)-$$-$RANDOM"
release_source="gs://${bucket}/preflight/${run_id}"

(
  cd "$repo_root/backend"
  go build -o "$temp_dir/coyote" ./cmd/coyote
)
coyote="$temp_dir/coyote"

echo "Live GCS release-store test prefix: $release_source"
echo 'Remote objects are preserved for inspection.'

"$coyote" release import \
  --file "$manifest_009" \
  --release-source "$release_source" \
  --expected-digest "$expected_009_digest"

fetched_009="$temp_dir/fetched-0.0.9.json"
"$coyote" release fetch --version 0.0.9 --release-source "$release_source" --output "$fetched_009"
cmp -s "$manifest_009" "$fetched_009" || fail 'fetched 0.0.9 bytes differ from the recovered manifest'

"$coyote" release import \
  --file "$manifest_009" \
  --release-source "$release_source" \
  --expected-digest "$expected_009_digest"

conflicting_009="$temp_dir/conflicting-0.0.9.json"
conflicting_created_at='2030-01-01T00:00:00Z'
if [[ "$(jq -r '.source.created_at' "$manifest_009")" == "$conflicting_created_at" ]]; then
  conflicting_created_at='2030-01-01T00:00:01Z'
fi
jq --arg created_at "$conflicting_created_at" '.source.created_at = $created_at' "$manifest_009" >"$conflicting_009"
conflicting_digest="sha256:$(shasum -a 256 "$conflicting_009" | awk '{print $1}')"
expect_exit 1 "$coyote" release import --file "$conflicting_009" --release-source "$release_source" --expected-digest "$conflicting_digest"

missing_output="$temp_dir/missing.json"
expect_exit 3 "$coyote" release fetch --version 9.9.9 --release-source "$release_source" --output "$missing_output"
[[ ! -e "$missing_output" ]] || fail 'missing-manifest fetch created output'

invalid_output="$temp_dir/invalid.json"
expect_exit 2 "$coyote" release fetch --version '' --release-source "$release_source" --output "$invalid_output"
[[ ! -e "$invalid_output" ]] || fail 'invalid fetch created output'

RELEASE_PLAN=true \
RELEASE_SOURCE="$release_source" \
RELEASE_REGISTRY="$registry" \
PREVIOUS_RELEASE=0.0.9 VERSION=0.0.10 RELEASE_CHANNEL= \
COYOTE_RELEASE_CLI="$coyote" \
COMPONENT_SERVER_ACTION=reuse COMPONENT_SERVER_VERSION="$platform_component_version" \
COMPONENT_WORKER_ACTION=reuse COMPONENT_WORKER_VERSION="$platform_component_version" \
COMPONENT_FRONTEND_ACTION=reuse COMPONENT_FRONTEND_VERSION="$platform_component_version" \
COMPONENT_MIGRATE_ACTION=reuse COMPONENT_MIGRATE_VERSION="$platform_component_version" \
COMPONENT_MCP_ACTION=reuse COMPONENT_MCP_VERSION="$mcp_component_version" \
COMPONENT_MCP_IMAGE="$mcp_image" \
bash "$repo_root/scripts/release-build.sh"

plan_output="$temp_dir/unexpected-0.0.10.json"
expect_exit 3 "$coyote" release fetch --version 0.0.10 --release-source "$release_source" --output "$plan_output"
[[ ! -e "$plan_output" ]] || fail 'plan mode published 0.0.10'

write_disposable_manifest() {
  local version="$1" output="$2" created_at="$3"
  jq \
    --arg version "$version" \
    --arg created_at "$created_at" \
    --arg component_version "$platform_component_version" \
    --arg mcp_version "$mcp_component_version" \
    --arg mcp_image "$mcp_image" \
    '{
      schema_version: 2,
      release: $version,
      source: {commit: .source.commit, created_at: $created_at},
      components: {
        server: {version: $component_version, image: .images.server},
        worker: {version: $component_version, image: .images.worker},
        frontend: {version: $component_version, image: .images.frontend},
        mcp: {version: $mcp_version, image: $mcp_image},
        migrate: {version: $component_version, image: .images.migrate}
      }
    }' "$manifest_009" >"$output"
}

manifest_a="$temp_dir/0.0.11.json"
manifest_b="$temp_dir/0.0.12.json"
write_disposable_manifest 0.0.11 "$manifest_a" '2030-01-01T00:00:00Z'
write_disposable_manifest 0.0.12 "$manifest_b" '2030-01-01T00:00:01Z'

"$coyote" release publish --manifest "$manifest_a" --release-source "$release_source"
"$coyote" release publish --manifest "$manifest_b" --release-source "$release_source"

stored_a="$temp_dir/stored-0.0.11.json"
stored_b="$temp_dir/stored-0.0.12.json"
"$coyote" release fetch --version 0.0.11 --release-source "$release_source" --output "$stored_a"
"$coyote" release fetch --version 0.0.12 --release-source "$release_source" --output "$stored_b"
digest_a="sha256:$(shasum -a 256 "$stored_a" | awk '{print $1}')"
digest_b="sha256:$(shasum -a 256 "$stored_b" | awk '{print $1}')"

"$coyote" release publish --manifest "$manifest_a" --release-source "$release_source" --channel stable
"$coyote" release publish --manifest "$manifest_b" --release-source "$release_source" --channel stable

( "$coyote" release publish --manifest "$manifest_a" --release-source "$release_source" --channel latest ) &
publisher_a=$!
( "$coyote" release publish --manifest "$manifest_b" --release-source "$release_source" --channel latest ) &
publisher_b=$!
wait "$publisher_a"
wait "$publisher_b"

channels="$temp_dir/channels.json"
gcloud storage cat "$release_source/channels.json" >"$channels"
jq -e \
  --arg digest_a "$digest_a" \
  --arg digest_b "$digest_b" \
  '.schema_version == 1 and
   .channels.stable == {release: "0.0.12", manifest_digest: $digest_b} and
   ((.channels.latest.release == "0.0.11" and .channels.latest.manifest_digest == $digest_a) or
    (.channels.latest.release == "0.0.12" and .channels.latest.manifest_digest == $digest_b))' \
  "$channels" >/dev/null || fail 'channel index does not contain the expected stable/latest entries'

echo "Live GCS release-store test passed: $release_source"
echo 'Both concurrent publishers succeeded; this verifies a valid final channel state, not that a CAS collision occurred.'