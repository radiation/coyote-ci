#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
control_chart="$repo_root/deploy/helm/coyote-ci"
worker_chart="$repo_root/deploy/helm/coyote-ci-worker"
control_values="${GKE_HELM_CONTROL_PLANE_VALUES:-$repo_root/.local/helm/gke-staging-control-plane-values.yaml}"
worker_values="${GKE_HELM_WORKER_VALUES:-$repo_root/.local/helm/gke-staging-worker-values.yaml}"
release_values="${GKE_HELM_RELEASE_VALUES:-}"
control_namespace="${GKE_HELM_CONTROL_PLANE_NAMESPACE:-coyote-ci}"
worker_namespace="${GKE_HELM_WORKER_NAMESPACE:-coyote-ci-staging}"
control_release="${GKE_HELM_CONTROL_PLANE_RELEASE:-coyote-ci}"
worker_release="${GKE_HELM_WORKER_RELEASE:-coyote-ci-worker}"
target_environment="${GKE_HELM_TARGET_ENVIRONMENT:-}"
expected_context="${GKE_HELM_EXPECTED_CONTEXT:-}"
expected_cluster_uid="${GKE_HELM_EXPECTED_CLUSTER_UID:-}"
timeout_seconds="${GKE_DEPLOY_TIMEOUT_SECONDS:-600}"
dry_run="${GKE_HELM_DRY_RUN:-false}"
operation="${GKE_HELM_OPERATION:-start}"
rollout_id="${HELM_ROLLOUT_ID:-}"
state_dir="${GKE_HELM_ROLLOUT_STATE_DIR:-$repo_root/.local/helm/rollouts}"
state_file=""
lock_name=""
lease_duration_seconds="${GKE_HELM_LEASE_DURATION_SECONDS:-900}"
unmanaged_mcp_database_isolation_confirmed="${GKE_HELM_UNMANAGED_MCP_DATABASE_ISOLATION_CONFIRMED:-false}"
restore_replicas_confirmed="${GKE_HELM_RESTORE_REPLICAS_CONFIRMED:-false}"
missing_state_reconciliation_confirmed="${GKE_HELM_MISSING_STATE_RECONCILIATION_CONFIRMED:-false}"
migration_job=""
last_confirmed_phase="preflight"
migration_status="unknown"
workloads_may_be_unavailable=false
lease_held=false
execution_id=""
lease_lost_file=""
lease_renewal_pid=""

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

run() {
  printf '+'
  printf ' %q' "$@"
  printf '\n'
  if [[ "$dry_run" != "true" ]]; then
    if [[ "$lease_held" == "true" ]]; then
      ensure_lease_healthy
    fi
    "$@"
  fi
}

fail() {
  echo "rollout_id=${rollout_id:-<unset>} environment=${target_environment:-<unset>} last_confirmed_phase=$last_confirmed_phase migration_status=$migration_status workloads_may_be_unavailable=$workloads_may_be_unavailable" >&2
  echo "intended_replicas: ${intended_replicas_summary:-unavailable}" >&2
  echo "safe next steps: GKE_HELM_OPERATION=status HELM_ROLLOUT_ID=${rollout_id:-REQUIRED} bash scripts/gke-helm-rollout.sh" >&2
  echo "for explicit replica recovery: GKE_HELM_OPERATION=restore-replicas GKE_HELM_RESTORE_REPLICAS_CONFIRMED=true HELM_ROLLOUT_ID=${rollout_id:-REQUIRED} bash scripts/gke-helm-rollout.sh" >&2
  echo "$*" >&2
  if [[ "$lease_held" == "true" ]]; then
    lease_release || true
  fi
  exit 1
}

on_error() {
  local exit_code="$1"
  local line="$2"
  local command="$3"
  trap - ERR
  if [[ -n "$state_file" && -f "$state_file" && "$dry_run" != "true" ]]; then
    state_set_failure "command failed at script line $line (exit $exit_code)" || true
  fi
  if [[ "$lease_held" == "true" ]]; then
    lease_release || true
  fi
  echo "rollout_id=${rollout_id:-<unset>} environment=${target_environment:-<unset>} last_confirmed_phase=$last_confirmed_phase migration_status=$migration_status workloads_may_be_unavailable=$workloads_may_be_unavailable" >&2
  echo "intended_replicas: ${intended_replicas_summary:-unavailable}" >&2
  echo "safe next steps: GKE_HELM_OPERATION=status HELM_ROLLOUT_ID=${rollout_id:-REQUIRED} bash scripts/gke-helm-rollout.sh" >&2
  echo "rollout failed at script line $line while running: $command (exit $exit_code)" >&2
  exit "$exit_code"
}

trap 'on_error "$?" "$LINENO" "$BASH_COMMAND"' ERR

require_command helm
require_command kubectl
require_command ruby

