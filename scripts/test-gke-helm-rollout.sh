#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT
log="$temp_dir/commands.log"
mkdir "$temp_dir/bin"
export GKE_HELM_ROLLOUT_STATE_DIR="$temp_dir/rollout-state"
export GKE_HELM_TARGET_ENVIRONMENT=staging
export GKE_HELM_EXPECTED_CONTEXT=test-context
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
if [[ "${HELM_FAIL_CONTROL_UPGRADE:-false}" == "true" && "$1" == "upgrade" && "$2" == "coyote-ci" ]]; then
  exit 1
fi
if [[ "${HELM_FAIL_WORKER_UPGRADE:-false}" == "true" && "$1" == "upgrade" && "$2" == "coyote-ci-worker" ]]; then
  exit 1
fi
if [[ -n "${HELM_SLEEP_SECONDS:-}" && "$1" == "upgrade" ]]; then
  sleep "$HELM_SLEEP_SECONDS"
fi
if [[ "$1" == "template" && "$*" == *"templates/gateway.yaml"* ]]; then
  if [[ "${HELM_GATEWAY_ENABLED:-false}" == "true" ]]; then
    printf '%s\n' 'apiVersion: gateway.networking.k8s.io/v1' 'kind: Gateway'
  fi
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
if [[ "$1" == "status" && "$*" == *"--output json"* ]]; then
  case "$2" in
    coyote-ci) printf '{"version":%s}\n' "${HELM_CONTROL_REVISION:-1}" ;;
    coyote-ci-worker) printf '{"version":%s}\n' "${HELM_WORKER_REVISION:-1}" ;;
  esac
fi
EOF
cat >"$temp_dir/bin/kubectl" <<'EOF'
#!/usr/bin/env bash
printf 'kubectl %s\n' "$*" >>"$COMMAND_LOG"
if [[ "$*" == "config current-context" ]]; then
  printf '%s\n' "${KUBECTL_CONTEXT:-test-context}"
  exit 0
fi
if [[ "$*" == *"get namespace kube-system"* ]]; then
  printf '%s\n' "${KUBECTL_CLUSTER_UID:-test-cluster-uid}"
  exit 0
fi
if [[ "$*" == *"get deployment/"*"jsonpath={.spec.replicas}"* ]]; then
  case "$*" in
    *"deployment/coyote-mcp"*) printf '%s\n' "${KUBECTL_MCP_REPLICAS:-2}" ;;
    *) printf '%s\n' "${KUBECTL_DEPLOYMENT_REPLICAS:-1}" ;;
  esac
  exit 0
fi
if [[ "$*" == *"get deployment/coyote-server -o json"* ]]; then
  printf '{"spec":{"template":{"spec":{"containers":[{"name":"server","image":"%s"}]}}}}\n' "${KUBECTL_SERVER_IMAGE:-example.invalid/coyote-server@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}"
  exit 0
fi
if [[ "$*" == *"get deployment/coyote-frontend -o json"* ]]; then
  printf '{"spec":{"template":{"spec":{"containers":[{"name":"frontend","image":"%s"}]}}}}\n' "${KUBECTL_FRONTEND_IMAGE:-example.invalid/coyote-frontend@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb}"
  exit 0
fi
if [[ "$*" == *"get deployment/coyote-kubernetes-worker"*"-o json"* ]]; then
  printf '{"spec":{"template":{"spec":{"containers":[{"name":"worker","image":"%s"}]}}}}\n' "${KUBECTL_WORKER_IMAGE:-example.invalid/coyote-worker@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc}"
  exit 0
fi
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
if [[ "$*" == *"get lease/"* ]]; then
  if [[ -f "${COMMAND_LOG}.lease" ]]; then
    cat "${COMMAND_LOG}.lease"
  elif [[ -n "${KUBECTL_LEASE_JSON:-}" ]]; then
    printf '%s\n' "$KUBECTL_LEASE_JSON"
  fi
  exit 0
