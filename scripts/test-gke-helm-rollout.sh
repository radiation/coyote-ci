#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT
log="$temp_dir/commands.log"
mkdir "$temp_dir/bin"
release_values="$temp_dir/release-images.yaml"
cat >"$release_values" <<'EOF'
images:
  server: example.invalid/coyote-server@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  frontend: example.invalid/coyote-frontend@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
  worker: example.invalid/coyote-worker@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
  migrate: example.invalid/coyote-migrate@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
EOF

cat >"$temp_dir/bin/helm" <<'EOF'
#!/usr/bin/env bash
printf 'helm %s\n' "$*" >>"$COMMAND_LOG"
if [[ "$1" == "template" && "$*" == *"templates/gateway.yaml"* ]]; then
  exit 0
fi
if [[ "$1" == "template" && "$*" == *"templates/identities-rbac.yaml"* ]]; then
  printf '%s\n' \
    'apiVersion: v1' \
    'kind: ServiceAccount' \
    'metadata: {name: coyote-migrate, namespace: coyote-ci}' \
    '---' \
    'apiVersion: secrets-store.csi.x-k8s.io/v1' \
    'kind: SecretProviderClass' \
    'metadata: {name: coyote-migrate-secrets, namespace: coyote-ci}'
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
  case "${KUBECTL_MCP_DEPLOYMENT_STATE:-absent}" in
    absent) ;;
    managed) printf '%s\n' '{"metadata":{"labels":{"app.kubernetes.io/managed-by":"Helm"},"annotations":{"meta.helm.sh/release-name":"coyote-ci","meta.helm.sh/release-namespace":"coyote-ci"}}}' ;;
    unowned) printf '%s\n' '{"metadata":{}}' ;;
    other) printf '%s\n' '{"metadata":{"labels":{"app.kubernetes.io/managed-by":"Helm"},"annotations":{"meta.helm.sh/release-name":"other-release","meta.helm.sh/release-namespace":"coyote-ci"}}}' ;;
    *) echo "unknown MCP deployment state: ${KUBECTL_MCP_DEPLOYMENT_STATE}" >&2; exit 2 ;;
  esac
  exit 0
fi
if [[ "${KUBECTL_FAIL_WAIT:-false}" == "true" && "$*" == *"wait --for=condition=complete"* ]]; then
  exit 1
fi
EOF
chmod +x "$temp_dir/bin/helm" "$temp_dir/bin/kubectl"

PATH="$temp_dir/bin:$PATH" \
COMMAND_LOG="$log" \
KUBECTL_MCP_DEPLOYMENT_STATE=absent \
HELM_ROLLOUT_ID=test-run \
GKE_HELM_DRY_RUN=false \
GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
GKE_HELM_RELEASE_VALUES="$release_values" \
"$repo_root/scripts/gke-helm-rollout.sh" >/dev/null

worker_values_without_name="$temp_dir/worker-without-name.yaml"
ruby -ryaml -e '
  values = YAML.load_file(ARGV.fetch(0))
  values.fetch("worker").delete("name")
  File.write(ARGV.fetch(1), YAML.dump(values))
' "$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" "$worker_values_without_name"
omitted_name_log="$temp_dir/omitted-worker-name-commands.log"
PATH="$temp_dir/bin:$PATH" \
COMMAND_LOG="$omitted_name_log" \
KUBECTL_MCP_DEPLOYMENT_STATE=absent \
HELM_ROLLOUT_ID=test-omitted-worker-name \
GKE_HELM_DRY_RUN=false \
GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
GKE_HELM_WORKER_VALUES="$worker_values_without_name" \
GKE_HELM_RELEASE_VALUES="$release_values" \
"$repo_root/scripts/gke-helm-rollout.sh" >/dev/null
grep -q 'patch deployment/coyote-kubernetes-worker-staging .*replicas.:0' "$omitted_name_log" || {
  echo "rollout did not use the Helm default worker name when worker.name was omitted" >&2
  cat "$omitted_name_log" >&2
  exit 1
}

line_number() {
  grep -n -m1 -- "$1" "$log" | cut -d: -f1
}

server_zero="$(line_number 'kubectl -n coyote-ci patch deployment/coyote-server .*--subresource=scale .*coyote-rollout .*replicas.:0')"
frontend_zero="$(line_number 'kubectl -n coyote-ci patch deployment/coyote-frontend .*--subresource=scale .*coyote-rollout .*replicas.:0')"
worker_zero="$(line_number 'kubectl -n coyote-ci-staging patch deployment/coyote-kubernetes-worker-staging .*--subresource=scale .*coyote-rollout .*replicas.:0')"
server_zero_ready="$(line_number 'kubectl -n coyote-ci rollout status deployment/coyote-server --timeout=600s')"
frontend_zero_ready="$(line_number 'kubectl -n coyote-ci rollout status deployment/coyote-frontend --timeout=600s')"
worker_zero_ready="$(line_number 'kubectl -n coyote-ci-staging rollout status deployment/coyote-kubernetes-worker-staging --timeout=600s')"
migrator_prerequisite_apply="$(grep -n 'kubectl -n coyote-ci apply -f -' "$log" | sed -n '1s/:.*//p')"
migration_apply="$(grep -n 'kubectl -n coyote-ci apply -f -' "$log" | sed -n '2s/:.*//p')"
migration_wait="$(line_number 'kubectl -n coyote-ci wait --for=condition=complete job/coyote-migrate-test-run')"
control_upgrade="$(grep -n 'helm upgrade coyote-ci ' "$log" | tail -n1 | cut -d: -f1)"
server_restore="$(line_number 'kubectl -n coyote-ci patch deployment/coyote-server .*--subresource=scale .*coyote-rollout .*replicas.:1')"
frontend_restore="$(line_number 'kubectl -n coyote-ci patch deployment/coyote-frontend .*--subresource=scale .*coyote-rollout .*replicas.:1')"
worker_upgrade="$(grep -n 'helm upgrade coyote-ci-worker ' "$log" | tail -n1 | cut -d: -f1)"
worker_restore="$(line_number 'kubectl -n coyote-ci-staging patch deployment/coyote-kubernetes-worker-staging .*--subresource=scale .*coyote-rollout .*replicas.:1')"

