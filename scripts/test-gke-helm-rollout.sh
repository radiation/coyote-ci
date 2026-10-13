#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT
log="$temp_dir/commands.log"
mkdir "$temp_dir/bin"

cat >"$temp_dir/bin/helm" <<'EOF'
#!/usr/bin/env bash
printf 'helm %s\n' "$*" >>"$COMMAND_LOG"
if [[ "$1" == "template" && "$*" == *"templates/gateway.yaml"* ]]; then
  exit 0
fi
if [[ "$1" == "template" && "$*" == *"templates/migration-job.yaml"* ]]; then
  printf '%s\n' 'apiVersion: batch/v1' 'kind: Job' 'metadata: {name: coyote-migrate-test-run}'
fi
EOF
cat >"$temp_dir/bin/kubectl" <<'EOF'
#!/usr/bin/env bash
printf 'kubectl %s\n' "$*" >>"$COMMAND_LOG"
if [[ "$*" == *"get deployment/coyote-mcp"* ]]; then
  if [[ "${KUBECTL_MCP_LOOKUP_ERROR:-false}" == "true" ]]; then
    exit 2
  fi
  if [[ "${KUBECTL_MCP_DEPLOYMENT_EXISTS:-false}" == "true" ]]; then
    printf '%s\n' 'deployment.apps/coyote-mcp'
  fi
  exit 0
fi
if [[ "${KUBECTL_FAIL_WAIT:-false}" == "true" && "$*" == *"wait --for=condition=complete"* ]]; then
  exit 1
fi
EOF
chmod +x "$temp_dir/bin/helm" "$temp_dir/bin/kubectl"

PATH="$temp_dir/bin:$PATH" \
COMMAND_LOG="$log" \
KUBECTL_MCP_DEPLOYMENT_EXISTS=false \
HELM_ROLLOUT_ID=test-run \
GKE_HELM_DRY_RUN=false \
GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
"$repo_root/scripts/gke-helm-rollout.sh" >/dev/null

line_number() {
  grep -n -m1 -- "$1" "$log" | cut -d: -f1
}

server_zero="$(line_number 'kubectl -n coyote-ci patch deployment/coyote-server .*--subresource=scale .*coyote-rollout .*replicas.:0')"
frontend_zero="$(line_number 'kubectl -n coyote-ci patch deployment/coyote-frontend .*--subresource=scale .*coyote-rollout .*replicas.:0')"
worker_zero="$(line_number 'kubectl -n coyote-ci-staging patch deployment/coyote-kubernetes-worker-staging .*--subresource=scale .*coyote-rollout .*replicas.:0')"
server_zero_ready="$(line_number 'kubectl -n coyote-ci rollout status deployment/coyote-server --timeout=600s')"
frontend_zero_ready="$(line_number 'kubectl -n coyote-ci rollout status deployment/coyote-frontend --timeout=600s')"
worker_zero_ready="$(line_number 'kubectl -n coyote-ci-staging rollout status deployment/coyote-kubernetes-worker-staging --timeout=600s')"
migration_apply="$(line_number 'kubectl -n coyote-ci apply -f -')"
migration_wait="$(line_number 'kubectl -n coyote-ci wait --for=condition=complete job/coyote-migrate-test-run')"
control_upgrade="$(grep -n 'helm upgrade coyote-ci ' "$log" | tail -n1 | cut -d: -f1)"
server_restore="$(line_number 'kubectl -n coyote-ci patch deployment/coyote-server .*--subresource=scale .*coyote-rollout .*replicas.:1')"
frontend_restore="$(line_number 'kubectl -n coyote-ci patch deployment/coyote-frontend .*--subresource=scale .*coyote-rollout .*replicas.:1')"
worker_upgrade="$(grep -n 'helm upgrade coyote-ci-worker ' "$log" | tail -n1 | cut -d: -f1)"
worker_restore="$(line_number 'kubectl -n coyote-ci-staging patch deployment/coyote-kubernetes-worker-staging .*--subresource=scale .*coyote-rollout .*replicas.:1')"

[[ "$server_zero" -lt "$frontend_zero" && "$frontend_zero" -lt "$worker_zero" && "$worker_zero" -lt "$server_zero_ready" && "$server_zero_ready" -lt "$frontend_zero_ready" && "$frontend_zero_ready" -lt "$worker_zero_ready" && "$worker_zero_ready" -lt "$migration_apply" && "$migration_apply" -lt "$migration_wait" && "$migration_wait" -lt "$control_upgrade" && "$control_upgrade" -lt "$server_restore" && "$server_restore" -lt "$frontend_restore" && "$frontend_restore" -lt "$worker_upgrade" && "$worker_upgrade" -lt "$worker_restore" ]] || {
  echo "unexpected Helm rollout command sequence" >&2
  cat "$log" >&2
  exit 1
}
! grep -q -- '--force-conflicts' "$log" || {
  echo "normal Helm rollout must not use adoption-only server-side conflict transfer" >&2
  cat "$log" >&2
  exit 1
}
! grep -q -- '--set .*replicas' "$log" || {
  echo "normal Helm rollout must not use replica overrides" >&2
  cat "$log" >&2
  exit 1
}

mcp_values="$temp_dir/mcp-control-values.yaml"
cp "$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" "$mcp_values"
cat >>"$mcp_values" <<'EOF'
mcp:
  enabled: true
  replicas: 2
  replicasManagedExternally: true
  service: {port: 8081}
  terminationGracePeriodSeconds: 30
  resources:
    requests: {cpu: 100m, memory: 128Mi}
    limits: {cpu: 500m, memory: 512Mi}
  auth:
    allowedOrigins: []
    allowDisabledAuth: false
EOF

