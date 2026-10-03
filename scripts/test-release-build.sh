#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$temp_dir"
}
trap cleanup EXIT

assert_contains() {
  local value="$1" expected="$2"
  [[ "$value" == *"$expected"* ]] || { echo "expected $value to contain $expected" >&2; exit 1; }
}

write_git_mock() {
  cat >"$temp_dir/git" <<'EOF'
#!/usr/bin/env bash
if [[ "$*" == *"rev-parse HEAD"* ]]; then printf '%040d\n' 0 | tr '0' 'a'; exit 0; fi
if [[ "$*" == *"status --porcelain"* ]]; then printf '%s' "${TEST_GIT_STATUS:-}"; exit 0; fi
exit 1
EOF
  chmod +x "$temp_dir/git"
}

write_docker_mock() {
  cat >"$temp_dir/docker" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$TEST_DOCKER_LOG"
if [[ "$*" == *"imagetools inspect"* ]]; then
  case "$*" in
    *coyote-server*) printf 'sha256:%064d\n' 0 | tr '0' 'a' ;;
    *coyote-frontend*) printf 'sha256:%064d\n' 0 | tr '0' 'b' ;;
    *coyote-worker*) printf 'sha256:%064d\n' 0 | tr '0' 'c' ;;
    *coyote-migrate*) printf 'sha256:%064d\n' 0 | tr '0' 'd' ;;
  esac
fi
EOF
  chmod +x "$temp_dir/docker"
}

write_go_mock() {
  cat >"$temp_dir/go" <<'EOF'
#!/usr/bin/env bash
for ((index = 1; index <= $#; index++)); do
  if [[ "${!index}" == "--manifest" ]]; then
    next=$((index + 1))
    cp "${!next}" "$TEST_MANIFEST"
    exit 0
  fi
done
exit 1
EOF
  chmod +x "$temp_dir/go"
}

write_git_mock
write_docker_mock
write_go_mock

manifest="$temp_dir/manifest.json"
docker_log="$temp_dir/docker.log"
PATH="$temp_dir:$PATH" TEST_MANIFEST="$manifest" TEST_DOCKER_LOG="$docker_log" VERSION=2.5.1 RELEASE_REGISTRY=registry.example:5000/coyote RELEASE_SOURCE="$temp_dir/release-store" RELEASE_BUILD_DATE=2026-10-03T14:00:00Z bash "$repo_root/scripts/release-build.sh"

manifest_contents="$(cat "$manifest")"
assert_contains "$manifest_contents" '"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"'
assert_contains "$manifest_contents" 'registry.example:5000/coyote/coyote-server@sha256:'
assert_contains "$manifest_contents" 'registry.example:5000/coyote/coyote-frontend@sha256:'
if [[ "$manifest_contents" == *':2.5.1@sha256:'* ]]; then
  echo "manifest contains a tagged digest reference" >&2
  exit 1
fi
assert_contains "$(cat "$docker_log")" '--target source-runtime'
assert_contains "$(cat "$docker_log")" '--target migrate-runtime'

if PATH="$temp_dir:$PATH" TEST_GIT_STATUS='?? untracked-file' VERSION=2.5.1 RELEASE_REGISTRY=registry.example/coyote RELEASE_SOURCE="$temp_dir/release-store" bash "$repo_root/scripts/release-build.sh" >/dev/null 2>&1; then
  echo "expected untracked worktree rejection" >&2
  exit 1
fi
if PATH="$temp_dir:$PATH" RELEASE_COMMIT="$(printf '%040d' 0 | tr '0' 'b')" VERSION=2.5.1 RELEASE_REGISTRY=registry.example/coyote RELEASE_SOURCE="$temp_dir/release-store" bash "$repo_root/scripts/release-build.sh" >/dev/null 2>&1; then
  echo "expected mismatched release commit rejection" >&2
  exit 1
fi

echo "Release build script tests passed"
