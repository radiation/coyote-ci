#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
real_helm="$(command -v helm)"
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT
mkdir "$temp_dir/bin"
log="$temp_dir/commands.log"

cat >"$temp_dir/bin/helm" <<'EOF'
#!/usr/bin/env bash
printf 'helm %s\n' "$*" >>"$COMMAND_LOG"
if [[ "$1" == "status" ]]; then
  printf '%s\n' '{"info":{"status":"deployed"}}'
elif [[ "$1" == "template" ]]; then
  exec "$REAL_HELM" "$@"
fi
EOF
cat >"$temp_dir/bin/kubectl" <<'EOF'
#!/usr/bin/env bash
printf 'kubectl %s\n' "$*" >>"$COMMAND_LOG"
if [[ "$*" == *" --show-managed-fields -o json"* ]]; then
  case "${NORMALIZATION_STATE:-no-scale-owner}" in
    kubectl-scale)
      printf '%s\n' '{"spec":{"replicas":1},"metadata":{"managedFields":[{"manager":"kubectl","operation":"Update","subresource":"scale","fieldsV1":{"f:spec":{"f:replicas":{}}}}]}}'
      ;;
    helm-replica-handoff)
      printf '%s\n' '{"spec":{"replicas":1},"metadata":{"managedFields":[{"manager":"helm-replica-handoff","operation":"Apply","subresource":"scale","fieldsV1":{"f:spec":{"f:replicas":{}}}}]}}'
      ;;
    helm-main)
      printf '%s\n' '{"spec":{"replicas":1},"metadata":{"managedFields":[{"manager":"helm","operation":"Apply","fieldsV1":{"f:spec":{"f:replicas":{}}}}]}}'
      ;;
    replica-mismatch)
      printf '%s\n' '{"spec":{"replicas":2},"metadata":{"managedFields":[]}}'
      ;;
    no-scale-owner)
      printf '%s\n' '{"spec":{"replicas":1},"metadata":{"managedFields":[]}}'
      ;;
    *)
      echo "unknown normalization mock state: ${NORMALIZATION_STATE}" >&2
      exit 1
      ;;
  esac
fi
EOF
cat >"$temp_dir/adoption-verifier" <<'EOF'
#!/usr/bin/env bash
printf 'adoption-verifier %s\n' "$*" >>"$COMMAND_LOG"
[[ "${GKE_HELM_ADOPT_VERIFY_ONLY:-}" == "true" ]]
EOF
chmod +x "$temp_dir/bin/helm" "$temp_dir/bin/kubectl" "$temp_dir/adoption-verifier"

run_normalizer() {
  env PATH="$temp_dir/bin:$PATH" \
  COMMAND_LOG="$log" \
  REAL_HELM="$real_helm" \
  GKE_HELM_ADOPT_SCRIPT="$temp_dir/adoption-verifier" \
  GKE_HELM_CONTROL_PLANE_VALUES="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" \
  GKE_HELM_WORKER_VALUES="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" \
  "$@"
}

dry_run_output="$(run_normalizer bash "$repo_root/scripts/gke-helm-normalize-ownership.sh")"
for deployment in coyote-server coyote-frontend coyote-kubernetes-worker-staging; do
  grep -q "patch deployment/$deployment --subresource=scale --type merge --field-manager=coyote-rollout" <<<"$dry_run_output" || {
    echo "dry-run did not plan coyote-rollout scale handoff for $deployment" >&2
    exit 1
  }
done
! grep -q '^kubectl .* patch ' "$log" || { echo "dry-run performed a scale patch" >&2; exit 1; }
! grep -q '^helm upgrade ' "$log" || { echo "dry-run performed a Helm upgrade" >&2; exit 1; }

: >"$log"
run_normalizer NORMALIZATION_STATE=kubectl-scale GKE_HELM_NORMALIZE_APPLY=true bash "$repo_root/scripts/gke-helm-normalize-ownership.sh" >/dev/null
grep -q '^adoption-verifier ' "$log" || { echo "normalization did not run structural parity verification" >&2; exit 1; }
[[ "$(grep -c '^kubectl -n .* get deployment/.* --show-managed-fields -o json$' "$log")" -eq 3 ]] || {
  echo "normalization must request managed fields for each ownership verification" >&2
  cat "$log" >&2
  exit 1
}
for deployment in coyote-server coyote-frontend coyote-kubernetes-worker-staging; do
  grep -q "patch deployment/$deployment --subresource=scale --type merge --field-manager=coyote-rollout --patch {\"spec\":{\"replicas\":1}}" "$log" || {
    echo "normalization did not use coyote-rollout scale patch for $deployment" >&2
    cat "$log" >&2
    exit 1
  }