mcp_log="$temp_dir/mcp-commands.log"
PATH="$temp_dir/bin:$PATH" \
COMMAND_LOG="$mcp_log" \
KUBECTL_MCP_DEPLOYMENT_EXISTS=true \
HELM_ROLLOUT_ID=test-mcp \
GKE_HELM_DRY_RUN=false \
GKE_HELM_CONTROL_PLANE_VALUES="$mcp_values" \
GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
"$repo_root/scripts/gke-helm-rollout.sh" >/dev/null

mcp_zero="$(grep -n -m1 'kubectl -n coyote-ci patch deployment/coyote-mcp .*--subresource=scale .*coyote-rollout .*replicas.:0' "$mcp_log" | cut -d: -f1)"
mcp_zero_ready="$(grep -n -m1 'kubectl -n coyote-ci rollout status deployment/coyote-mcp --timeout=600s' "$mcp_log" | head -n1 | cut -d: -f1)"
mcp_restore="$(grep -n 'kubectl -n coyote-ci patch deployment/coyote-mcp .*--subresource=scale .*coyote-rollout .*replicas.:2' "$mcp_log" | tail -n1 | cut -d: -f1)"
[[ -n "$mcp_zero" && -n "$mcp_zero_ready" && -n "$mcp_restore" && "$mcp_zero" -lt "$mcp_zero_ready" && "$mcp_zero_ready" -lt "$mcp_restore" ]] || {
  echo "enabled MCP was not safely scaled around migration" >&2
  cat "$mcp_log" >&2
  exit 1
}

first_enable_log="$temp_dir/first-enable-commands.log"
PATH="$temp_dir/bin:$PATH" \
COMMAND_LOG="$first_enable_log" \
KUBECTL_MCP_DEPLOYMENT_EXISTS=false \
HELM_ROLLOUT_ID=test-first-enable \
GKE_HELM_DRY_RUN=false \
GKE_HELM_CONTROL_PLANE_VALUES="$mcp_values" \
GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
"$repo_root/scripts/gke-helm-rollout.sh" >/dev/null

! grep -q 'patch deployment/coyote-mcp .*replicas.:0' "$first_enable_log" || {
  echo "first-time MCP enablement attempted to scale a nonexistent deployment" >&2
  cat "$first_enable_log" >&2
  exit 1
}
grep -q 'patch deployment/coyote-mcp .*replicas.:2' "$first_enable_log" || {
  echo "first-time MCP enablement did not restore the desired replica count" >&2
  cat "$first_enable_log" >&2
  exit 1
}

lookup_error_log="$temp_dir/mcp-lookup-error-commands.log"
if lookup_error_output="$(PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$lookup_error_log" \
  KUBECTL_MCP_LOOKUP_ERROR=true \
  HELM_ROLLOUT_ID=test-mcp-lookup-error \
  GKE_HELM_DRY_RUN=false \
  GKE_HELM_CONTROL_PLANE_VALUES="$mcp_values" \
  GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
  "$repo_root/scripts/gke-helm-rollout.sh" 2>&1)"; then
  echo "MCP deployment lookup error must stop the rollout" >&2
  exit 1
fi
[[ "$lookup_error_output" == *"failed to determine whether MCP deployment exists"* ]] || {
  echo "MCP deployment lookup error did not report a safe diagnostic" >&2
  printf '%s\n' "$lookup_error_output" >&2
  exit 1
}
! grep -q ' patch deployment/' "$lookup_error_log" || {
  echo "MCP deployment lookup error started scaling workloads" >&2
  cat "$lookup_error_log" >&2
  exit 1
}
! grep -q ' apply -f -' "$lookup_error_log" || {
  echo "MCP deployment lookup error started the migration" >&2
  cat "$lookup_error_log" >&2
  exit 1
}

disable_log="$temp_dir/disable-commands.log"
PATH="$temp_dir/bin:$PATH" \
COMMAND_LOG="$disable_log" \
KUBECTL_MCP_DEPLOYMENT_EXISTS=true \
HELM_ROLLOUT_ID=test-disable \
GKE_HELM_DRY_RUN=false \
GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
"$repo_root/scripts/gke-helm-rollout.sh" >/dev/null

grep -q 'patch deployment/coyote-mcp .*replicas.:0' "$disable_log" || {
  echo "disabled MCP deployment was not scaled down before migration" >&2
  cat "$disable_log" >&2
  exit 1
}
! grep -q 'patch deployment/coyote-mcp .*replicas.:[1-9]' "$disable_log" || {
  echo "disabled MCP deployment was restored after Helm removed it" >&2
  cat "$disable_log" >&2
  exit 1
}

failure_log="$temp_dir/failure-commands.log"
if PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$failure_log" \
  KUBECTL_MCP_DEPLOYMENT_EXISTS=false \
  HELM_ROLLOUT_ID=test-failure \
  KUBECTL_FAIL_WAIT=true \
  GKE_HELM_DRY_RUN=false \
  GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
  GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
  "$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>&1; then
  echo "migration wait failure must stop the rollout" >&2
  exit 1
fi
if ! grep -q 'kubectl -n coyote-ci-staging patch deployment/coyote-kubernetes-worker-staging .*replicas.:0' "$failure_log" ||
  ! grep -q 'kubectl -n coyote-ci wait --for=condition=complete' "$failure_log"; then
  echo "migration failure did not reach the expected wait phase" >&2
  cat "$failure_log" >&2
  exit 1
fi
[[ "$(grep -c '^helm upgrade ' "$failure_log")" -eq 0 ]] || {
  echo "migration failure started a later rollout phase" >&2
  cat "$failure_log" >&2
  exit 1
}

echo "GKE Helm rollout sequencing test passed"