[[ -f "$control_values" ]] || fail "control-plane values file does not exist: $control_values"
[[ -f "$worker_values" ]] || fail "worker values file does not exist: $worker_values"
[[ -z "$release_values" || -f "$release_values" ]] || fail "release values file does not exist: $release_values"
[[ "$dry_run" == "true" || "$dry_run" == "false" ]] || fail "GKE_HELM_DRY_RUN must be true or false"
[[ "$operation" == "start" || "$operation" == "status" || "$operation" == "resume" || "$operation" == "restore-replicas" ]] || fail "GKE_HELM_OPERATION must be start, status, resume, or restore-replicas"
[[ "$unmanaged_mcp_database_isolation_confirmed" == "true" || "$unmanaged_mcp_database_isolation_confirmed" == "false" ]] || fail "GKE_HELM_UNMANAGED_MCP_DATABASE_ISOLATION_CONFIRMED must be true or false"
[[ "$restore_replicas_confirmed" == "true" || "$restore_replicas_confirmed" == "false" ]] || fail "GKE_HELM_RESTORE_REPLICAS_CONFIRMED must be true or false"
[[ "$missing_state_reconciliation_confirmed" == "true" || "$missing_state_reconciliation_confirmed" == "false" ]] || fail "GKE_HELM_MISSING_STATE_RECONCILIATION_CONFIRMED must be true or false"
[[ "$target_environment" =~ ^[a-z][a-z0-9-]*$ ]] || fail "GKE_HELM_TARGET_ENVIRONMENT must be an explicit lowercase environment name"
[[ "$control_namespace" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || fail "invalid control-plane namespace: $control_namespace"
[[ "$worker_namespace" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || fail "invalid worker namespace: $worker_namespace"
[[ "$control_release" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || fail "invalid control-plane release: $control_release"
[[ "$worker_release" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || fail "invalid worker release: $worker_release"
[[ "$lease_duration_seconds" =~ ^[1-9][0-9]*$ ]] || fail "GKE_HELM_LEASE_DURATION_SECONDS must be a positive integer"

if [[ "$operation" != "status" || "$dry_run" != "true" ]]; then
  [[ "$rollout_id" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ && ${#rollout_id} -le 48 ]] || fail "HELM_ROLLOUT_ID must be a Kubernetes-safe lowercase identifier of at most 48 characters"
fi
if [[ "$target_environment" == "production" && "$dry_run" != "true" ]]; then
  [[ -n "$expected_context" ]] || fail "GKE_HELM_EXPECTED_CONTEXT is required for state-changing production operations"
  [[ -n "$expected_cluster_uid" ]] || fail "GKE_HELM_EXPECTED_CLUSTER_UID is required for state-changing production operations"
  [[ "$control_namespace" == "coyote-ci" && "$worker_namespace" == "coyote-ci" ]] || fail "production rollout namespaces must both be coyote-ci"
  [[ "$control_release" == "coyote-ci" && "$worker_release" == "coyote-ci-worker" ]] || fail "production Helm release names must be coyote-ci and coyote-ci-worker"
fi

state_file="$state_dir/${target_environment}-${rollout_id}.json"
lock_name="coyote-rollout-${control_release}"
migration_job="coyote-migrate-${rollout_id}"
execution_id="${rollout_id}-$$-${RANDOM}"
lease_lost_file="$(mktemp "${TMPDIR:-/tmp}/coyote-rollout-lease.XXXXXX")"
rm -f "$lease_lost_file"

external_replica_value() {
  local values="$1"
  local section="$2"
  ruby -ryaml -e '
    values = YAML.load_file(ARGV.fetch(0))
    section = values.fetch(ARGV.fetch(1))
    abort "#{ARGV.fetch(1)}.replicasManagedExternally must be true for GKE Helm rollout" unless section["replicasManagedExternally"] == true
    replicas = section["replicas"]
    abort "#{ARGV.fetch(1)}.replicas must be a non-negative integer" unless replicas.is_a?(Integer) && replicas >= 0
    puts replicas
  ' "$values" "$section"
}

worker_deployment_name() {
  ruby -ryaml -e '
    name = YAML.load_file(ARGV.fetch(0)).fetch("worker").fetch("name", "coyote-kubernetes-worker-staging")
    abort "worker.name must be a Kubernetes-safe name" unless name.match?(/\A[a-z0-9]([-a-z0-9]*[a-z0-9])?\z/)
    puts name
  ' "$worker_values"
}

mcp_enabled() {
  ruby -ryaml -e 'puts YAML.load_file(ARGV.fetch(0)).fetch("mcp", {}).fetch("enabled", false) == true' "$control_values"
}

release_identity() {
  ruby -ryaml -rdigest -rjson -e '
    def merge(left, right)
      left.merge(right) { |_key, old, new| old.is_a?(Hash) && new.is_a?(Hash) ? merge(old, new) : new }
    end
    control = YAML.load_file(ARGV.fetch(0)) || {}
    worker = YAML.load_file(ARGV.fetch(1)) || {}
    overlay = ARGV.fetch(2).empty? ? {} : (YAML.load_file(ARGV.fetch(2)) || {})
    control = merge(control, overlay)
    worker = merge(worker, overlay)
    images = {
      "server" => control.dig("images", "server"),
      "frontend" => control.dig("images", "frontend"),
      "migrate" => control.dig("images", "migrate"),
      "worker" => worker.dig("images", "worker")
    }
    abort "release images must use complete SHA-256 digest references" unless images.values.all? { |value| value.is_a?(String) && value.match?(/\A.+@sha256:[0-9a-f]{64}\z/) }
    release = control.fetch("release", {})
    record = {
      "fingerprint" => Digest::SHA256.hexdigest(JSON.generate(images.sort.to_h.merge("release_version" => release["version"], "manifest_digest" => release["manifestDigest"]))),
      "images" => images,
      "release_version" => release["version"],
      "manifest_digest" => release["manifestDigest"]
    }
    puts JSON.generate(record)
  ' "$control_values" "$worker_values" "$release_values"
}

current_context() {
  kubectl config current-context
}

current_cluster_uid() {
  kubectl get namespace kube-system -o jsonpath='{.metadata.uid}'
}

verify_target_identity() {
  local actual_context actual_cluster_uid
  actual_context="$(current_context)"
  [[ -n "$actual_context" ]] || fail "kubectl did not report an active context"
  if [[ -n "$expected_context" && "$actual_context" != "$expected_context" ]]; then
    fail "active Kubernetes context $actual_context does not match expected context $expected_context"
  fi
  if [[ "$target_environment" == "production" || -n "$expected_cluster_uid" ]]; then
    actual_cluster_uid="$(current_cluster_uid)"
    [[ "$actual_cluster_uid" == "$expected_cluster_uid" ]] || fail "active Kubernetes cluster identity does not match the configured target"
  else
    actual_cluster_uid=""
  fi
  active_context="$actual_context"
  active_cluster_uid="$actual_cluster_uid"
}

state_create() {
  local replicas_json="$1"
  local release_json="$2"
  local revisions_json="$3"
  local observed_images_json="$4"
  mkdir -p "$state_dir"
  ruby -rjson -rtime -e '
    path, rollout_id, environment, context, cluster_uid, control_namespace, worker_namespace, control_release, worker_release, migration_job, replicas_json, release_json, revisions_json, observed_images_json = ARGV
    abort "rollout state already exists: #{path}" if File.exist?(path)
    state = {
      "schema_version" => 1,
      "rollout_id" => rollout_id,
      "target_environment" => environment,
      "context" => context,
      "cluster_uid" => cluster_uid,
      "control_namespace" => control_namespace,
      "worker_namespace" => worker_namespace,
      "control_release" => control_release,
      "worker_release" => worker_release,
      "migration_job" => migration_job,
      "intended_replicas" => JSON.parse(replicas_json),
      "release" => JSON.parse(release_json),
      "observed_images" => JSON.parse(observed_images_json),
      "helm_revisions" => JSON.parse(revisions_json),
      "phases" => {},
      "migration_status" => "not_started",
      "failure" => nil
    }
    temporary = "#{path}.tmp-#{Process.pid}"
    File.write(temporary, JSON.pretty_generate(state) + "\n")
    File.rename(temporary, path)
  ' "$state_file" "$rollout_id" "$target_environment" "$active_context" "$active_cluster_uid" "$control_namespace" "$worker_namespace" "$control_release" "$worker_release" "$migration_job" "$replicas_json" "$release_json" "$revisions_json" "$observed_images_json"
}

state_update() {
  local action="$1"
  local first="${2:-}"
  local second="${3:-}"
  ruby -rjson -rtime -e '
    path, action, first, second = ARGV
    state = JSON.parse(File.read(path))
    case action
    when "phase"
      state.fetch("phases")[first] = second
    when "migration"
      state["migration_status"] = first
    when "failure"
      state["failure"] = {"message" => first, "at" => Time.now.utc.iso8601}
    when "clear_failure"
      state["failure"] = nil
    when "helm_revision"
      state.fetch("helm_revisions")[first] = second
    else
      abort "unknown state update #{action}"
    end
    temporary = "#{path}.tmp-#{Process.pid}"
    File.write(temporary, JSON.pretty_generate(state) + "\n")
    File.rename(temporary, path)
  ' "$state_file" "$action" "$first" "$second"
}

state_set_phase() {
  local phase="$1"
  local status="$2"
  [[ "$dry_run" == "true" ]] || state_update phase "$phase" "$status"
  if [[ "$status" == "completed" ]]; then
    last_confirmed_phase="$phase"
  fi
}

state_set_migration() {
  migration_status="$1"
  [[ "$dry_run" == "true" ]] || state_update migration "$migration_status"
}

state_set_failure() {
  local message="$1"
  state_update failure "$message"
}

state_load_and_verify() {
  [[ -f "$state_file" ]] || fail "rollout state is missing: $state_file; inspect existing migration Jobs and Helm releases before setting GKE_HELM_MISSING_STATE_RECONCILIATION_CONFIRMED=true for a new rollout"
  state_values="$(ruby -rjson -e '
    state = JSON.parse(File.read(ARGV.fetch(0)))
    keys = %w[rollout_id target_environment context cluster_uid control_namespace worker_namespace control_release worker_release migration_job]
    keys.each { |key| puts "#{key}=#{state.fetch(key, "")}" }
    puts "release_fingerprint=#{state.dig("release", "fingerprint")}"
    puts "migration_status=#{state["migration_status"]}"
    puts "last_confirmed_phase=#{state.fetch("phases", {}).select { |_key, value| value == "completed" }.keys.last || "preflight"}"
    puts "intended_replicas_json=#{JSON.generate(state.fetch("intended_replicas"))}"
    puts "control_revision=#{state.dig("helm_revisions", "control")}"
    puts "worker_revision=#{state.dig("helm_revisions", "worker")}"
    puts "observed_images_json=#{JSON.generate(state.fetch("observed_images"))}"
    puts "control_phase=#{state.dig("phases", "control-plane")}"
    puts "worker_phase=#{state.dig("phases", "worker")}"
  ' "$state_file")"
  while IFS= read -r item; do
    case "$item" in
      rollout_id=*) state_rollout_id="${item#*=}" ;;
      target_environment=*) state_environment="${item#*=}" ;;
      context=*) state_context="${item#*=}" ;;
      cluster_uid=*) state_cluster_uid="${item#*=}" ;;
      control_namespace=*) state_control_namespace="${item#*=}" ;;
      worker_namespace=*) state_worker_namespace="${item#*=}" ;;
      control_release=*) state_control_release="${item#*=}" ;;
      worker_release=*) state_worker_release="${item#*=}" ;;
      migration_job=*) state_migration_job="${item#*=}" ;;
      release_fingerprint=*) state_release_fingerprint="${item#*=}" ;;
      migration_status=*) migration_status="${item#*=}" ;;
      last_confirmed_phase=*) last_confirmed_phase="${item#*=}" ;;
      intended_replicas_json=*) intended_replicas_json="${item#*=}" ;;
      control_revision=*) state_control_revision="${item#*=}" ;;
      worker_revision=*) state_worker_revision="${item#*=}" ;;
      observed_images_json=*) observed_images_json="${item#*=}" ;;
      control_phase=*) state_control_phase="${item#*=}" ;;
      worker_phase=*) state_worker_phase="${item#*=}" ;;
    esac
  done <<<"$state_values"
  [[ "$state_rollout_id" == "$rollout_id" && "$state_environment" == "$target_environment" && "$state_control_namespace" == "$control_namespace" && "$state_worker_namespace" == "$worker_namespace" && "$state_control_release" == "$control_release" && "$state_worker_release" == "$worker_release" ]] || fail "rollout state does not match the requested target"
  [[ -z "$state_context" || "$state_context" == "$active_context" ]] || fail "rollout state context does not match the active Kubernetes context"
  [[ -z "$state_cluster_uid" || "$state_cluster_uid" == "$active_cluster_uid" ]] || fail "rollout state cluster identity does not match the active Kubernetes cluster"
  [[ "$state_migration_job" == "$migration_job" ]] || fail "rollout state migration Job does not match the requested rollout ID"
  [[ "$state_release_fingerprint" == "$release_fingerprint" ]] || fail "release inputs changed since rollout state was created; refusing to resume"
  intended_replicas_summary="$(ruby -rjson -e 'puts JSON.parse(ARGV.fetch(0)).map { |name, replicas| "#{name}=#{replicas}" }.join(", ")' "$intended_replicas_json")"
}