[[ -n "$migrator_prerequisite_apply" && -n "$migration_apply" && "$server_zero" -lt "$frontend_zero" && "$frontend_zero" -lt "$worker_zero" && "$worker_zero" -lt "$server_zero_ready" && "$server_zero_ready" -lt "$frontend_zero_ready" && "$frontend_zero_ready" -lt "$worker_zero_ready" && "$worker_zero_ready" -lt "$migrator_prerequisite_apply" && "$migrator_prerequisite_apply" -lt "$migration_apply" && "$migration_apply" -lt "$migration_wait" && "$migration_wait" -lt "$control_upgrade" && "$control_upgrade" -lt "$server_restore" && "$server_restore" -lt "$frontend_restore" && "$frontend_restore" -lt "$worker_upgrade" && "$worker_upgrade" -lt "$worker_restore" ]] || {
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
grep -q -- "--values $release_values" "$log" || {
  echo "rollout did not apply the immutable release image overlay" >&2
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
KUBECTL_MCP_DEPLOYMENT_STATE=managed \
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
KUBECTL_MCP_DEPLOYMENT_STATE=absent \
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
[[ "$lookup_error_output" == *"failed to determine coyote-mcp ownership"* ]] || {
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
if disable_output="$(PATH="$temp_dir/bin:$PATH" \
COMMAND_LOG="$disable_log" \
KUBECTL_MCP_DEPLOYMENT_STATE=unowned \
HELM_ROLLOUT_ID=test-disable \
GKE_HELM_DRY_RUN=false \
GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
"$repo_root/scripts/gke-helm-rollout.sh" 2>&1)"; then
  echo "unowned disabled MCP must require an explicit database-isolation confirmation" >&2
  exit 1
fi

[[ "$disable_output" == *"GKE_HELM_UNMANAGED_MCP_DATABASE_ISOLATION_CONFIRMED=true"* ]] || {
  echo "unowned MCP guard did not require database-isolation confirmation" >&2
  printf '%s\n' "$disable_output" >&2
  exit 1
}
! grep -q 'patch deployment/coyote-mcp' "$disable_log" || {
  echo "unowned MCP guard scaled the deployment" >&2
  cat "$disable_log" >&2
  exit 1
}
! grep -q ' apply -f -' "$disable_log" || {
  echo "unowned MCP guard started the migration" >&2
  cat "$disable_log" >&2
  exit 1
}

disable_log="$temp_dir/confirmed-disable-commands.log"
PATH="$temp_dir/bin:$PATH" \
COMMAND_LOG="$disable_log" \
KUBECTL_MCP_DEPLOYMENT_STATE=unowned \
GKE_HELM_UNMANAGED_MCP_DATABASE_ISOLATION_CONFIRMED=true \
HELM_ROLLOUT_ID=test-confirmed-disable \
GKE_HELM_DRY_RUN=false \
GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
"$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>"$temp_dir/disable.err"

grep -q 'operator confirmed database isolation, leaving it unchanged' "$temp_dir/disable.err" || {
  echo "confirmed unowned MCP was not reported as untouched" >&2
  cat "$temp_dir/disable.err" >&2
  exit 1
}
! grep -q 'patch deployment/coyote-mcp' "$disable_log" || {
  echo "confirmed unowned MCP deployment was scaled" >&2
  cat "$disable_log" >&2
  exit 1
}

managed_disable_log="$temp_dir/managed-disable-commands.log"
if PATH="$temp_dir/bin:$PATH" \
COMMAND_LOG="$managed_disable_log" \
KUBECTL_MCP_DEPLOYMENT_STATE=managed \
HELM_ROLLOUT_ID=test-managed-disable \
GKE_HELM_DRY_RUN=false \
GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
"$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>"$temp_dir/managed-disable.err"; then
  echo "rollout must stop before deleting a Helm-managed MCP deployment" >&2
  exit 1
fi
grep -q 'refusing a rollout that would delete it' "$temp_dir/managed-disable.err" || {
  echo "managed MCP guard did not report a controlled error" >&2
  cat "$temp_dir/managed-disable.err" >&2
  exit 1
}
! grep -q ' patch deployment/' "$managed_disable_log" || {
  echo "managed MCP guard started scaling workloads" >&2
  cat "$managed_disable_log" >&2
  exit 1
}

production_log="$temp_dir/production-commands.log"
PATH="$temp_dir/bin:$PATH" \
COMMAND_LOG="$production_log" \
KUBECTL_MCP_DEPLOYMENT_STATE=absent \
HELM_ROLLOUT_ID=test-production \
GKE_HELM_DRY_RUN=false \
GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-production-control-plane-values.yaml" \
GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-production-worker-values.yaml" \
GKE_HELM_WORKER_NAMESPACE=coyote-ci \
"$repo_root/scripts/gke-helm-rollout.sh" >/dev/null

grep -q 'kubectl -n coyote-ci patch deployment/coyote-kubernetes-worker .*replicas.:0' "$production_log" || {
  echo "production rollout did not quiesce the production worker" >&2
  cat "$production_log" >&2
  exit 1
}
grep -q 'kubectl -n coyote-ci patch deployment/coyote-kubernetes-worker .*replicas.:1' "$production_log" || {
  echo "production rollout did not restore the production worker" >&2
  cat "$production_log" >&2
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
