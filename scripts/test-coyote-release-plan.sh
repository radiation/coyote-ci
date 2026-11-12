#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT

commit_a="$(printf '%040d' 0 | tr '0' a)"
commit_b="$(printf '%040d' 0 | tr '0' b)"
digest() { printf 'sha256:%064d' 0 | tr '0' "$1"; }
assert_eq() { [[ "$1" == "$2" ]] || { echo "expected $2, got $1" >&2; exit 1; }; }
assert_contains() { [[ "$1" == *"$2"* ]] || { echo "expected output to contain: $2" >&2; exit 1; }; }

cat >"$temp_dir/git" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$1" in
  -C) shift 2 ;;
esac
case "$1" in
  status) exit 0 ;;
  rev-parse) printf '%s\n' "${TEST_HEAD_COMMIT}" ;;
  cat-file) [[ "${TEST_PREDECESSOR_AVAILABLE:-true}" == true ]] ;;
  merge-base) [[ "${TEST_PREDECESSOR_ANCESTOR:-true}" == true ]] ;;
  diff)
    [[ "$2" == --no-renames && "$3" == --name-only ]] || exit 1
    [[ "${TEST_DIFF_FAILURE:-false}" != true ]] || exit 1
    printf '%s\n' "${TEST_CHANGED_FILES:-}"
    ;;
  *) exit 1 ;;
