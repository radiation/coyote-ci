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
if [[ "$*" == *"jsonpath={.metadata.labels.app"* ]]; then
  printf '%s' "${MOCK_MANAGED_BY:-}"
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
  grep -q "patch deployment/$deployment --subresource=scale --type merge --field-manager=coyote-raw-rollout --patch {\"spec\":{\"replicas\":0}}" "$log" || {
    echo "raw deployment path did not scale $deployment to zero before migration" >&2
    exit 1
  }
  grep -q "patch deployment/$deployment --subresource=scale --type merge --field-manager=coyote-raw-rollout --patch {\"spec\":{\"replicas\":1}}" "$log" || {
    echo "raw deployment path did not explicitly restore $deployment replicas" >&2
    exit 1
  }
done
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

echo "GKE control-plane deployment test passed"