done
[[ "$(grep -c '^kubectl .* patch deployment/.*--subresource=scale' "$log")" -eq 3 ]] || {
  echo "normalization must patch exactly three scale subresources" >&2
  exit 1
}
[[ "$(grep -c '^helm upgrade ' "$log")" -eq 2 ]] || {
  echo "normalization must run ordinary Helm upgrades to relinquish replica fields" >&2
  exit 1
}
! grep -q -- '--force-conflicts\|--force-replace\|helm uninstall\|kubectl .* delete\|managedFields' "$log" || {
  echo "normalization used an obsolete or destructive ownership operation" >&2
  cat "$log" >&2
  exit 1
}

for state in helm-replica-handoff no-scale-owner; do
  : >"$log"
  run_normalizer NORMALIZATION_STATE="$state" GKE_HELM_NORMALIZE_APPLY=true bash "$repo_root/scripts/gke-helm-normalize-ownership.sh" >/dev/null
done

for state in helm-main replica-mismatch; do
  : >"$log"
  if run_normalizer NORMALIZATION_STATE="$state" GKE_HELM_NORMALIZE_APPLY=true bash "$repo_root/scripts/gke-helm-normalize-ownership.sh" >/dev/null 2>&1; then
    echo "normalization must reject $state managed-field state" >&2
    exit 1
  fi
done

render_dir="$temp_dir/render"
mkdir "$render_dir"
helm template coyote-ci "$repo_root/deploy/helm/coyote-ci" --namespace coyote-ci --values "$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" >"$render_dir/control.yaml"
helm template coyote-ci-worker "$repo_root/deploy/helm/coyote-ci-worker" --namespace coyote-ci-staging --values "$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" >"$render_dir/worker.yaml"
ruby -ryaml -e '
  paths = ARGV
  deployments = paths.flat_map { |path| YAML.load_stream(File.read(path)).compact }.select { |item| item["kind"] == "Deployment" }
  %w[coyote-server coyote-frontend coyote-kubernetes-worker-staging].each do |name|
    deployment = deployments.find { |item| item.dig("metadata", "name") == name } or abort "missing #{name}"
    abort "#{name} still renders spec.replicas" if deployment.fetch("spec").key?("replicas")
  end
' "$render_dir/control.yaml" "$render_dir/worker.yaml"

internal_values_dir="$temp_dir/internal-values"
mkdir "$internal_values_dir"
ruby -ryaml -e '
  values = YAML.load_file(ARGV.fetch(0))
  %w[server frontend].each { |component| values.fetch(component)["replicasManagedExternally"] = false }
  File.write(ARGV.fetch(1), YAML.dump(values))
' "$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml" "$internal_values_dir/control.yaml"
ruby -ryaml -e '
  values = YAML.load_file(ARGV.fetch(0))
  values.fetch("worker")["replicasManagedExternally"] = false
  File.write(ARGV.fetch(1), YAML.dump(values))
' "$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml" "$internal_values_dir/worker.yaml"
"$real_helm" template coyote-ci "$repo_root/deploy/helm/coyote-ci" --namespace coyote-ci --values "$internal_values_dir/control.yaml" >"$render_dir/control-internal.yaml"
"$real_helm" template coyote-ci-worker "$repo_root/deploy/helm/coyote-ci-worker" --namespace coyote-ci-staging --values "$internal_values_dir/worker.yaml" >"$render_dir/worker-internal.yaml"
ruby -ryaml -e '
  deployments = ARGV.flat_map { |path| YAML.load_stream(File.read(path)).compact }.select { |item| item["kind"] == "Deployment" }
  %w[coyote-server coyote-frontend coyote-kubernetes-worker-staging].each do |name|
    deployment = deployments.find { |item| item.dig("metadata", "name") == name } or abort "missing #{name}"
    abort "#{name} does not render spec.replicas when externally managed replicas are disabled" unless deployment.fetch("spec").key?("replicas")
  end
' "$render_dir/control-internal.yaml" "$render_dir/worker-internal.yaml"

if rg -q 'kubectl .*scale deployment/(coyote-server|coyote-frontend|coyote-kubernetes-worker-staging)' "$repo_root/scripts/"*.sh; then
  echo "a normal script still uses kubectl scale for a Helm-managed deployment" >&2
  exit 1
fi

echo "GKE Helm ownership normalization test passed"