deployment_replicas() {
  local namespace="$1"
  local deployment="$2"
  local replicas
  replicas="$(kubectl -n "$namespace" get "deployment/$deployment" -o jsonpath='{.spec.replicas}')"
  [[ "$replicas" =~ ^[0-9]+$ ]] || fail "could not capture replicas for deployment/$deployment in namespace $namespace"
  printf '%s\n' "$replicas"
}

deployment_image() {
  local namespace="$1"
  local deployment="$2"
  local container="$3"
  kubectl -n "$namespace" get "deployment/$deployment" -o json | ruby -rjson -e '
    containers = JSON.parse(STDIN.read).dig("spec", "template", "spec", "containers") || []
    image = containers.find { |item| item["name"] == ARGV.fetch(0) }.to_h["image"]
    abort "container #{ARGV.fetch(0)} image is missing" if image.to_s.empty?
    puts image
  ' "$container"
}

capture_observed_images() {
  observed_images_json="$(ruby -rjson -e 'puts JSON.generate({"server" => ARGV[0], "frontend" => ARGV[1], "worker" => ARGV[2]})' \
    "$(deployment_image "$control_namespace" coyote-server server)" \
    "$(deployment_image "$control_namespace" coyote-frontend frontend)" \
    "$(deployment_image "$worker_namespace" "$worker_deployment" worker)")"
}