fi
if [[ "$*" == *" create -f -" || "$*" == *" replace -f -" ]]; then
  payload="$(cat)"
  if [[ "$payload" == *'"kind":"Lease"'* ]]; then
    if [[ "${KUBECTL_TRANSFER_BEFORE_LEASE_RELEASE:-false}" == "true" ]] && printf '%s' "$payload" | ruby -rjson -e 'exit(JSON.parse(STDIN.read).dig("spec", "holderIdentity").to_s.empty? ? 0 : 1)'; then
      printf '%s\n' '{"metadata":{"uid":"successor-uid","resourceVersion":"2"},"spec":{"holderIdentity":"successor-execution","renewTime":"2099-01-01T00:00:00Z","leaseDurationSeconds":900}}' >"${COMMAND_LOG}.lease"
      exit 1
    fi
    if [[ "${KUBECTL_FAIL_LEASE_RENEW:-false}" == "true" && "$*" == *" replace -f -" ]]; then
      exit 1
    fi
    payload="$(printf '%s' "$payload" | ruby -rjson -e '
      document = JSON.parse(STDIN.read)
      document.fetch("metadata")["uid"] ||= "test-lease-uid"
      document.fetch("metadata")["resourceVersion"] ||= "1"
      puts JSON.generate(document)
    ')"
    printf '%s\n' "$payload" >"${COMMAND_LOG}.lease"
  fi
  exit 0
fi
if [[ "$*" == *"delete lease/"* ]]; then
  rm -f "${COMMAND_LOG}.lease"
  exit 0
fi
if [[ -n "${KUBECTL_FAIL_SCALE_AFTER:-}" && "$*" == *"patch deployment/"* ]]; then
  patches_file="${COMMAND_LOG}.patches"
  patches=0
  [[ -f "$patches_file" ]] && patches="$(cat "$patches_file")"
  patches=$((patches + 1))
  printf '%s\n' "$patches" >"$patches_file"
  if [[ "$patches" -ge "$KUBECTL_FAIL_SCALE_AFTER" ]]; then
    exit 1
  fi
fi
if [[ "$*" == *"get job/coyote-migrate-"* ]]; then
  case "${KUBECTL_MIGRATION_JOB_STATUS:-absent}" in
    absent) ;;
    complete) printf '{"spec":{"template":{"spec":{"containers":[{"name":"migrate","image":"%s"}]}}},"status":{"conditions":[{"type":"Complete","status":"True"}]}}\n' "${KUBECTL_MIGRATION_IMAGE:-example.invalid/coyote-migrate@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd}" ;;
    failed) printf '{"spec":{"template":{"spec":{"containers":[{"name":"migrate","image":"%s"}]}}},"status":{"conditions":[{"type":"Failed","status":"True"}]}}\n' "${KUBECTL_MIGRATION_IMAGE:-example.invalid/coyote-migrate@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd}" ;;
    running) printf '{"spec":{"template":{"spec":{"containers":[{"name":"migrate","image":"%s"}]}}},"status":{"conditions":[]}}\n' "${KUBECTL_MIGRATION_IMAGE:-example.invalid/coyote-migrate@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd}" ;;
  esac
  exit 0
fi
if [[ "$*" == *"get jobs -l app.kubernetes.io/name=coyote-migrate"* ]]; then
  if [[ "${KUBECTL_EXISTING_MIGRATION_HISTORY:-false}" == "true" ]]; then
    printf '%s\n' '{"items":[{"metadata":{"name":"coyote-migrate-prior"}}]}'
  else
    printf '%s\n' '{"items":[]}'
  fi
  exit 0
fi
if [[ "${KUBECTL_FAIL_PREREQUISITE_APPLY:-false}" == "true" && "$*" == *" apply -f -"* ]]; then
  exit 1
fi
if [[ "${KUBECTL_INTERRUPT_AFTER_MIGRATION_APPLY:-false}" == "true" && "$*" == *" apply -f -"* ]]; then
  applies_file="${COMMAND_LOG}.applies"
  applies=0
  [[ -f "$applies_file" ]] && applies="$(cat "$applies_file")"
  applies=$((applies + 1))
  printf '%s\n' "$applies" >"$applies_file"
  if [[ "$applies" -eq 2 ]]; then
    exit 130
  fi
fi
if [[ "${KUBECTL_FAIL_WAIT:-false}" == "true" && "$*" == *"wait --for=condition=complete"* ]]; then
  exit 1
fi
if [[ -n "${KUBECTL_SLEEP_MIGRATION_WAIT:-}" && "$*" == *"wait --for=condition=complete"* ]]; then
  sleep "$KUBECTL_SLEEP_MIGRATION_WAIT"
