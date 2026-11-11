#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT
mkdir -p "$temp_dir/bin"
log="$temp_dir/commands.log"
state_dir="$temp_dir/rollout-state"
mkdir "$state_dir"

digest() { printf 'sha256:%064d' 0 | tr '0' "$1"; }
assert_contains() { [[ "$1" == *"$2"* ]] || { printf 'expected output to contain: %s\nactual output:\n%s\n' "$2" "$1" >&2; exit 1; }; }
assert_not_contains() { [[ "$1" != *"$2"* ]] || { echo "unexpected output: $2" >&2; exit 1; }; }
assert_read_only() { ! grep -Eq '(^| )(upgrade|install|apply|patch|delete|scale|rollout|create|replace|wait)( |$)' "$log" || { cat "$log" >&2; echo 'deployment assessment issued a mutation command' >&2; exit 1; }; }

server_digest="$(digest a)"
frontend_digest="$(digest b)"
mcp_digest="$(digest c)"
migrate_digest="$(digest d)"
worker_digest="$(digest e)"

cat >"$temp_dir/bin/coyote" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'coyote %s\n' "$*" >>"$COMMAND_LOG"
output=""
for ((index = 1; index <= $#; index++)); do
  if [[ "${!index}" == --output ]]; then next=$((index + 1)); output="${!next}"; fi
done
[[ "$2" == fetch ]] || exit 1
printf '%s\n' "$TEST_MANIFEST" >"$output"
EOF

cat >"$temp_dir/bin/helm" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'helm %s\n' "$*" >>"$COMMAND_LOG"
case "$1" in
  get)
    if [[ "$2" == manifest ]]; then
      if [[ "${TEST_HELM_MANIFEST_DRIFT:-false}" == true ]]; then printf '%s\n' 'apiVersion: v1' 'kind: Secret'; else printf '%s\n' 'apiVersion: v1' 'kind: ConfigMap'; fi
      exit 0
    fi
    [[ "$2" == values ]] || exit 1
    release="$3"
    if [[ "$release" == coyote-ci ]]; then
      jq -n \
        --arg server "$TEST_SERVER_IMAGE" --arg frontend "$TEST_FRONTEND_IMAGE" --arg mcp "$TEST_MCP_IMAGE" --arg migrate "$TEST_MIGRATE_IMAGE" \
        --arg version "$TEST_HELM_RELEASE_VERSION" --arg manifest "$TEST_HELM_MANIFEST_DIGEST" \
        '{images: {server: $server, frontend: $frontend, mcp: $mcp, migrate: $migrate}, release: {version: $version, manifestDigest: $manifest}, mcp: {enabled: true}}'
    else
      jq -n --arg worker "$TEST_WORKER_IMAGE" --arg version "$TEST_HELM_RELEASE_VERSION" --arg manifest "$TEST_HELM_MANIFEST_DIGEST" \
        '{images: {worker: $worker}, release: {version: $version, manifestDigest: $manifest}, worker: {name: "coyote-kubernetes-worker"}}'
    fi
    ;;
  status)
    if [[ "$2" == coyote-ci-worker ]]; then printf '%s\n' '{"chart":"coyote-ci-worker-0.1.0","version":1}'; else printf '%s\n' '{"chart":"coyote-ci-0.1.0","version":1}'; fi
    ;;
  template) printf '%s\n' 'apiVersion: v1' 'kind: ConfigMap' ;;
  *) exit 1 ;;
esac
EOF