validate_resume_reconciliation() {
  local current_server current_frontend current_worker current_control_revision current_worker_revision
  current_server="$(deployment_image "$control_namespace" coyote-server server)"
  current_frontend="$(deployment_image "$control_namespace" coyote-frontend frontend)"
  current_worker="$(deployment_image "$worker_namespace" "$worker_deployment" worker)"
  ruby -rjson -e '
    observed, release, server, frontend, worker, control_phase, worker_phase = ARGV
    observed = JSON.parse(observed)
    release = JSON.parse(release).fetch("images")
    actual = {"server" => server, "frontend" => frontend, "worker" => worker}
    actual.each do |name, image|
      allowed = [observed.fetch(name), release.fetch(name)]
      abort "deployment #{name} image does not match checkpointed or target immutable image" unless allowed.include?(image)
    end
    abort "completed control-plane phase is not running target server image" if control_phase == "completed" && server != release.fetch("server")
    abort "completed control-plane phase is not running target frontend image" if control_phase == "completed" && frontend != release.fetch("frontend")
    abort "completed worker phase is not running target worker image" if worker_phase == "completed" && worker != release.fetch("worker")
  ' "$observed_images_json" "$release_json" "$current_server" "$current_frontend" "$current_worker" "${state_control_phase:-}" "${state_worker_phase:-}" || fail "resume deployment images do not reconcile with the checkpointed rollout"
  current_control_revision="$(helm_revision "$control_release" "$control_namespace")"
  current_worker_revision="$(helm_revision "$worker_release" "$worker_namespace")"
  if [[ "${state_control_revision:-unknown}" != "unknown" && "$current_control_revision" != "$state_control_revision" ]]; then
    fail "control-plane Helm revision changed since the checkpoint; inspect the release before resuming"
  fi
  if [[ "${state_worker_revision:-unknown}" != "unknown" && "$current_worker_revision" != "$state_worker_revision" ]]; then
    fail "worker Helm revision changed since the checkpoint; inspect the release before resuming"
  fi
}

capture_intended_replicas() {
  local server frontend worker mcp
  server="$(deployment_replicas "$control_namespace" coyote-server)"
  frontend="$(deployment_replicas "$control_namespace" coyote-frontend)"
  worker="$(deployment_replicas "$worker_namespace" "$worker_deployment")"
  mcp="null"
  if [[ "$mcp_is_desired" == "true" && "$mcp_state" == "managed" ]]; then
    mcp="$(deployment_replicas "$control_namespace" coyote-mcp)"
  elif [[ "$mcp_is_desired" == "true" ]]; then
    mcp="$mcp_replicas"
  fi
  intended_replicas_json="$(ruby -rjson -e 'puts JSON.generate({"server" => Integer(ARGV[0]), "frontend" => Integer(ARGV[1]), "worker" => Integer(ARGV[2]), "mcp" => ARGV[3] == "null" ? nil : Integer(ARGV[3])})' "$server" "$frontend" "$worker" "$mcp")"
  intended_replicas_summary="$(ruby -rjson -e 'puts JSON.parse(ARGV.fetch(0)).compact.map { |name, replicas| "#{name}=#{replicas}" }.join(", ")' "$intended_replicas_json")"
}

replica_from_state() {
  ruby -rjson -e 'value = JSON.parse(ARGV.fetch(0)).fetch(ARGV.fetch(1)); abort "no intended replica count for #{ARGV.fetch(1)}" if value.nil?; puts value' "$intended_replicas_json" "$1"
}

helm_revision() {
  local release="$1"
  local namespace="$2"
  helm status "$release" --namespace "$namespace" --output json 2>/dev/null | ruby -rjson -e 'document = JSON.parse(STDIN.read); puts document.dig("version") || document.dig("info", "status") || "unknown"' 2>/dev/null || printf '%s\n' unknown
}