fi
if [[ "$*" == *"get gateway "* || "$*" == *"get httproute "* ]]; then
  [[ -n "${KUBECTL_SLEEP_GATEWAY_GET:-}" ]] && sleep "$KUBECTL_SLEEP_GATEWAY_GET"
  if [[ "$*" == *"get gateway "* ]]; then
    printf '%s\n' '{"status":{"conditions":[{"type":"Accepted","status":"True"},{"type":"Programmed","status":"True"}]}}'
  else
    printf '%s\n' '{"status":{"parents":[{"conditions":[{"type":"Accepted","status":"True"},{"type":"ResolvedRefs","status":"True"}]}]}}'
  fi
  exit 0
fi
if [[ "${KUBECTL_FAIL_ROLLOUT:-false}" == "true" && "$*" == *"rollout status"* ]]; then
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
  GKE_HELM_RELEASE_VALUES="$release_values" \
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

wrong_context_log="$temp_dir/wrong-context-commands.log"
if PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$wrong_context_log" \
  KUBECTL_CONTEXT=unexpected-context \
  KUBECTL_MCP_DEPLOYMENT_STATE=absent \
  HELM_ROLLOUT_ID=test-wrong-context \
  GKE_HELM_DRY_RUN=false \
  GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
  GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
  GKE_HELM_RELEASE_VALUES="$release_values" \
  "$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>&1; then
  echo "wrong Kubernetes context must stop the rollout" >&2
  exit 1
fi
! grep -q ' patch deployment/\| apply -f -\|helm upgrade' "$wrong_context_log" || {
  echo "wrong Kubernetes context mutated the cluster" >&2
  cat "$wrong_context_log" >&2
  exit 1
}

locked_log="$temp_dir/locked-commands.log"
if PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$locked_log" \
  KUBECTL_LEASE_JSON='{"metadata":{"resourceVersion":"1"},"spec":{"holderIdentity":"another-rollout","renewTime":"2099-01-01T00:00:00Z","leaseDurationSeconds":900}}' \
  KUBECTL_MCP_DEPLOYMENT_STATE=absent \
  HELM_ROLLOUT_ID=test-locked \
  GKE_HELM_DRY_RUN=false \
  GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
  GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
  "$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>&1; then
  echo "conflicting rollout Lease must stop the rollout" >&2
  exit 1
fi
! grep -q ' patch deployment/\| apply -f -\|helm upgrade' "$locked_log" || {
  echo "conflicting rollout Lease mutated workloads" >&2
  cat "$locked_log" >&2
  exit 1
}

partial_scale_log="$temp_dir/partial-scale-commands.log"
if PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$partial_scale_log" \
  KUBECTL_DEPLOYMENT_REPLICAS=3 \
  KUBECTL_FAIL_SCALE_AFTER=2 \
  KUBECTL_MCP_DEPLOYMENT_STATE=absent \
  HELM_ROLLOUT_ID=test-partial-scale \
  GKE_HELM_DRY_RUN=false \
  GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
  GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
  "$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>&1; then
  echo "partial scale-down failure must stop the rollout" >&2
  exit 1
fi
grep -q '"server": 3' "$GKE_HELM_ROLLOUT_STATE_DIR/staging-test-partial-scale.json" || {
  echo "partial scale-down failure did not preserve captured replica intent" >&2
  exit 1
}
! grep -q ' apply -f -\|helm upgrade' "$partial_scale_log" || {
  echo "partial scale-down failure continued into a later phase" >&2
  cat "$partial_scale_log" >&2
  exit 1
}

prerequisite_failure_log="$temp_dir/prerequisite-failure-commands.log"
if PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$prerequisite_failure_log" \
  KUBECTL_FAIL_PREREQUISITE_APPLY=true \
  KUBECTL_MCP_DEPLOYMENT_STATE=absent \
  HELM_ROLLOUT_ID=test-prerequisite-failure \
  GKE_HELM_DRY_RUN=false \
  GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
  GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
  "$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>&1; then
  echo "migration prerequisite failure must stop the rollout" >&2
  exit 1
fi
[[ -f "$GKE_HELM_ROLLOUT_STATE_DIR/staging-test-prerequisite-failure.json" ]] || {
  echo "migration prerequisite failure did not preserve rollout state" >&2
  exit 1
}
! grep -q 'wait --for=condition=complete\|helm upgrade' "$prerequisite_failure_log" || {
  echo "migration prerequisite failure continued into a later phase" >&2
  cat "$prerequisite_failure_log" >&2
  exit 1
}