cat >"$temp_dir/bin/kubectl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'kubectl %s\n' "$*" >>"$COMMAND_LOG"
if [[ "$*" == 'config current-context' ]]; then printf '%s\n' "${KUBECTL_CONTEXT:-production-context}"; exit 0; fi
if [[ "$*" == *'get namespace kube-system'* ]]; then printf '%s\n' "${KUBECTL_CLUSTER_UID:-production-uid}"; exit 0; fi
if [[ "$*" == *'get deployment/coyote-server'* ]]; then printf '{"spec":{"template":{"spec":{"containers":[{"name":"server","image":"%s"}]}}}}\n' "$KUBECTL_SERVER_IMAGE"; exit 0; fi
if [[ "$*" == *'get deployment/coyote-frontend'* ]]; then printf '{"spec":{"template":{"spec":{"containers":[{"name":"frontend","image":"%s"}]}}}}\n' "$KUBECTL_FRONTEND_IMAGE"; exit 0; fi
if [[ "$*" == *'get deployment/coyote-mcp'* ]]; then
  printf '{"metadata":{"labels":{"app.kubernetes.io/managed-by":"Helm"},"annotations":{"meta.helm.sh/release-name":"coyote-ci","meta.helm.sh/release-namespace":"coyote-ci"}},"spec":{"template":{"spec":{"containers":[{"name":"mcp","image":"%s"}]}}}}\n' "$KUBECTL_MCP_IMAGE"
  exit 0
fi
if [[ "$*" == *'get deployment/coyote-kubernetes-worker'* ]]; then printf '{"spec":{"template":{"spec":{"containers":[{"name":"worker","image":"%s"}]}}}}\n' "$KUBECTL_WORKER_IMAGE"; exit 0; fi
if [[ "$*" == *'get jobs -l app.kubernetes.io/name=coyote-migrate'* ]]; then
  if [[ "${KUBECTL_HISTORICAL_MIGRATION:-false}" == true ]]; then printf '{"items":[{"metadata":{"name":"coyote-migrate-history"},"spec":{"template":{"spec":{"containers":[{"name":"migrate","image":"%s"}]} }},"status":{"conditions":[{"type":"Complete","status":"True"}]}}]}\n' "$KUBECTL_MIGRATE_IMAGE"; else printf '%s\n' '{"items":[]}'; fi
  exit 0
fi
exit 1
EOF
chmod +x "$temp_dir/bin/coyote" "$temp_dir/bin/helm" "$temp_dir/bin/kubectl"

manifest="$(jq -n \
  --arg server "registry.example/coyote-server@$server_digest" \
  --arg frontend "registry.example/coyote-frontend@$frontend_digest" \
  --arg mcp "registry.example/coyote-mcp@$mcp_digest" \
  --arg migrate "registry.example/coyote-migrate@$migrate_digest" \
  --arg worker "registry.example/coyote-worker@$worker_digest" \
  '{schema_version: 2, release: "0.0.10", source: {commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", created_at: "2026-11-11T00:00:00Z"}, components: {server: {version: "0.0.10", image: $server}, frontend: {version: "0.0.10", image: $frontend}, mcp: {version: "0.0.10", image: $mcp}, migrate: {version: "0.0.10", image: $migrate}, worker: {version: "0.0.10", image: $worker}}}')"
manifest_path="$temp_dir/manifest.json"
printf '%s\n' "$manifest" >"$manifest_path"
manifest_digest="sha256:$(shasum -a 256 "$manifest_path" | awk '{print $1}')"

write_profile() {
  local target="$1" worker="$2"
  jq -n --arg target "$target" --arg worker "$worker" '
    {schema_version: 1, target: $target, release_source: "file:///release-store", registry: "registry.example/coyote", kubernetes: {context: ($target + "-context"), cluster_uid: ($target + "-uid"), control_plane_release: "coyote-ci"}}
    | if $worker == "true" then .kubernetes.worker_release = "coyote-ci-worker" else . end
  ' >"$temp_dir/$target-profile.json"
}