mcp_deployment_state() {
  local deployment
  if ! deployment="$(kubectl -n "$control_namespace" get deployment/coyote-mcp --ignore-not-found -o json)"; then
    echo "failed to determine coyote-mcp ownership" >&2
    return 2
  fi
  if [[ -z "$deployment" ]]; then
    printf '%s\n' absent
    return
  fi
  ruby -rjson -rtime -e '
    metadata = JSON.parse(STDIN.read).fetch("metadata", {})
    actual = [
      metadata.fetch("labels", {})["app.kubernetes.io/managed-by"],
      metadata.fetch("annotations", {})["meta.helm.sh/release-name"],
      metadata.fetch("annotations", {})["meta.helm.sh/release-namespace"]
    ]
    expected = ["Helm", ARGV.fetch(0), ARGV.fetch(1)]
    puts actual == expected ? "managed" : actual.all?(&:nil?) ? "unowned" : "other"
  ' "$control_release" "$control_namespace" <<<"$deployment"
}

scale_deployment() {
  local namespace="$1"
  local deployment="$2"
  local replicas="$3"
  run kubectl -n "$namespace" patch "deployment/$deployment" --subresource=scale --type merge --field-manager=coyote-rollout --patch "{\"spec\":{\"replicas\":$replicas}}"
}

lease_manifest() {
  local resource_version="${1:-}"
  local holder_identity="${2-$execution_id}"
  ruby -rjson -rtime -e '
    name, namespace, holder, duration, resource_version = ARGV
    metadata = {"name" => name, "namespace" => namespace}
    metadata["resourceVersion"] = resource_version unless resource_version.empty?
    document = {
      "apiVersion" => "coordination.k8s.io/v1",
      "kind" => "Lease",
      "metadata" => metadata,
      "spec" => {
        "holderIdentity" => holder,
        "leaseDurationSeconds" => Integer(duration),
        "acquireTime" => Time.now.utc.iso8601(6),
        "renewTime" => Time.now.utc.iso8601(6)
      }
    }
    puts JSON.generate(document)
  ' "$lock_name" "$control_namespace" "$holder_identity" "$lease_duration_seconds" "$resource_version"
}

lease_acquire() {
  local live state holder resource_version
  live="$(kubectl -n "$control_namespace" get "lease/$lock_name" --ignore-not-found -o json)"
  if [[ -z "$live" ]]; then
    if [[ "$dry_run" == "true" ]]; then
      echo "+ kubectl -n $control_namespace create -f - # rollout Lease $lock_name"
    else
      lease_manifest | kubectl -n "$control_namespace" create -f - || fail "another operator acquired rollout Lease $lock_name; inspect it before retrying"
    fi
    lease_held=true
    return
  fi
  state="$(ruby -rjson -rtime -e '
    document = JSON.parse(STDIN.read)
    spec = document.fetch("spec", {})
    holder = spec["holderIdentity"].to_s
    duration = Integer(spec.fetch("leaseDurationSeconds", 0))
    renewed = spec["renewTime"] || spec["acquireTime"]
    stale = false
    if !renewed.nil? && duration > 0
      stale = Time.parse(renewed) + duration < Time.now.utc
    end
    puts [holder.empty? ? "__released__" : holder, stale, document.dig("metadata", "resourceVersion").to_s].join("\t")
  ' <<<"$live")"
  IFS=$'\t' read -r holder stale resource_version <<<"$state"
  if [[ "$holder" == "$execution_id" ]]; then
    lease_held=true
    return
  fi
  if [[ "$holder" == "__released__" ]]; then
    if [[ "$dry_run" == "true" ]]; then
      echo "+ kubectl -n $control_namespace replace -f - # acquire released rollout Lease $lock_name"
    else
      lease_manifest "$resource_version" | kubectl -n "$control_namespace" replace -f - || fail "released rollout Lease $lock_name changed before ownership could be acquired; retry after inspection"
    fi
    lease_held=true
    return
  fi
  if [[ "$stale" != "true" ]]; then
    fail "another rollout execution owns Lease $lock_name (holder=$holder); a second process cannot use rollout ID $rollout_id until the active Lease expires or is reconciled"
  fi
  if [[ "$dry_run" == "true" ]]; then
    echo "+ kubectl -n $control_namespace replace -f - # take expired rollout Lease $lock_name"
  else
    lease_manifest "$resource_version" | kubectl -n "$control_namespace" replace -f - || fail "expired rollout Lease $lock_name changed before ownership could be taken; retry after inspection"
  fi
  lease_held=true
}

lease_release() {
  [[ "$dry_run" == "true" ]] && return
  local live holder resource_version
  stop_lease_renewal
  live="$(kubectl -n "$control_namespace" get "lease/$lock_name" --ignore-not-found -o json)"
  [[ -z "$live" ]] && { lease_held=false; return; }
  IFS=$'\t' read -r holder resource_version < <(ruby -rjson -e 'document = JSON.parse(STDIN.read); puts [document.dig("spec", "holderIdentity").to_s, document.dig("metadata", "resourceVersion").to_s].join("\t")' <<<"$live")
  [[ "$holder" == "$execution_id" && -n "$resource_version" ]] || { lease_held=false; return; }
  if ! lease_manifest "$resource_version" "" | kubectl -n "$control_namespace" replace -f - >/dev/null; then
    echo "rollout Lease $lock_name changed before release; leaving the current owner untouched" >&2
  fi
  lease_held=false
}

lease_renew_once() {
  [[ "$dry_run" == "true" ]] && return 0
  local live holder resource_version
  live="$(kubectl -n "$control_namespace" get "lease/$lock_name" --ignore-not-found -o json)"
  [[ -n "$live" ]] || return 1
  IFS=$'\t' read -r holder resource_version < <(ruby -rjson -e 'document = JSON.parse(STDIN.read); puts [document.dig("spec", "holderIdentity").to_s, document.dig("metadata", "resourceVersion").to_s].join("\t")' <<<"$live")
  [[ "$holder" == "$execution_id" ]] || return 1
  lease_manifest "$resource_version" | kubectl -n "$control_namespace" replace -f -
}

lease_verify_owned() {
  ensure_lease_healthy
  lease_renew_once || fail "rollout Lease $lock_name is missing, owned by another execution, or changed while renewing; refusing to continue"
}