control_failure_log="$temp_dir/control-failure-commands.log"
if PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$control_failure_log" \
  HELM_FAIL_CONTROL_UPGRADE=true \
  KUBECTL_MCP_DEPLOYMENT_STATE=absent \
  HELM_ROLLOUT_ID=test-control-failure \
  GKE_HELM_DRY_RUN=false \
  GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
  GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
  GKE_HELM_RELEASE_VALUES="$release_values" \
  "$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>&1; then
  echo "control-plane Helm failure must stop the rollout" >&2
  exit 1
fi
grep -q '"migration_status": "complete"' "$GKE_HELM_ROLLOUT_STATE_DIR/staging-test-control-failure.json" || {
  echo "successful migration was not checkpointed before control-plane failure" >&2
  exit 1
}

resume_log="$temp_dir/resume-commands.log"
PATH="$temp_dir/bin:$PATH" \
COMMAND_LOG="$resume_log" \
KUBECTL_MIGRATION_JOB_STATUS=complete \
KUBECTL_MCP_DEPLOYMENT_STATE=absent \
HELM_ROLLOUT_ID=test-control-failure \
GKE_HELM_OPERATION=resume \
GKE_HELM_DRY_RUN=false \
GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
GKE_HELM_RELEASE_VALUES="$release_values" \
"$repo_root/scripts/gke-helm-rollout.sh" >/dev/null
grep -q 'helm upgrade coyote-ci ' "$resume_log" || {
  echo "resume did not continue the control-plane rollout" >&2
  cat "$resume_log" >&2
  exit 1
}
! grep -q 'wait --for=condition=complete' "$resume_log" || {
  echo "resume repeated a completed migration" >&2
  cat "$resume_log" >&2
  exit 1
}

worker_failure_log="$temp_dir/worker-failure-commands.log"
if PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$worker_failure_log" \
  HELM_FAIL_WORKER_UPGRADE=true \
  KUBECTL_MCP_DEPLOYMENT_STATE=absent \
  HELM_ROLLOUT_ID=test-worker-failure \
  GKE_HELM_DRY_RUN=false \
  GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
  GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
  "$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>&1; then
  echo "worker Helm failure must stop the rollout" >&2
  exit 1
fi
grep -q 'helm upgrade coyote-ci ' "$worker_failure_log" || {
  echo "worker failure did not reach a successful control-plane upgrade" >&2
  cat "$worker_failure_log" >&2
  exit 1
}

interrupted_log="$temp_dir/interrupted-commands.log"
if PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$interrupted_log" \
  KUBECTL_INTERRUPT_AFTER_MIGRATION_APPLY=true \
  KUBECTL_MCP_DEPLOYMENT_STATE=absent \
  HELM_ROLLOUT_ID=test-interrupted \
  GKE_HELM_DRY_RUN=false \
  GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
  GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
  "$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>&1; then
  echo "interruption after migration apply must stop the rollout" >&2
  exit 1
fi
grep -q '"migration_status": "applying"' "$GKE_HELM_ROLLOUT_STATE_DIR/staging-test-interrupted.json" || {
  echo "interrupted migration did not preserve its attempted state" >&2
  exit 1
}
if PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$temp_dir/ambiguous-resume-commands.log" \
  KUBECTL_MCP_DEPLOYMENT_STATE=absent \
  HELM_ROLLOUT_ID=test-interrupted \
  GKE_HELM_OPERATION=resume \
  GKE_HELM_DRY_RUN=false \
  GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
  GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
  "$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>&1; then
  echo "resume must refuse an ambiguous migration Job" >&2
  exit 1
fi

changed_release_values="$temp_dir/changed-release-images.yaml"
cp "$release_values" "$changed_release_values"
ruby -ryaml -e '
  values = YAML.load_file(ARGV.fetch(0))
  values.fetch("images")["server"] = "example.invalid/coyote-server@sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
  File.write(ARGV.fetch(1), YAML.dump(values))
' "$release_values" "$changed_release_values"

incomplete_digest_values="$temp_dir/incomplete-release-images.yaml"
cp "$release_values" "$incomplete_digest_values"
ruby -ryaml -e '
  values = YAML.load_file(ARGV.fetch(0))
  values.fetch("images")["server"] = "example.invalid/coyote-server@sha256:deadbeef"
  File.write(ARGV.fetch(1), YAML.dump(values))