run_deploy() {
  local target="$1"
  shift
  : >"$log"
  TEST_MANIFEST="$TEST_MANIFEST" TEST_SERVER_IMAGE="$TEST_SERVER_IMAGE" TEST_FRONTEND_IMAGE="$TEST_FRONTEND_IMAGE" TEST_MCP_IMAGE="$TEST_MCP_IMAGE" TEST_MIGRATE_IMAGE="$TEST_MIGRATE_IMAGE" TEST_WORKER_IMAGE="$TEST_WORKER_IMAGE" TEST_HELM_RELEASE_VERSION="$TEST_HELM_RELEASE_VERSION" TEST_HELM_MANIFEST_DIGEST="$TEST_HELM_MANIFEST_DIGEST" \
    KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-$target-context}" KUBECTL_CLUSTER_UID="${KUBECTL_CLUSTER_UID:-$target-uid}" \
    KUBECTL_SERVER_IMAGE="${KUBECTL_SERVER_IMAGE:-$TEST_SERVER_IMAGE}" KUBECTL_FRONTEND_IMAGE="${KUBECTL_FRONTEND_IMAGE:-$TEST_FRONTEND_IMAGE}" KUBECTL_MCP_IMAGE="${KUBECTL_MCP_IMAGE:-$TEST_MCP_IMAGE}" KUBECTL_WORKER_IMAGE="${KUBECTL_WORKER_IMAGE:-$TEST_WORKER_IMAGE}" \
    KUBECTL_MIGRATE_IMAGE="${KUBECTL_MIGRATE_IMAGE:-$TEST_MIGRATE_IMAGE}" COYOTE_RELEASE_ROLLOUT_STATE_DIR="$state_dir" \
    PATH="$temp_dir/bin:$PATH" COMMAND_LOG="$log" COYOTE_RELEASE_CLI="$temp_dir/bin/coyote" \
    bash "$repo_root/scripts/coyote-release" deploy --release 0.0.10 --target "$target" --profile "$temp_dir/$target-profile.json" "$@"
}

write_checkpoint() {
  local path="$1" context="$2" uid="$3" migration_status="$4"
  jq -n --arg context "$context" --arg uid "$uid" --arg status "$migration_status" --arg migrate "registry.example/coyote-migrate@$migrate_digest" '
    {schema_version: 1, rollout_id: "history", target_environment: "production", context: $context, cluster_uid: $uid, control_namespace: "coyote-ci", worker_namespace: "coyote-ci", control_release: "coyote-ci", worker_release: "coyote-ci-worker", migration_job: "coyote-migrate-history", migration_status: $status, phases: {migration: (if $status == "complete" then "completed" else "attempted" end)}, failure: null, release: {fingerprint: "historical-fingerprint", images: {migrate: $migrate}}}
  ' >"$path"
}

write_profile production false
write_profile staging true
export TEST_MANIFEST="$manifest"
export TEST_SERVER_IMAGE="$server_digest"
export TEST_FRONTEND_IMAGE="registry.example/coyote-frontend@$frontend_digest"
export TEST_MCP_IMAGE="registry.example/coyote-mcp:0.0.10@$mcp_digest"
export TEST_MIGRATE_IMAGE="registry.example/coyote-migrate@$migrate_digest"
export TEST_WORKER_IMAGE="registry.example/coyote-worker@$worker_digest"
export TEST_HELM_RELEASE_VERSION=0.0.9
export TEST_HELM_MANIFEST_DIGEST="sha256:$(printf '%064d' 0 | tr '0' f)"

output="$(run_deploy production)"
assert_contains "$output" 'Component: server runtime-equivalent'
assert_contains "$output" 'Component: frontend runtime-equivalent'
assert_contains "$output" 'Component: mcp runtime-equivalent'
assert_contains "$output" 'Deployment assessment: runtime-equivalent'
assert_not_contains "$(cat "$log")" coyote-kubernetes-worker
assert_read_only

export TEST_HELM_RELEASE_VERSION=0.0.10
export TEST_HELM_MANIFEST_DIGEST="$manifest_digest"
output="$(run_deploy production)"
assert_contains "$output" 'Deployment assessment: release-equivalent'
assert_read_only

output="$(TEST_HELM_MANIFEST_DRIFT=true run_deploy production)"
assert_contains "$output" 'rendered Helm manifest differs from the installed release'
assert_contains "$output" 'Deployment assessment: drift'
assert_read_only

output="$(TEST_FRONTEND_IMAGE="registry.example/coyote-frontend@$(digest f)" KUBECTL_FRONTEND_IMAGE="registry.example/coyote-frontend@$frontend_digest" run_deploy production)"
assert_contains "$output" 'Component: frontend helm-input drift'
assert_contains "$output" 'Deployment assessment: drift'
assert_read_only