ensure_lease_healthy() {
  if [[ -s "$lease_lost_file" ]]; then
    fail "$(cat "$lease_lost_file"); an in-flight Kubernetes command may have changed state, so inspect its result before recovery"
  fi
}

start_lease_renewal() {
  [[ "$dry_run" == "true" ]] && return
  [[ -z "$lease_renewal_pid" ]] || return
  local renewal_interval
  renewal_interval=$(( lease_duration_seconds / 3 ))
  (( renewal_interval > 0 )) || renewal_interval=1
  rm -f "$lease_lost_file"
  (
    while sleep "$renewal_interval"; do
      if ! lease_renew_once; then
        printf '%s\n' "Lease ownership was lost while a blocking command was running" >"$lease_lost_file"
        exit 0
      fi
    done
  ) &
  lease_renewal_pid=$!
}

stop_lease_renewal() {
  if [[ -n "$lease_renewal_pid" ]]; then
    kill "$lease_renewal_pid" 2>/dev/null || true
    wait "$lease_renewal_pid" 2>/dev/null || true
    lease_renewal_pid=""
  fi
}

migration_job_status() {
  local job
  job="$(kubectl -n "$control_namespace" get "job/$migration_job" --ignore-not-found -o json)"
  if [[ -z "$job" ]]; then
    printf '%s\n' absent
    return
  fi
  ruby -rjson -e '
    conditions = JSON.parse(STDIN.read).dig("status", "conditions") || []
    if conditions.any? { |condition| condition["type"] == "Complete" && condition["status"] == "True" }
      puts "complete"
    elsif conditions.any? { |condition| condition["type"] == "Failed" && condition["status"] == "True" }
      puts "failed"
    else
      puts "running"
    end
  ' <<<"$job"
}

migration_job_image() {
  local job
  job="$(kubectl -n "$control_namespace" get "job/$migration_job" --ignore-not-found -o json)"
  [[ -n "$job" ]] || return 1
  ruby -rjson -e '
    containers = JSON.parse(STDIN.read).dig("spec", "template", "spec", "containers") || []
    image = containers.find { |item| item["name"] == "migrate" }.to_h["image"]
    abort "migration Job does not contain a migrate container image" if image.to_s.empty?
    puts image
  ' <<<"$job"
}

ensure_no_untracked_migration() {
  [[ "$target_environment" == "production" ]] || return 0
  local jobs
  jobs="$(kubectl -n "$control_namespace" get jobs -l app.kubernetes.io/name=coyote-migrate -o json)"
  if ruby -rjson -e 'exit((JSON.parse(STDIN.read)["items"] || []).empty? ? 0 : 1)' <<<"$jobs"; then
    return
  fi
  if [[ "$missing_state_reconciliation_confirmed" == "true" ]]; then
    fail "migration Jobs exist but local rollout state is missing; the reconciliation acknowledgement does not authorize a new rollout or migration. Recover the original checkpoint or escalate with the Job, Helm revisions, deployed image digests, and replica counts"
  fi
  fail "migration Jobs exist but local rollout state is missing; inspect the Job, Helm revisions, deployed image digests, and replica counts. The acknowledgement variable cannot authorize a new rollout"
}

restore_replicas() {
  [[ "$restore_replicas_confirmed" == "true" ]] || fail "replica restoration requires GKE_HELM_RESTORE_REPLICAS_CONFIRMED=true because migration compatibility is uncertain"
  state_set_phase restore-replicas attempted
  scale_deployment "$control_namespace" coyote-server "$(replica_from_state server)"
  scale_deployment "$control_namespace" coyote-frontend "$(replica_from_state frontend)"
  if [[ "$(replica_from_state mcp 2>/dev/null || true)" != "" ]]; then
    scale_deployment "$control_namespace" coyote-mcp "$(replica_from_state mcp)"
  fi
  scale_deployment "$worker_namespace" "$worker_deployment" "$(replica_from_state worker)"
  state_set_phase restore-replicas scaled
  run kubectl -n "$control_namespace" rollout status deployment/coyote-server --timeout="${timeout_seconds}s"
  run kubectl -n "$control_namespace" rollout status deployment/coyote-frontend --timeout="${timeout_seconds}s"
  if [[ "$(replica_from_state mcp 2>/dev/null || true)" != "" ]]; then
    run kubectl -n "$control_namespace" rollout status deployment/coyote-mcp --timeout="${timeout_seconds}s"
  fi
  run kubectl -n "$worker_namespace" rollout status "deployment/$worker_deployment" --timeout="${timeout_seconds}s"
  state_set_phase restore-replicas completed
  workloads_may_be_unavailable=false
  echo "replicas restored from rollout state: $intended_replicas_summary"
}

print_status() {
  if [[ ! -f "$state_file" ]]; then
    echo "no local rollout state exists for rollout_id=$rollout_id environment=$target_environment"
    echo "inspect migration Jobs and Helm releases before starting a rollout with this ID"
    return
  fi
  cat "$state_file"
  echo
  echo "migration_job_status=$(migration_job_status)"
  echo "control_helm_revision=$(helm_revision "$control_release" "$control_namespace")"
  echo "worker_helm_revision=$(helm_revision "$worker_release" "$worker_namespace")"
}

worker_deployment="$(worker_deployment_name)"
control_values_args=(--values "$control_values")
worker_values_args=(--values "$worker_values")
if [[ -n "$release_values" ]]; then
  control_values_args+=(--values "$release_values")
  worker_values_args+=(--values "$release_values")
fi
mcp_replicas=""
mcp_is_desired=false
if [[ "$(mcp_enabled)" == "true" ]]; then
  mcp_is_desired=true
  mcp_replicas="$(external_replica_value "$control_values" mcp)"
fi

release_json="$(release_identity)"
release_fingerprint="$(ruby -rjson -e 'puts JSON.parse(STDIN.read).fetch("fingerprint")' <<<"$release_json")"
expected_migration_image="$(ruby -rjson -e 'puts JSON.parse(STDIN.read).fetch("images").fetch("migrate")' <<<"$release_json")"

