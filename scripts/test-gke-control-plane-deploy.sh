#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT
mkdir "$temp_dir/bin"
log="$temp_dir/commands.log"
image="example.invalid/coyote@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

cat >"$temp_dir/bin/kubectl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'kubectl %s\n' "$*" >>"$COMMAND_LOG"
if [[ "$*" == *"control-plane-before-migration.yaml"* ]]; then
  manifest="${@: -1}"
  ruby -ryaml -e '
    YAML.load_stream(File.read(ARGV.fetch(0))).compact
      .select { |document| document["kind"] == "Deployment" }
      .each { |document| abort "pre-migration deployment is not scaled to zero" unless document.dig("spec", "replicas") == 0 }
  ' "$manifest"
fi
if [[ "$*" == *"jsonpath={.metadata.labels.app"* ]]; then
  if [[ "$*" == *"deployment coyote-server"* ]]; then
    printf '%s' "${MOCK_SERVER_MANAGED_BY:-${MOCK_MANAGED_BY:-}}"
  elif [[ "$*" == *"deployment coyote-frontend"* ]]; then
    printf '%s' "${MOCK_FRONTEND_MANAGED_BY:-${MOCK_MANAGED_BY:-}}"
  fi
elif [[ "$*" == *"jsonpath={.spec.template.spec.containers[0].image}"* ]]; then
  printf '%s' "$MOCK_IMAGE"
fi
EOF
chmod +x "$temp_dir/bin/kubectl"

run_deployer() {
  env PATH="$temp_dir/bin:$PATH" \
    COMMAND_LOG="$log" \
    MOCK_IMAGE="$image" \
    GCP_PROJECT=test-project \
    COYOTE_SERVER_GSA_EMAIL=server@test-project.iam.gserviceaccount.com \
    COYOTE_MIGRATE_GSA_EMAIL=migrate@test-project.iam.gserviceaccount.com \
    COYOTE_SERVER_IMAGE="$image" \
    COYOTE_FRONTEND_IMAGE="$image" \
    COYOTE_MIGRATE_IMAGE="$image" \
    ARTIFACT_GCS_BUCKET=test-artifacts \
    WORKER_CACHE_GCS_BUCKET=test-cache \
    WORKSPACE_REVISION_GCS_BUCKET=test-revisions \
    CONTROL_PLANE_AUTH_MODE=oidc \
    CONTROL_PLANE_BOOTSTRAP_ADMIN_EMAILS=admin@example.com \
    CONTROL_PLANE_OIDC_ISSUER_URL=https://issuer.example.com \
    CONTROL_PLANE_OIDC_CLIENT_ID=test-client \
    CONTROL_PLANE_OIDC_REDIRECT_URL=https://coyote.example.com/auth/callback \
    CONTROL_PLANE_PUBLIC_URL=https://coyote.example.com \
    "$@"
}

run_deployer bash "$repo_root/scripts/gke-control-plane-deploy.sh" >/dev/null

for deployment in coyote-server coyote-frontend; do
  grep -q "patch deployment/$deployment --subresource=scale --type merge --field-manager=coyote-raw-rollout --patch {\"spec\":{\"replicas\":1}}" "$log" || {
    echo "raw deployment path did not explicitly restore $deployment replicas" >&2
    exit 1
  }
done
[[ "$(grep -c '^kubectl .*control-plane-before-migration.yaml$' "$log")" -eq 2 ]] || {
  echo "raw deployment path did not dry-run and apply the explicit zero-replica manifest" >&2
  cat "$log" >&2
  exit 1
}
! grep -q 'patch deployment/.*replicas.:0' "$log" || {
  echo "raw deployment path must create or update zero replicas with the pre-migration manifest" >&2
  exit 1
}
pre_migration_apply="$(grep -n -m1 'kubectl apply -f .*/control-plane-before-migration\.yaml$' "$log" | cut -d: -f1)"
migration_apply="$(grep -n -m1 'kubectl apply -f .*/migration\.yaml$' "$log" | cut -d: -f1)"
[[ "$pre_migration_apply" -lt "$migration_apply" ]] || {
  echo "raw deployment path applied the migration before the zero-replica manifest" >&2
  cat "$log" >&2
  exit 1
}
! grep -q 'kubectl .* scale deployment/' "$log" || {
  echo "raw deployment path must use the scale subresource patch, not kubectl scale" >&2
  exit 1
}
ruby -ryaml -e '
  deployments = YAML.load_stream(File.read(ARGV.fetch(0))).compact.select { |document| document["kind"] == "Deployment" }
  deployments.each do |deployment|
    abort "#{deployment.dig("metadata", "name")} unexpectedly renders replicas" if deployment.fetch("spec").key?("replicas")
  end
' "$repo_root/deploy/kubernetes/gke/control-plane.yaml"

: >"$log"
if run_deployer MOCK_MANAGED_BY=Helm bash "$repo_root/scripts/gke-control-plane-deploy.sh" >/dev/null 2>"$temp_dir/helm.err"; then
  echo "raw deployment path must refuse Helm-managed control planes" >&2
  exit 1
fi
grep -q 'coyote-server is Helm-managed' "$temp_dir/helm.err" || {
  echo "raw deployment Helm guard did not report a controlled error" >&2
  exit 1
}
! grep -q '^kubectl .* apply ' "$log" || {
  echo "raw deployment path mutated a Helm-managed control plane" >&2
  exit 1
}

: >"$log"
if run_deployer MOCK_FRONTEND_MANAGED_BY=Helm bash "$repo_root/scripts/gke-control-plane-deploy.sh" >/dev/null 2>"$temp_dir/frontend-helm.err"; then
  echo "raw deployment path must refuse a Helm-managed frontend" >&2
  exit 1
fi
grep -q 'coyote-frontend is Helm-managed' "$temp_dir/frontend-helm.err" || {
  echo "raw deployment frontend Helm guard did not report a controlled error" >&2
  exit 1
}
! grep -q '^kubectl .* apply ' "$log" || {
  echo "raw deployment path mutated a Helm-managed frontend" >&2
  exit 1
}

: >"$log"
if run_deployer CONTROL_PLANE_SERVER_REPLICAS=01 bash "$repo_root/scripts/gke-control-plane-deploy.sh" >/dev/null 2>"$temp_dir/replicas.err"; then
  echo "raw deployment path must reject non-canonical replica counts" >&2
  exit 1
fi
grep -q 'CONTROL_PLANE_SERVER_REPLICAS must be a non-negative integer without leading zeros' "$temp_dir/replicas.err" || {
  echo "non-canonical replica count did not produce a controlled error" >&2
  exit 1
}
! grep -q '^kubectl .* apply ' "$log" || {
  echo "raw deployment path applied resources after invalid replica count validation" >&2
  exit 1
}

echo "GKE control-plane deployment test passed"