' "$release_values" "$incomplete_digest_values"
incomplete_digest_log="$temp_dir/incomplete-digest-commands.log"
if PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$incomplete_digest_log" \
  KUBECTL_MCP_DEPLOYMENT_STATE=absent \
  HELM_ROLLOUT_ID=test-incomplete-digest \
  GKE_HELM_DRY_RUN=false \
  GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
  GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
  GKE_HELM_RELEASE_VALUES="$incomplete_digest_values" \
  "$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>&1; then
  echo "rollout must reject an incomplete image digest" >&2
  exit 1
fi
if [[ -f "$incomplete_digest_log" ]] && grep -q ' patch deployment/\| apply -f -\|helm upgrade' "$incomplete_digest_log"; then
  echo "incomplete image digest reached a cluster mutation" >&2
  cat "$incomplete_digest_log" >&2
  exit 1
fi

if PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$temp_dir/changed-release-resume-commands.log" \
  KUBECTL_MCP_DEPLOYMENT_STATE=absent \
  HELM_ROLLOUT_ID=test-control-failure \
  GKE_HELM_OPERATION=resume \
  GKE_HELM_DRY_RUN=false \
  GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
  GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
  GKE_HELM_RELEASE_VALUES="$changed_release_values" \
  "$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>&1; then
  echo "resume must reject changed release inputs" >&2
  exit 1
fi

replica_recovery_log="$temp_dir/replica-recovery-commands.log"
if PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$replica_recovery_log" \
  KUBECTL_DEPLOYMENT_REPLICAS=3 \
  KUBECTL_FAIL_WAIT=true \
  KUBECTL_MCP_DEPLOYMENT_STATE=absent \
  HELM_ROLLOUT_ID=test-replica-recovery \
  GKE_HELM_DRY_RUN=false \
  GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
  GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
  "$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>&1; then
  echo "migration failure must stop before explicit replica recovery" >&2
  exit 1
fi
grep -q '"server": 3' "$GKE_HELM_ROLLOUT_STATE_DIR/staging-test-replica-recovery.json" || {
  echo "rollout state did not preserve the live nonzero server replica intent" >&2
  exit 1
}
PATH="$temp_dir/bin:$PATH" \
COMMAND_LOG="$replica_recovery_log" \
KUBECTL_MCP_DEPLOYMENT_STATE=absent \
HELM_ROLLOUT_ID=test-replica-recovery \
GKE_HELM_OPERATION=restore-replicas \
GKE_HELM_RESTORE_REPLICAS_CONFIRMED=true \
GKE_HELM_DRY_RUN=false \
GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
"$repo_root/scripts/gke-helm-rollout.sh" >/dev/null
grep -q 'patch deployment/coyote-server .*replicas.:3' "$replica_recovery_log" || {
  echo "explicit replica recovery did not restore the captured server count" >&2
  cat "$replica_recovery_log" >&2
  exit 1
}

same_rollout_lock_log="$temp_dir/same-rollout-lock-commands.log"
if PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$same_rollout_lock_log" \
  KUBECTL_LEASE_JSON='{"metadata":{"resourceVersion":"1"},"spec":{"holderIdentity":"test-same-rollout-111-222","renewTime":"2099-01-01T00:00:00Z","leaseDurationSeconds":900}}' \
  KUBECTL_MCP_DEPLOYMENT_STATE=absent \
  HELM_ROLLOUT_ID=test-same-rollout \
  GKE_HELM_DRY_RUN=false \
  GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
  GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
  "$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>&1; then
  echo "a second process using the same rollout ID must not acquire an active Lease" >&2
  exit 1
fi
! grep -q ' patch deployment/\| apply -f -\|helm upgrade' "$same_rollout_lock_log" || {
  echo "same-rollout Lease conflict mutated workloads" >&2
  cat "$same_rollout_lock_log" >&2
  exit 1
}

stale_lease_log="$temp_dir/stale-lease-commands.log"
PATH="$temp_dir/bin:$PATH" \
COMMAND_LOG="$stale_lease_log" \
KUBECTL_LEASE_JSON='{"metadata":{"resourceVersion":"1"},"spec":{"holderIdentity":"abandoned-rollout","renewTime":"2000-01-01T00:00:00Z","leaseDurationSeconds":1}}' \
KUBECTL_MCP_DEPLOYMENT_STATE=absent \
HELM_ROLLOUT_ID=test-stale-takeover \
GKE_HELM_DRY_RUN=false \
GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
"$repo_root/scripts/gke-helm-rollout.sh" >/dev/null
grep -q 'kubectl -n coyote-ci replace -f -' "$stale_lease_log" || {
  echo "stale Lease was not taken over with resource-version replacement" >&2
  cat "$stale_lease_log" >&2
  exit 1
}