if [[ "$operation" == "status" ]]; then
  verify_target_identity
  print_status
  exit 0
fi

verify_target_identity

# Rendering validates the supplied digest-pinned images and chart-required inputs before mutation.
helm template "$control_release" "$control_chart" --namespace "$control_namespace" "${control_values_args[@]}" >/dev/null
helm template "$worker_release" "$worker_chart" --namespace "$worker_namespace" "${worker_values_args[@]}" >/dev/null
gateway_manifest="$(helm template "$control_release" "$control_chart" --namespace "$control_namespace" "${control_values_args[@]}" --show-only templates/gateway.yaml)"
migrator_prerequisite_manifest="$(helm template "$control_release" "$control_chart" --namespace "$control_namespace" "${control_values_args[@]}" --show-only templates/identities-rbac.yaml --show-only templates/secrets.yaml | ruby -ryaml -e '
  documents = YAML.load_stream(STDIN.read).compact
  required = [["v1", "ServiceAccount", "coyote-migrate"], ["secrets-store.csi.x-k8s.io/v1", "SecretProviderClass", "coyote-migrate-secrets"]]
  selected = required.map { |api_version, kind, name| documents.find { |document| document["apiVersion"] == api_version && document["kind"] == kind && document.dig("metadata", "name") == name } }
  missing = required.zip(selected).map { |resource, document| resource.join("/") unless document }.compact
  abort "missing migrator prerequisite resources: #{missing.join(", ")}" unless missing.empty?
  puts YAML.dump_stream(*selected)
')"
migration_manifest="$(helm template "$control_release" "$control_chart" --namespace "$control_namespace" "${control_values_args[@]}" --show-only templates/migration-job.yaml --set migration.render=true --set "migration.runID=$rollout_id")"
[[ -n "$migration_manifest" ]] || fail "migration render did not produce a Job"
helm status "$control_release" --namespace "$control_namespace" >/dev/null || fail "control-plane Helm release is not installed; ownership adoption must complete before Helm rollout"
helm status "$worker_release" --namespace "$worker_namespace" >/dev/null || fail "worker Helm release is not installed; ownership adoption must complete before Helm rollout"

mcp_state="$(mcp_deployment_state)" || exit $?
case "$mcp_state:$mcp_is_desired" in
  managed:false) fail "coyote-mcp is owned by Helm release $control_release but mcp.enabled is false; refusing a rollout that would delete it" ;;
  unowned:true|other:true) fail "coyote-mcp exists but is not owned by Helm release $control_release; refusing to adopt or scale it" ;;
  unowned:false)
    [[ "$unmanaged_mcp_database_isolation_confirmed" == "true" ]] || fail "coyote-mcp is unowned and disabled; verify database isolation, then set GKE_HELM_UNMANAGED_MCP_DATABASE_ISOLATION_CONFIRMED=true"
    echo "coyote-mcp is unowned; operator confirmed database isolation, leaving it unchanged" >&2
    ;;
  other:false) fail "coyote-mcp is owned by another or partial Helm release; refusing to continue while it remains unmanaged by $control_release" ;;
esac

if [[ "$operation" == "start" ]]; then
  [[ ! -f "$state_file" ]] || fail "rollout state already exists: $state_file; use GKE_HELM_OPERATION=status, resume, or restore-replicas"
  ensure_no_untracked_migration
  capture_intended_replicas
  capture_observed_images
  helm_revisions_json="$(ruby -rjson -e 'puts JSON.generate({"control" => ARGV[0], "worker" => ARGV[1]})' "$(helm_revision "$control_release" "$control_namespace")" "$(helm_revision "$worker_release" "$worker_namespace")")"
  [[ "$dry_run" == "true" ]] || state_create "$intended_replicas_json" "$release_json" "$helm_revisions_json" "$observed_images_json"
else
  state_load_and_verify
fi

lease_acquire
start_lease_renewal

if [[ "$operation" == "restore-replicas" ]]; then
  lease_verify_owned
  restore_replicas
  lease_release
  exit 0
fi

if [[ "$operation" == "resume" ]]; then
  validate_resume_reconciliation
  observed_migration_status="$(migration_job_status)"
  case "$observed_migration_status:$migration_status" in
    complete:*)
      [[ "$(migration_job_image)" == "$expected_migration_image" ]] || fail "completed migration Job image does not match the checkpointed immutable migration image"
      state_set_migration complete
      ;;
    failed:*|running:*|absent:complete|absent:running|absent:failed)
      fail "migration Job reconciliation is ambiguous or unsuccessful ($observed_migration_status); inspect job/$migration_job and do not repeat the migration automatically"
      ;;
    absent:not_started)
      ;;
    *)
      fail "migration Job reconciliation is ambiguous ($observed_migration_status); inspect job/$migration_job before resuming"
      ;;
  esac
fi