esac
EOF
cat >"$temp_dir/coyote" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$TEST_COYOTE_LOG"
command="$2"
output=""
channel=""
version=""
for ((index = 1; index <= $#; index++)); do
  if [[ "${!index}" == --output ]]; then next=$((index + 1)); output="${!next}"; fi
  if [[ "${!index}" == --channel ]]; then next=$((index + 1)); channel="${!next}"; fi
  if [[ "${!index}" == --version ]]; then next=$((index + 1)); version="${!next}"; fi
done
if [[ "$command" == resolve ]]; then
  [[ "$channel" == latest ]] || exit 1
  printf 'release:\n  version: %s\n' "${TEST_PREDECESSOR_VERSION}" >"$output"
  exit 0
fi
[[ "$command" == fetch && "$version" == "${TEST_PREDECESSOR_VERSION}" ]] || exit 1
printf '%s\n' "$TEST_MANIFEST" >"$output"
EOF
chmod +x "$temp_dir/git" "$temp_dir/coyote"

profile="$temp_dir/production.json"
write_profile() {
  local source="$1"
  jq -n --arg source "$source" '{schema_version: 1, target: "production", release_source: $source, registry: "registry.example/coyote"}' >"$profile"
}

write_v2_manifest() {
  jq -n --arg commit "$commit_a" --arg a "registry.example/coyote-server@$(digest a)" --arg b "registry.example/coyote-worker@$(digest b)" --arg c "registry.example/coyote-frontend@$(digest c)" --arg d "registry.example/coyote-mcp@$(digest d)" --arg e "registry.example/coyote-migrate@$(digest e)" '
    {schema_version: 2, release: "0.0.10", source: {commit: $commit, created_at: "2026-11-11T00:00:00Z"}, components: {
      server: {version: "0.0.10", image: $a}, worker: {version: "0.0.10", image: $b}, frontend: {version: "0.0.10", image: $c}, mcp: {version: "0.0.10", image: $d}, migrate: {version: "0.0.10", image: $e}
    }}'
}

write_v1_manifest() {
  jq -n --arg commit "$commit_a" --arg a "registry.example/coyote-server@$(digest a)" --arg b "registry.example/coyote-worker@$(digest b)" --arg c "registry.example/coyote-frontend@$(digest c)" --arg e "registry.example/coyote-migrate@$(digest e)" '
    {schema_version: 1, release: "0.0.10", source: {commit: $commit, created_at: "2026-11-11T00:00:00Z"}, images: {server: $a, worker: $b, frontend: $c, migrate: $e}}'
}

run_plan() {
  local name="$1"
  shift
  TEST_HEAD_COMMIT="$commit_b" TEST_PREDECESSOR_VERSION=0.0.10 TEST_MANIFEST="$TEST_MANIFEST" TEST_CHANGED_FILES="${TEST_CHANGED_FILES:-}" TEST_DIFF_FAILURE="${TEST_DIFF_FAILURE:-false}" TEST_COYOTE_LOG="$temp_dir/$name.coyote" \
    PATH="$temp_dir:$PATH" COYOTE_RELEASE_CLI="$temp_dir/coyote" \
    bash "$repo_root/scripts/coyote-release" plan --release 0.0.11 --from latest --target production --profile "$profile" --output "$temp_dir/$name.json" "$@"
}

write_profile "file://$temp_dir/release-store"
TEST_MANIFEST="$(write_v2_manifest)"
TEST_CHANGED_FILES=""
run_plan all-reuse >/dev/null
for component in server worker frontend mcp migrate; do
  assert_eq "$(jq -r ".components.$component.action" "$temp_dir/all-reuse.json")" reuse
done
assert_eq "$(jq -r '.fixed_inputs.build_date' "$temp_dir/all-reuse.json")" null
assert_contains "$(cat "$temp_dir/all-reuse.coyote")" 'release resolve --channel latest --release-source file://'
assert_contains "$(cat "$temp_dir/all-reuse.coyote")" 'release fetch --version 0.0.10'

TEST_CHANGED_FILES='frontend/src/App.tsx'
run_plan frontend >/dev/null
assert_eq "$(jq -r '.components.frontend.action' "$temp_dir/frontend.json")" build
assert_eq "$(jq -r '.components.server.action' "$temp_dir/frontend.json")" reuse
jq -e '.fixed_inputs.build_date | type == "string" and test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$")' "$temp_dir/frontend.json" >/dev/null

TEST_CHANGED_FILES=$'Makefile\ndeploy/release-targets/example.json\nscripts/gke-helm-rollout.sh\nbackend/internal/cli/release.go'
run_plan tooling-only >/dev/null
for component in server worker frontend mcp migrate; do
  assert_eq "$(jq -r ".components.$component.action" "$temp_dir/tooling-only.json")" reuse
done

TEST_CHANGED_FILES=$'README.md\ndocs/release-planning.md\nbackend/docs/swagger.yaml'
run_plan documentation-only >/dev/null
for component in server worker frontend mcp migrate; do
  assert_eq "$(jq -r ".components.$component.action" "$temp_dir/documentation-only.json")" reuse
done

TEST_CHANGED_FILES='backend/docs/docs.go'
run_plan generated-server-docs >/dev/null
assert_eq "$(jq -r '.components.server.action' "$temp_dir/generated-server-docs.json")" build
assert_eq "$(jq -r '.components.worker.action' "$temp_dir/generated-server-docs.json")" build
assert_eq "$(jq -r '.components.mcp.action' "$temp_dir/generated-server-docs.json")" reuse

TEST_CHANGED_FILES=$'frontend/src/App.tsx\nbackend/cmd/mcp/main.go'
run_plan cross-component-move >/dev/null
assert_eq "$(jq -r '.components.frontend.action' "$temp_dir/cross-component-move.json")" build
assert_eq "$(jq -r '.components.mcp.action' "$temp_dir/cross-component-move.json")" build

TEST_CHANGED_FILES=$'frontend/src/App.tsx\nbackend/go.mod'
run_plan mixed-backend-frontend >/dev/null
for component in server worker frontend mcp; do
  assert_eq "$(jq -r ".components.$component.action" "$temp_dir/mixed-backend-frontend.json")" build
done
assert_eq "$(jq -r '.components.migrate.action' "$temp_dir/mixed-backend-frontend.json")" reuse

TEST_CHANGED_FILES='backend/Dockerfile'
run_plan migration-dockerfile >/dev/null
assert_eq "$(jq -r '.components.migrate.action' "$temp_dir/migration-dockerfile.json")" build

TEST_CHANGED_FILES='frontend/src/App.tsx'
if TEST_DIFF_FAILURE=true run_plan diff-failure >/dev/null 2>&1; then
  echo 'expected git diff failure to abort release planning' >&2
  exit 1
fi

TEST_CHANGED_FILES='backend/internal/service/build/lifecycle.go'
run_plan shared-backend >/dev/null
assert_eq "$(jq -r '.components.server.action' "$temp_dir/shared-backend.json")" build
assert_eq "$(jq -r '.components.worker.action' "$temp_dir/shared-backend.json")" build
assert_eq "$(jq -r '.components.mcp.action' "$temp_dir/shared-backend.json")" build

TEST_CHANGED_FILES='backend/cmd/mcp/main.go'
run_plan mcp >/dev/null
assert_eq "$(jq -r '.components.mcp.action' "$temp_dir/mcp.json")" build
assert_eq "$(jq -r '.components.server.action' "$temp_dir/mcp.json")" reuse

TEST_CHANGED_FILES='backend/db/migrations/0001_init.sql'
run_plan migration >/dev/null
assert_eq "$(jq -r '.components.migrate.action' "$temp_dir/migration.json")" build
assert_eq "$(jq -r '.components.frontend.action' "$temp_dir/migration.json")" reuse

TEST_CHANGED_FILES='.goreleaser.yml'
run_plan unknown >/dev/null
for component in server worker frontend mcp migrate; do
  assert_eq "$(jq -r ".components.$component.action" "$temp_dir/unknown.json")" manual_review
done

TEST_CHANGED_FILES='.goreleaser.yml'
run_plan forced --force-build frontend --force-reuse server >/dev/null
assert_eq "$(jq -r '.components.frontend.action' "$temp_dir/forced.json")" build
assert_eq "$(jq -r '.components.server.action' "$temp_dir/forced.json")" reuse
assert_eq "$(jq -r '.components.worker.action' "$temp_dir/forced.json")" reuse
assert_eq "$(jq -r '.components.server.classification.action' "$temp_dir/forced.json")" manual_review
assert_eq "$(jq -r '.components.server.reason' "$temp_dir/forced.json")" 'unknown_source_change:.goreleaser.yml'
assert_eq "$(jq -r '.components.server.override.action' "$temp_dir/forced.json")" reuse

TEST_CHANGED_FILES='frontend/src/App.tsx'
run_plan explicit-build --force-build frontend --component-version frontend=1.2.3 >/dev/null
assert_eq "$(jq -r '.components.frontend.action' "$temp_dir/explicit-build.json")" build
assert_eq "$(jq -r '.components.frontend.version' "$temp_dir/explicit-build.json")" 1.2.3
assert_eq "$(jq -r '.components.frontend.version_source' "$temp_dir/explicit-build.json")" operator_override

TEST_CHANGED_FILES=''
if run_plan conflicting-reuse-version --component-version frontend=0.0.11 >/dev/null 2>&1; then
  echo 'expected reused component version conflict failure' >&2
  exit 1
fi

if TEST_PREDECESSOR_AVAILABLE=false TEST_MANIFEST="$(write_v2_manifest)" TEST_CHANGED_FILES='' run_plan missing-commit >/dev/null 2>&1; then
  echo 'expected missing predecessor commit failure' >&2
  exit 1
fi
if TEST_PREDECESSOR_ANCESTOR=false TEST_MANIFEST="$(write_v2_manifest)" TEST_CHANGED_FILES='' run_plan non-ancestor >/dev/null 2>&1; then
  echo 'expected non-ancestor predecessor failure' >&2
  exit 1
fi

TEST_MANIFEST="$(write_v2_manifest)"
TEST_CHANGED_FILES='frontend/src/App.tsx'
run_plan deterministic-first --build-date 2026-11-11T12:00:00Z >/dev/null
run_plan deterministic-second --build-date 2026-11-11T12:00:00Z >/dev/null
cmp "$temp_dir/deterministic-first.json" "$temp_dir/deterministic-second.json"
assert_eq "$(jq -r '.plan_sha256' "$temp_dir/deterministic-first.json")" "sha256:$(jq 'del(.plan_sha256)' "$temp_dir/deterministic-first.json" | jq -S -c . | shasum -a 256 | awk '{print $1}')"

TEST_MANIFEST="$(write_v1_manifest)"
TEST_CHANGED_FILES=''
run_plan v1 >/dev/null
assert_eq "$(jq -r '.components.server.action' "$temp_dir/v1.json")" manual_review
assert_eq "$(jq -r '.components.server.version' "$temp_dir/v1.json")" null
assert_eq "$(jq -r '.components.server.predecessor_image' "$temp_dir/v1.json")" "registry.example/coyote-server@$(digest a)"
assert_eq "$(jq -r '.components.server.provenance' "$temp_dir/v1.json")" missing_predecessor_component_version

run_plan v1-explicit-reuse --force-reuse server --component-version server=0.0.10 --component-version worker=0.0.10 >/dev/null
assert_eq "$(jq -r '.components.server.action' "$temp_dir/v1-explicit-reuse.json")" reuse
assert_eq "$(jq -r '.components.worker.action' "$temp_dir/v1-explicit-reuse.json")" reuse
assert_eq "$(jq -r '.components.server.version' "$temp_dir/v1-explicit-reuse.json")" 0.0.10
assert_eq "$(jq -r '.components.server.version_source' "$temp_dir/v1-explicit-reuse.json")" operator_supplied_predecessor

TEST_MANIFEST="$(write_v2_manifest | jq '.components.server.version = 2')"
TEST_CHANGED_FILES=''
if run_plan malformed-component >/dev/null 2>&1; then
  echo 'expected malformed component record failure' >&2
  exit 1
fi

jq '.release_source = "gs://release-bucket/releases "' "$profile" >"$temp_dir/invalid-profile.json"
TEST_MANIFEST="$(write_v2_manifest)"
TEST_CHANGED_FILES=''
if TEST_HEAD_COMMIT="$commit_b" TEST_PREDECESSOR_VERSION=0.0.10 TEST_MANIFEST="$TEST_MANIFEST" TEST_CHANGED_FILES="$TEST_CHANGED_FILES" TEST_COYOTE_LOG="$temp_dir/invalid-profile.coyote" PATH="$temp_dir:$PATH" COYOTE_RELEASE_CLI="$temp_dir/coyote" bash "$repo_root/scripts/coyote-release" plan --release 0.0.11 --from latest --target production --profile "$temp_dir/invalid-profile.json" --output "$temp_dir/invalid-profile.json.out" >/dev/null 2>&1; then
  echo 'expected whitespace profile failure' >&2
  exit 1
fi

jq --arg source $'gs://release-bucket/releases\n' '.release_source = $source' "$profile" >"$temp_dir/control-profile.json"
if TEST_HEAD_COMMIT="$commit_b" TEST_PREDECESSOR_VERSION=0.0.10 TEST_MANIFEST="$TEST_MANIFEST" TEST_CHANGED_FILES="$TEST_CHANGED_FILES" TEST_COYOTE_LOG="$temp_dir/control-profile.coyote" PATH="$temp_dir:$PATH" COYOTE_RELEASE_CLI="$temp_dir/coyote" bash "$repo_root/scripts/coyote-release" plan --release 0.0.11 --from latest --target production --profile "$temp_dir/control-profile.json" --output "$temp_dir/control-profile.json.out" >/dev/null 2>&1; then
  echo 'expected control-character profile failure' >&2
  exit 1
fi

write_profile 'gs://release-bucket/releases'
TEST_MANIFEST="$(write_v2_manifest)"
TEST_CHANGED_FILES=''
run_plan gcs >/dev/null
assert_eq "$(jq -r '.release_source' "$temp_dir/gcs.json")" 'gs://release-bucket/releases'
assert_contains "$(cat "$temp_dir/gcs.coyote")" 'release-source gs://release-bucket/releases'

echo 'Coyote release plan tests passed'