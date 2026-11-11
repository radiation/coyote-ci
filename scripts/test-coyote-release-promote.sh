#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT

digest="sha256:$(printf '%064d' 0 | tr '0' a)"
assert_contains() { [[ "$1" == *"$2"* ]] || { echo "expected output to contain: $2" >&2; exit 1; }; }
assert_empty() { [[ ! -s "$1" ]] || { echo "expected no promotion, found $(cat "$1")" >&2; exit 1; }; }

cat >"$temp_dir/coyote" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
test_root="$(cd "$(dirname "$0")" && pwd)"
command="$2"
output=""; version=""; channel=""; source=""
for ((index = 1; index <= $#; index++)); do
  case "${!index}" in
    --output) next=$((index + 1)); output="${!next}" ;;
    --version|--release) next=$((index + 1)); version="${!next}" ;;
    --channel) next=$((index + 1)); channel="${!next}" ;;
    --release-source) next=$((index + 1)); source="${!next}" ;;
  esac
done
printf '%s\n' "$*" >>"$test_root/coyote.log"
if [[ "$command" == fetch ]]; then
  [[ "$version" == "$(cat "$test_root/release")" ]] || { echo "release document not found: $version" >&2; exit 3; }
  cat "$test_root/manifest.json" >"$output"
  exit 0
fi
[[ "$command" == promote-channel ]] || exit 1
lock="$test_root/channel.lock"
until mkdir "$lock" 2>/dev/null; do :; done
trap 'rmdir "$lock"' EXIT
previous_release="$(jq -r --arg channel "$channel" '.channels[$channel].release // empty' "$test_root/channels.json")"
previous_digest="$(jq -r --arg channel "$channel" '.channels[$channel].manifest_digest // empty' "$test_root/channels.json")"
changed=true
if [[ "$previous_release" == "$version" && "$previous_digest" == "$(cat "$test_root/digest")" ]]; then changed=false; fi
jq --arg channel "$channel" --arg release "$version" --arg digest "$(cat "$test_root/digest")" '.channels[$channel] = {release: $release, manifest_digest: $digest}' "$test_root/channels.json" >"$test_root/channels.next"
mv "$test_root/channels.next" "$test_root/channels.json"
printf '%s\n' "$channel $version" >>"$test_root/promotion.log"
printf 'Channel: %s\nPrevious target: %s\nNew target: %s (%s)\nManifest digest: %s\nChanged: %s\n' "$channel" "${previous_release:-<none>}" "$version" "$(cat "$test_root/digest")" "$(cat "$test_root/digest")" "$changed"
EOF
chmod +x "$temp_dir/coyote"

write_profile() {
  jq -n --arg source "$1" '{schema_version: 1, target: "production", release_source: $source, registry: "registry.example/coyote"}' >"$temp_dir/profile.json"
}

run_promote() {
  PATH="$temp_dir:$PATH" COYOTE_RELEASE_CLI="$temp_dir/coyote" \
    bash "$repo_root/scripts/coyote-release" promote --release "$1" --channel "$2" --target production --profile "$temp_dir/profile.json"
}

printf '%s\n' 0.0.11 >"$temp_dir/release"
printf '%s\n' "$digest" >"$temp_dir/digest"
jq -n --arg digest "$digest" '{schema_version: 2, release: "0.0.11", source: {commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", created_at: "2026-11-11T00:00:00Z"}, components: {server: {version: "0.0.11", image: ("registry.example/coyote-server@" + $digest)}, worker: {version: "0.0.11", image: ("registry.example/coyote-worker@" + $digest)}, frontend: {version: "0.0.11", image: ("registry.example/coyote-frontend@" + $digest)}, mcp: {version: "0.0.11", image: ("registry.example/coyote-mcp@" + $digest)}, migrate: {version: "0.0.11", image: ("registry.example/coyote-migrate@" + $digest)}}}' >"$temp_dir/manifest.json"
jq -n '{schema_version: 1, channels: {}}' >"$temp_dir/channels.json"
: >"$temp_dir/coyote.log"
: >"$temp_dir/promotion.log"
write_profile "file://$temp_dir/store"

output="$(run_promote 0.0.11 latest)"
assert_contains "$output" 'Previous target: <none>'
assert_contains "$output" 'New target: 0.0.11'
assert_contains "$output" "Manifest digest: $digest"
assert_contains "$output" 'Changed: true'
assert_contains "$(cat "$temp_dir/coyote.log")" 'release fetch --version 0.0.11 --release-source file://'
assert_contains "$(cat "$temp_dir/coyote.log")" 'release promote-channel --release 0.0.11 --channel latest --release-source file://'

output="$(run_promote 0.0.11 latest)"
assert_contains "$output" 'Changed: false'

: >"$temp_dir/promotion.log"
if run_promote 0.0.12 latest >/dev/null 2>&1; then
  echo 'expected missing release rejection' >&2
  exit 1
fi
assert_empty "$temp_dir/promotion.log"
if run_promote 0.0.11 preview >/dev/null 2>&1; then
  echo 'expected invalid channel rejection' >&2
  exit 1
fi
assert_empty "$temp_dir/promotion.log"

jq '.release_source = "gs://bad source"' "$temp_dir/profile.json" >"$temp_dir/invalid-profile.json"
if PATH="$temp_dir:$PATH" COYOTE_RELEASE_CLI="$temp_dir/coyote" bash "$repo_root/scripts/coyote-release" promote --release 0.0.11 --channel latest --target production --profile "$temp_dir/invalid-profile.json" >/dev/null 2>&1; then
  echo 'expected invalid profile rejection' >&2
  exit 1
fi
assert_empty "$temp_dir/promotion.log"

write_profile 'gs://release-bucket/releases'
output="$(run_promote 0.0.11 stable)"
assert_contains "$output" 'Changed: true'
assert_contains "$(cat "$temp_dir/coyote.log")" 'release-source gs://release-bucket/releases'

: >"$temp_dir/promotion.log"
run_promote 0.0.11 latest >"$temp_dir/latest.out" &
latest_pid=$!
run_promote 0.0.11 stable >"$temp_dir/stable.out" &
stable_pid=$!
wait "$latest_pid"
wait "$stable_pid"
[[ "$(jq -r '.channels.latest.release' "$temp_dir/channels.json")" == 0.0.11 ]] || { echo 'latest update was lost' >&2; exit 1; }
[[ "$(jq -r '.channels.stable.release' "$temp_dir/channels.json")" == 0.0.11 ]] || { echo 'stable update was lost' >&2; exit 1; }

echo 'Coyote release promotion tests passed'