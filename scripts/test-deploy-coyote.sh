#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT
mkdir "$temp_dir/bin"

cat >"$temp_dir/bin/kubectl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == 'config current-context' ]]
printf '%s\n' "$COYOTE_KUBE_CONTEXT"
EOF

cat >"$temp_dir/bin/coyote" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$CLI_LOG"
if [[ " $* " == *' release publish-coyote '* ]]; then
  printf 'Published Coyote release 0.0.16 (sha256:%064d)\n' 0
  exit 0
fi
operation=resolve
if [[ " $* " == *' release fetch '* ]]; then
  operation=fetch
  [[ "${TEST_FETCH_FAIL:-false}" == false ]] || exit 1
else
  [[ " $* " == *' release resolve '* ]]
fi
output=''
source=''
version=''
while (($#)); do
  case "$1" in
    --output) output="$2"; shift ;;
    --release-source) source="$2"; shift ;;
    --release|--version) version="$2"; shift ;;
  esac
  shift
done
[[ "$version" == 0.0.16 && -n "$output" ]]
if [[ "$operation" == fetch ]]; then
  [[ "$source" == "$COYOTE_RELEASE_STORE" ]]
  cp "$TEST_MANIFEST" "$output"
else
  [[ "$source" != "$COYOTE_RELEASE_STORE" ]]
  cmp "$source/releases/$version.json" "$TEST_MANIFEST"
  printf '{}\n' >"$output"
fi
EOF

cat >"$temp_dir/bin/helm" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$1" in
  status) exit 0 ;;
  get)
    printf 'migration:\n  onlineUpgradeApproved: true\n  maintenanceAcknowledged: true\n'
    exit 0 ;;
  upgrade) ;;
  *) exit 1 ;;
esac
printf '%s\n' "$*" >>"$HELM_LOG"
[[ "$3" == "$EXPECTED_CHART_REFERENCE" ]]
[[ "${TEST_CHART_UNAVAILABLE:-false}" == false ]] || exit 1
dry_run=false
values_count=0
online_seen=false
maintenance_seen=false
while (($#)); do
  case "$1" in
    --version) echo 'mutable chart version selection is forbidden' >&2; exit 1 ;;
    --values) values_count=$((values_count + 1)); shift ;;
    --set)
      [[ "$values_count" == 2 ]]
      case "$2" in
        "migration.onlineUpgradeApproved=$EXPECTED_ONLINE") online_seen=true ;;
        "migration.maintenanceAcknowledged=$EXPECTED_MAINTENANCE") maintenance_seen=true ;;
        *) exit 1 ;;
      esac
      shift ;;
    --dry-run=server) dry_run=true ;;
  esac
  shift
done
[[ "$online_seen" == true && "$maintenance_seen" == true ]]
if [[ "$dry_run" == true ]]; then printf 'DESCRIPTION: Dry run complete\n'; fi
EOF
chmod +x "$temp_dir/bin/"*

export PATH="$temp_dir/bin:$PATH"
export COYOTE_CLI="$temp_dir/bin/coyote"
export COYOTE_SERVER=https://example.invalid
export COYOTE_RELEASE_STORE=gs://test-releases
export COYOTE_CHART_REPO=oci://example.invalid/charts/coyote-ci
export COYOTE_HELM_RELEASE=test-coyote COYOTE_NAMESPACE=test-coyote COYOTE_KUBE_CONTEXT=test-context
export CLI_LOG="$temp_dir/cli.log" HELM_LOG="$temp_dir/helm.log"
export TEST_MANIFEST="$temp_dir/manifest.json"
chart_digest="sha256:$(printf '%064d' 1)"
export EXPECTED_CHART_REFERENCE="oci://manifest.example.invalid/release-charts/coyote-ci@$chart_digest"
jq -n --arg digest "$chart_digest" '{
  schema_version: 3, release: "0.0.16",
  helm: {name: "coyote-ci", repository: "oci://manifest.example.invalid/release-charts/coyote-ci", version: "0.0.16", digest: $digest}
}' >"$TEST_MANIFEST"

for source in version build; do
  for preview in true false; do
    for approval in none online maintenance; do
      args=("--$source" "0.0.16")
      if [[ "$source" == build ]]; then args=(--build test-build); fi
      if [[ "$preview" == true ]]; then args+=(--dry-run); fi
      export EXPECTED_ONLINE=false EXPECTED_MAINTENANCE=false
      case "$approval" in
        online) args+=(--approve-online-upgrade); EXPECTED_ONLINE=true ;;
        maintenance) args+=(--acknowledge-maintenance); EXPECTED_MAINTENANCE=true ;;
      esac
      : >"$HELM_LOG"
      : >"$CLI_LOG"
      bash "$repo_root/scripts/deploy-coyote.sh" "${args[@]}" >"$temp_dir/output" 2>&1 || {
        cat "$temp_dir/output" >&2
        exit 1
      }
      expected_calls=2
      if [[ "$preview" == true ]]; then expected_calls=1; fi
      [[ "$(wc -l <"$HELM_LOG" | tr -d ' ')" == "$expected_calls" ]]
      [[ "$(grep -c ' release fetch ' "$CLI_LOG")" == 1 ]]
    done
  done
done

: >"$CLI_LOG"
: >"$HELM_LOG"
if bash "$repo_root/scripts/deploy-coyote.sh" --build test-build --approve-online-upgrade --acknowledge-maintenance >"$temp_dir/output" 2>&1; then
  echo 'conflicting migration approvals were accepted' >&2
  exit 1
fi
grep -q 'choose either online upgrade approval or maintenance acknowledgement' "$temp_dir/output"
[[ ! -s "$CLI_LOG" && ! -s "$HELM_LOG" ]]

valid_manifest="$TEST_MANIFEST"
for mutation in 'del(.helm)' '.helm.digest = "sha256:bad"' '.helm.repository = "oci://example.invalid/coyote-ci:0.0.16"' '.release = "0.0.17"'; do
  export TEST_MANIFEST="$temp_dir/invalid.json"
  jq "$mutation" "$valid_manifest" >"$TEST_MANIFEST"
  : >"$HELM_LOG"
  if bash "$repo_root/scripts/deploy-coyote.sh" --version 0.0.16 >"$temp_dir/output" 2>&1; then
    echo "invalid manifest was accepted: $mutation" >&2
    exit 1
  fi
  grep -q 'valid digest-pinned coyote-ci Helm descriptor' "$temp_dir/output"
  [[ ! -s "$HELM_LOG" ]]
done
export TEST_MANIFEST="$valid_manifest" EXPECTED_ONLINE=false EXPECTED_MAINTENANCE=false

: >"$HELM_LOG"
if TEST_FETCH_FAIL=true bash "$repo_root/scripts/deploy-coyote.sh" --version 0.0.16 >"$temp_dir/output" 2>&1; then
  echo 'failed manifest fetch was accepted' >&2
  exit 1
fi
[[ ! -s "$HELM_LOG" ]]

: >"$HELM_LOG"
if TEST_CHART_UNAVAILABLE=true bash "$repo_root/scripts/deploy-coyote.sh" --version 0.0.16 >"$temp_dir/output" 2>&1; then
  echo 'unavailable pinned chart was accepted' >&2
  exit 1
fi
[[ "$(wc -l <"$HELM_LOG" | tr -d ' ')" == 1 ]]
grep -q -- '--dry-run=server' "$HELM_LOG"

echo 'Deploy migration approval and chart identity tests passed'