if [[ "$migration_status" != "complete" ]]; then
  echo "Phase A: scale all Helm-managed database consumers to zero before migration"
  lease_verify_owned
  state_set_phase quiesce attempted
  workloads_may_be_unavailable=true
  scale_deployment "$control_namespace" coyote-server 0
  scale_deployment "$control_namespace" coyote-frontend 0
  if [[ "$mcp_is_desired" == "true" && "$mcp_state" == "managed" ]]; then
    scale_deployment "$control_namespace" coyote-mcp 0
  fi
  scale_deployment "$worker_namespace" "$worker_deployment" 0
  run kubectl -n "$control_namespace" rollout status deployment/coyote-server --timeout="${timeout_seconds}s"
  run kubectl -n "$control_namespace" rollout status deployment/coyote-frontend --timeout="${timeout_seconds}s"
  if [[ "$mcp_is_desired" == "true" && "$mcp_state" == "managed" ]]; then
    run kubectl -n "$control_namespace" rollout status deployment/coyote-mcp --timeout="${timeout_seconds}s"
  fi
  run kubectl -n "$worker_namespace" rollout status "deployment/$worker_deployment" --timeout="${timeout_seconds}s"
  state_set_phase quiesce completed

  echo "Phase B: update migrator prerequisites"
  lease_verify_owned
  state_set_phase prerequisites attempted
  if [[ "$dry_run" == "true" ]]; then
    printf '+ kubectl -n %q apply -f - # rendered migrator ServiceAccount and SecretProviderClass\n' "$control_namespace"
  else
    printf '%s\n' "$migrator_prerequisite_manifest" | kubectl -n "$control_namespace" apply -f -
  fi
  state_set_phase prerequisites completed

  echo "Phase C: apply and wait for migration Job $migration_job"
  lease_verify_owned
  state_set_phase migration attempted
  state_set_migration applying
  if [[ "$dry_run" == "true" ]]; then
    printf '+ kubectl -n %q apply -f - # rendered migration Job %q\n' "$control_namespace" "$migration_job"
    printf '+ kubectl -n %q wait --for=condition=complete job/%q --timeout=%qs\n' "$control_namespace" "$migration_job" "$timeout_seconds"
  else
    printf '%s\n' "$migration_manifest" | kubectl -n "$control_namespace" apply -f -
    if ! run kubectl -n "$control_namespace" wait --for=condition=complete "job/$migration_job" --timeout="${timeout_seconds}s"; then
      state_set_migration failed
      kubectl -n "$control_namespace" describe "job/$migration_job" >&2 || true
      kubectl -n "$control_namespace" logs "job/$migration_job" --all-containers=true >&2 || true
      fail "migration Job $migration_job failed or timed out; it remains for inspection"
    fi
  fi
  state_set_migration complete
  state_set_phase migration completed
fi

echo "Phase D: restore the desired control-plane replicas"
lease_verify_owned
state_set_phase control-plane attempted
run helm upgrade "$control_release" "$control_chart" --namespace "$control_namespace" "${control_values_args[@]}" --wait --timeout "${timeout_seconds}s"
scale_deployment "$control_namespace" coyote-server "$(replica_from_state server)"
scale_deployment "$control_namespace" coyote-frontend "$(replica_from_state frontend)"
if [[ "$mcp_is_desired" == "true" ]]; then
  scale_deployment "$control_namespace" coyote-mcp "$(replica_from_state mcp)"
fi
run kubectl -n "$control_namespace" rollout status deployment/coyote-server --timeout="${timeout_seconds}s"
run kubectl -n "$control_namespace" rollout status deployment/coyote-frontend --timeout="${timeout_seconds}s"
if [[ "$mcp_is_desired" == "true" ]]; then
  run kubectl -n "$control_namespace" rollout status deployment/coyote-mcp --timeout="${timeout_seconds}s"
fi
[[ "$dry_run" == "true" ]] || state_update helm_revision control "$(helm_revision "$control_release" "$control_namespace")"
state_set_phase control-plane completed

echo "Phase E: restore the desired worker replicas"
lease_verify_owned
state_set_phase worker attempted
run helm upgrade "$worker_release" "$worker_chart" --namespace "$worker_namespace" "${worker_values_args[@]}" --wait --timeout "${timeout_seconds}s"
scale_deployment "$worker_namespace" "$worker_deployment" "$(replica_from_state worker)"
run kubectl -n "$worker_namespace" rollout status "deployment/$worker_deployment" --timeout="${timeout_seconds}s"
[[ "$dry_run" == "true" ]] || state_update helm_revision worker "$(helm_revision "$worker_release" "$worker_namespace")"
state_set_phase worker completed
workloads_may_be_unavailable=false

if [[ -n "$gateway_manifest" ]]; then
  require_command jq
  gateway_ready() { jq -e 'def condition_true($type): any(.status.conditions[]?; .type == $type and .status == "True"); condition_true("Accepted") and condition_true("Programmed")' >/dev/null; }
  http_route_ready() { jq -e 'def condition_true($type): any(.conditions[]?; .type == $type and .status == "True"); any(.status.parents[]?; condition_true("Accepted") and condition_true("ResolvedRefs"))' >/dev/null; }
  echo "Phase F: verify Gateway and HTTPRoute conditions"
  lease_verify_owned
  state_set_phase gateway attempted
  if [[ "$dry_run" == "true" ]]; then
    printf '+ kubectl -n %q get gateway coyote-temporary-public -o json # wait for Accepted and Programmed\n' "$control_namespace"
    printf '+ kubectl -n %q get httproute coyote-temporary-public coyote-temporary-http-redirect -o json # wait for Accepted and ResolvedRefs\n' "$control_namespace"
  else
    deadline=$(( $(date +%s) + timeout_seconds ))
    while (( $(date +%s) < deadline )); do
      ensure_lease_healthy
      gateway="$(kubectl -n "$control_namespace" get gateway coyote-temporary-public -o json)"
      route="$(kubectl -n "$control_namespace" get httproute coyote-temporary-public -o json)"
      redirect_route="$(kubectl -n "$control_namespace" get httproute coyote-temporary-http-redirect -o json)"
      if gateway_ready <<<"$gateway" && http_route_ready <<<"$route" && http_route_ready <<<"$redirect_route"; then break; fi
      sleep 5
    done
    ensure_lease_healthy
    if ! gateway_ready <<<"$gateway" || ! http_route_ready <<<"$route" || ! http_route_ready <<<"$redirect_route"; then
      kubectl -n "$control_namespace" describe gateway coyote-temporary-public >&2 || true
      kubectl -n "$control_namespace" describe httproute coyote-temporary-public coyote-temporary-http-redirect >&2 || true
      fail "Gateway or HTTPRoute did not become ready within ${timeout_seconds}s"
    fi
  fi
  state_set_phase gateway completed
fi

[[ "$dry_run" == "true" ]] || state_update clear_failure
lease_release
echo "Helm rollout completed; run the existing smoke tests as the final validation layer."