export KUBECTL_FRONTEND_IMAGE="registry.example/coyote-frontend@$(digest f)"
output="$(run_deploy production)"
assert_contains "$output" 'Component: frontend drift'
assert_contains "$output" 'Deployment assessment: drift'
assert_read_only
unset KUBECTL_FRONTEND_IMAGE

if run_deploy production --scope worker >/dev/null 2>&1; then
  echo 'expected missing production worker release rejection' >&2
  exit 1
fi
assert_not_contains "$(cat "$log")" coyote-kubernetes-worker

output="$(run_deploy staging)"
assert_contains "$output" 'Deployment assessment: release-equivalent'
assert_not_contains "$(cat "$log")" coyote-kubernetes-worker
assert_read_only

output="$(run_deploy staging --scope worker)"
assert_contains "$output" 'Component: worker runtime-equivalent'
assert_contains "$output" 'Deployment assessment: release-equivalent'
assert_read_only

output="$(KUBECTL_HISTORICAL_MIGRATION=true run_deploy staging --scope worker)"
assert_contains "$output" 'Deployment assessment: release-equivalent'
assert_not_contains "$(cat "$log")" 'get jobs -l app.kubernetes.io/name=coyote-migrate'
assert_read_only

if KUBECTL_CONTEXT=wrong-context run_deploy production >/dev/null 2>&1; then
  echo 'expected context mismatch rejection' >&2
  exit 1
fi
assert_not_contains "$(cat "$log")" 'coyote release fetch'
if KUBECTL_CLUSTER_UID=wrong-uid run_deploy production >/dev/null 2>&1; then
  echo 'expected cluster UID mismatch rejection' >&2
  exit 1
fi

export TEST_MANIFEST='not-json'
if run_deploy production >/dev/null 2>&1; then
  echo 'expected malformed manifest rejection' >&2
  exit 1
fi
export TEST_MANIFEST="$manifest"

export TEST_MANIFEST="$(jq '.components.server.image += " trailing"' "$manifest_path")"
if run_deploy production >/dev/null 2>&1; then
  echo 'expected trailing manifest image rejection' >&2
  exit 1
fi
export TEST_MANIFEST="$manifest"

export TEST_MANIFEST="$(jq '.components.server.image = "registry.example/invalid^name@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"' "$manifest_path")"
if run_deploy production >/dev/null 2>&1; then
  echo 'expected malformed manifest image rejection' >&2
  exit 1
fi
export TEST_MANIFEST="$manifest"

output="$(KUBECTL_SERVER_IMAGE="registry.example/coyote-server@$server_digest trailing" run_deploy production)"
assert_contains "$output" 'Component: server drift'
assert_contains "$output" 'observed=<not-immutable>'
assert_contains "$output" 'Deployment assessment: drift'
assert_read_only

output="$(KUBECTL_HISTORICAL_MIGRATION=true run_deploy production)"
assert_contains "$output" 'Component: migrate reconciliation-required'
assert_contains "$output" 'Deployment assessment: migration-reconciliation-required'
assert_read_only

write_checkpoint "$state_dir/production-valid.json" production-context production-uid complete
output="$(KUBECTL_HISTORICAL_MIGRATION=true run_deploy production)"
assert_contains "$output" 'Component: migrate checkpointed historical-job=coyote-migrate-history'
assert_contains "$output" 'Deployment assessment: release-equivalent'
assert_read_only

rm -f "$state_dir"/*.json
write_checkpoint "$state_dir/production-wrong-cluster.json" wrong-context production-uid complete
output="$(KUBECTL_HISTORICAL_MIGRATION=true run_deploy production)"
assert_contains "$output" 'Component: migrate reconciliation-required'
assert_contains "$output" 'Deployment assessment: migration-reconciliation-required'
assert_read_only

write_checkpoint "$state_dir/production-valid.json" production-context production-uid complete
write_checkpoint "$state_dir/production-incomplete.json" production-context production-uid applying
output="$(KUBECTL_HISTORICAL_MIGRATION=true run_deploy production)"
assert_contains "$output" 'Component: migrate reconciliation-required'
assert_contains "$output" 'Deployment assessment: migration-reconciliation-required'
assert_read_only

echo 'Coyote release deploy tests passed'