lease_loss_log="$temp_dir/lease-loss-commands.log"
if PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$lease_loss_log" \
  KUBECTL_FAIL_LEASE_RENEW=true \
  HELM_SLEEP_SECONDS=2 \
  KUBECTL_MCP_DEPLOYMENT_STATE=absent \
  HELM_ROLLOUT_ID=test-lease-loss \
  GKE_HELM_LEASE_DURATION_SECONDS=3 \
  GKE_HELM_DRY_RUN=false \
  GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
  GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
  "$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>&1; then
  echo "Lease loss during a blocking Helm operation must stop the rollout" >&2
  exit 1
fi
! grep -q 'helm upgrade coyote-ci-worker ' "$lease_loss_log" || {
  echo "rollout continued after Lease loss during a blocking command" >&2
  cat "$lease_loss_log" >&2
  exit 1
}

migration_renewal_log="$temp_dir/migration-renewal-commands.log"
PATH="$temp_dir/bin:$PATH" \
COMMAND_LOG="$migration_renewal_log" \
KUBECTL_SLEEP_MIGRATION_WAIT=2 \
KUBECTL_MCP_DEPLOYMENT_STATE=absent \
HELM_ROLLOUT_ID=test-migration-renewal \
GKE_HELM_LEASE_DURATION_SECONDS=3 \
GKE_HELM_DRY_RUN=false \
GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
"$repo_root/scripts/gke-helm-rollout.sh" >/dev/null
grep -q 'kubectl -n coyote-ci replace -f -' "$migration_renewal_log" || {
  echo "Lease was not renewed while waiting for the migration Job" >&2
  cat "$migration_renewal_log" >&2
  exit 1
}

migration_loss_log="$temp_dir/migration-lease-loss-commands.log"
if PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$migration_loss_log" \
  KUBECTL_FAIL_LEASE_RENEW=true \
  KUBECTL_SLEEP_MIGRATION_WAIT=2 \
  KUBECTL_MCP_DEPLOYMENT_STATE=absent \
  HELM_ROLLOUT_ID=test-migration-lease-loss \
  GKE_HELM_LEASE_DURATION_SECONDS=3 \
  GKE_HELM_DRY_RUN=false \
  GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
  GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
  "$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>&1; then
  echo "Lease loss during migration waiting must stop the rollout" >&2
  exit 1
fi
! grep -q 'helm upgrade coyote-ci ' "$migration_loss_log" || {
  echo "rollout started a Helm upgrade after Lease loss during migration waiting" >&2
  cat "$migration_loss_log" >&2
  exit 1
}

gateway_renewal_log="$temp_dir/gateway-renewal-commands.log"
PATH="$temp_dir/bin:$PATH" \
COMMAND_LOG="$gateway_renewal_log" \
HELM_GATEWAY_ENABLED=true \
KUBECTL_SLEEP_GATEWAY_GET=2 \
KUBECTL_MCP_DEPLOYMENT_STATE=absent \
HELM_ROLLOUT_ID=test-gateway-renewal \
GKE_HELM_LEASE_DURATION_SECONDS=3 \
GKE_HELM_DRY_RUN=false \
GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
"$repo_root/scripts/gke-helm-rollout.sh" >/dev/null
grep -q 'kubectl -n coyote-ci replace -f -' "$gateway_renewal_log" || {
  echo "Lease was not renewed while checking Gateway readiness" >&2
  cat "$gateway_renewal_log" >&2
  exit 1
}

successor_lease_log="$temp_dir/successor-lease-commands.log"
PATH="$temp_dir/bin:$PATH" \
COMMAND_LOG="$successor_lease_log" \
KUBECTL_TRANSFER_BEFORE_LEASE_RELEASE=true \
KUBECTL_MCP_DEPLOYMENT_STATE=absent \
HELM_ROLLOUT_ID=test-successor-lease \
GKE_HELM_DRY_RUN=false \
GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
"$repo_root/scripts/gke-helm-rollout.sh" >/dev/null
grep -q 'successor-execution' "${successor_lease_log}.lease" || {
  echo "release deleted a successor execution Lease" >&2
  exit 1
}
grep -q 'kubectl -n coyote-ci replace -f -' "$successor_lease_log" || {
  echo "Lease release did not use resource-version conflict protection" >&2
  cat "$successor_lease_log" >&2
  exit 1
}

