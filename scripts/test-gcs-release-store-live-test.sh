#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output="$(mktemp)"
trap 'rm -f "$output"' EXIT

set +e
env -u COYOTE_GCS_LIVE_TEST bash "$repo_root/scripts/gcs-release-store-live-test.sh" >"$output" 2>&1
status=$?
set -e

[[ "$status" == 2 ]] || { cat "$output" >&2; echo "expected opt-in guard exit 2, got $status" >&2; exit 1; }
grep -Fq 'set COYOTE_GCS_LIVE_TEST=1' "$output" || { cat "$output" >&2; echo 'missing opt-in guard message' >&2; exit 1; }

set +e
env -i PATH="$PATH" COYOTE_GCS_LIVE_TEST=1 bash "$repo_root/scripts/gcs-release-store-live-test.sh" >"$output" 2>&1
status=$?
set -e

[[ "$status" == 2 ]] || { cat "$output" >&2; echo "expected configuration guard exit 2, got $status" >&2; exit 1; }
grep -Fq 'GCS_RELEASE_TEST_BUCKET is required' "$output" || { cat "$output" >&2; echo 'missing required configuration guard message' >&2; exit 1; }

echo 'GCS live-test guard checks passed'