restore_readiness_log="$temp_dir/restore-readiness-commands.log"
if PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$restore_readiness_log" \
  KUBECTL_FAIL_ROLLOUT=true \
  KUBECTL_MCP_DEPLOYMENT_STATE=absent \
  HELM_ROLLOUT_ID=test-replica-recovery \
  GKE_HELM_OPERATION=restore-replicas \
  GKE_HELM_RESTORE_REPLICAS_CONFIRMED=true \
  GKE_HELM_DRY_RUN=false \
  GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
  GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
  "$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>&1; then
  echo "failed replica restoration readiness must stop the operation" >&2
  exit 1
fi
grep -q '"restore-replicas": "scaled"' "$GKE_HELM_ROLLOUT_STATE_DIR/staging-test-replica-recovery.json" || {
  echo "failed replica restoration did not retain the scaled checkpoint state" >&2
  exit 1
}

if PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$temp_dir/unexpected-migration-image.log" \
  KUBECTL_MIGRATION_JOB_STATUS=complete \
  KUBECTL_MIGRATION_IMAGE='example.invalid/coyote-migrate@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff' \
  KUBECTL_MCP_DEPLOYMENT_STATE=absent \
  HELM_ROLLOUT_ID=test-control-failure \
  GKE_HELM_OPERATION=resume \
  GKE_HELM_DRY_RUN=false \
  GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
  GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
  GKE_HELM_RELEASE_VALUES="$release_values" \
  "$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>&1; then
  echo "resume must reject a completed migration Job with an unexpected image" >&2
  exit 1
fi

if PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$temp_dir/changed-helm-revision.log" \
  KUBECTL_MIGRATION_JOB_STATUS=complete \
  HELM_CONTROL_REVISION=999 \
  KUBECTL_MCP_DEPLOYMENT_STATE=absent \
  HELM_ROLLOUT_ID=test-control-failure \
  GKE_HELM_OPERATION=resume \
  GKE_HELM_DRY_RUN=false \
  GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
  GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
  GKE_HELM_RELEASE_VALUES="$release_values" \
  "$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>&1; then
  echo "resume must reject a changed Helm revision" >&2
  exit 1
fi

if PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$temp_dir/changed-deployment-image.log" \
  KUBECTL_MIGRATION_JOB_STATUS=complete \
  KUBECTL_SERVER_IMAGE='example.invalid/coyote-server@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff' \
  KUBECTL_MCP_DEPLOYMENT_STATE=absent \
  HELM_ROLLOUT_ID=test-control-failure \
  GKE_HELM_OPERATION=resume \
  GKE_HELM_DRY_RUN=false \
  GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
  GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
  GKE_HELM_RELEASE_VALUES="$release_values" \
  "$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>&1; then
  echo "resume must reject a changed deployment image" >&2
  exit 1
fi

missing_state_log="$temp_dir/missing-production-state.log"
if PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$missing_state_log" \
  KUBECTL_EXISTING_MIGRATION_HISTORY=true \
  KUBECTL_MCP_DEPLOYMENT_STATE=absent \
  HELM_ROLLOUT_ID=test-missing-production-state \
  GKE_HELM_TARGET_ENVIRONMENT=production \
  GKE_HELM_EXPECTED_CONTEXT=test-context \
  GKE_HELM_EXPECTED_CLUSTER_UID=test-cluster-uid \
  GKE_HELM_WORKER_NAMESPACE=coyote-ci \
  GKE_HELM_DRY_RUN=false \
  GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-production-control-plane-values.yaml" \
  GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-production-worker-values.yaml" \
  "$repo_root/scripts/gke-helm-rollout.sh" >/dev/null 2>&1; then
  echo "production rollout with missing state and migration history must fail closed" >&2
  exit 1
fi
! grep -q ' patch deployment/\| apply -f -\|helm upgrade' "$missing_state_log" || {
  echo "missing production state with migration history mutated workloads" >&2
  cat "$missing_state_log" >&2
  exit 1
}

echo "GKE Helm rollout sequencing test passed"
