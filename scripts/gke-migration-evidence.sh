#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
profile=""
checkpoint=""
ledger_file=""
migrations_dir="$repo_root/backend/db/migrations"
namespace="coyote-ci"
output=""

usage() {
  cat <<'EOF'
Usage: scripts/gke-migration-evidence.sh --profile PATH [options]

Read-only historical migration evidence report. This command never creates,
updates, deletes, annotates, scales, or executes Kubernetes or database resources.

Options:
  --profile PATH             Target profile with Kubernetes identity fields.
  --checkpoint PATH          Optional local rollout state to compare as evidence.
  --goose-ledger-file PATH   Optional JSON export of goose_db_version rows.
  --migrations-dir PATH      Migration directory (default: backend/db/migrations).
  --namespace NAME           Migration Job namespace (default: coyote-ci).
  --output PATH              Write the JSON report locally instead of stdout.

To obtain a ledger export with separately authorized read-only database access:
  psql "$DATABASE_URL" -X -qAt -v ON_ERROR_STOP=1 \
    -c 'BEGIN READ ONLY' \
    -c "SELECT COALESCE(json_agg(json_build_object(
      'id', id, 'version_id', version_id, 'is_applied', is_applied,
      'tstamp', tstamp
    ) ORDER BY id), '[]'::json) FROM goose_db_version" \
    -c 'ROLLBACK'

Save only the JSON result to a file and pass it with --goose-ledger-file.
EOF
}

fail() {
  printf '%s\n' "$*" >&2
  exit 2
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --profile) profile="${2:-}"; shift 2 ;;
    --checkpoint) checkpoint="${2:-}"; shift 2 ;;
    --goose-ledger-file) ledger_file="${2:-}"; shift 2 ;;
    --migrations-dir) migrations_dir="${2:-}"; shift 2 ;;
    --namespace) namespace="${2:-}"; shift 2 ;;
    --output) output="${2:-}"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) fail "unknown argument: $1" ;;
  esac
done

[[ -n "$profile" && -f "$profile" ]] || fail "--profile must name an existing file"
[[ -z "$checkpoint" || -f "$checkpoint" ]] || fail "checkpoint does not exist: $checkpoint"
[[ -z "$ledger_file" || -f "$ledger_file" ]] || fail "Goose ledger export does not exist: $ledger_file"
[[ -d "$migrations_dir" ]] || fail "migrations directory does not exist: $migrations_dir"
[[ "$namespace" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || fail "invalid namespace: $namespace"

require_command kubectl
require_command helm
require_command ruby

temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/coyote-migration-evidence.XXXXXX")"
trap 'rm -rf "$temporary_dir"' EXIT

profile_values="$(ruby -rjson -e '
  profile = JSON.parse(File.read(ARGV.fetch(0)))
  kubernetes = profile.fetch("kubernetes")
  values = {
    "target" => profile["target"],
    "context" => kubernetes["context"],
    "cluster_uid" => kubernetes["cluster_uid"],
    "control_release" => kubernetes["control_plane_release"]
  }
  values.each do |key, value|
    abort "profile #{key} is required" unless value.is_a?(String) && !value.strip.empty?
    puts "#{key}=#{value}"
  end
' "$profile")" || fail "profile Kubernetes identity is malformed"
while IFS= read -r item; do
  case "$item" in
    target=*) target="${item#*=}" ;;
    context=*) expected_context="${item#*=}" ;;
    cluster_uid=*) expected_cluster_uid="${item#*=}" ;;
    control_release=*) control_release="${item#*=}" ;;
  esac
done <<<"$profile_values"

actual_context="$(kubectl config current-context)"
[[ "$actual_context" == "$expected_context" ]] || fail "active Kubernetes context $actual_context does not match target context $expected_context"
actual_cluster_uid="$(kubectl --context "$expected_context" get namespace kube-system -o jsonpath='{.metadata.uid}')"
[[ "$actual_cluster_uid" == "$expected_cluster_uid" ]] || fail "active Kubernetes cluster identity does not match target cluster UID"

kubectl --context "$expected_context" -n "$namespace" get jobs -l app.kubernetes.io/name=coyote-migrate -o json >"$temporary_dir/jobs.json"
helm --kube-context "$expected_context" -n "$namespace" status "$control_release" --output json >"$temporary_dir/helm-status.json"

report_args=("$profile" "$temporary_dir/jobs.json" "$actual_context" "$actual_cluster_uid" "$namespace" "$temporary_dir/helm-status.json" "$migrations_dir")
[[ -n "$checkpoint" ]] && report_args+=("$checkpoint") || report_args+=("")
[[ -n "$ledger_file" ]] && report_args+=("$ledger_file") || report_args+=("")

report="$(ruby -rjson -rdigest -e '
  def canonical(value)
    case value
    when Hash
      value.keys.sort.each_with_object({}) { |key, result| result[key] = canonical(value[key]) }
    when Array
      value.map { |item| canonical(item) }
    else
      value
    end
  end

  def parse_json(path, description)
    JSON.parse(File.read(path))
  rescue JSON::ParserError => error
    abort "#{description} is malformed: #{error.message}"
  end

  profile_path, jobs_path, actual_context, actual_cluster_uid, namespace, helm_status_path, migrations_dir, checkpoint_path, ledger_path = ARGV
  profile = parse_json(profile_path, "target profile")
  kubernetes = profile.fetch("kubernetes")
  target = profile.fetch("target")
  expected_context = kubernetes.fetch("context")
  expected_cluster_uid = kubernetes.fetch("cluster_uid")
  control_release = kubernetes.fetch("control_plane_release")
  worker_release = kubernetes["worker_release"]
  jobs_document = parse_json(jobs_path, "migration Job response")
  helm_status = parse_json(helm_status_path, "control-plane Helm status")
  errors = []
  items = jobs_document["items"]
  unless items.is_a?(Array)
    errors << "migration Job response items must be an array"
    items = []
  end

  name_counts = Hash.new(0)
  uid_counts = Hash.new(0)
  items.each do |job|
    metadata = job.is_a?(Hash) ? job["metadata"] : nil
    name_counts[metadata["name"]] += 1 if metadata.is_a?(Hash) && metadata["name"].is_a?(String)
    uid_counts[metadata["uid"]] += 1 if metadata.is_a?(Hash) && metadata["uid"].is_a?(String)
  end

  jobs = items.map do |job|
    unless job.is_a?(Hash)
      {"status" => "malformed", "errors" => ["Job is not an object"]}
      next
    end
    metadata = job["metadata"].is_a?(Hash) ? job["metadata"] : {}
    status = job["status"].is_a?(Hash) ? job["status"] : {}
    containers = job.dig("spec", "template", "spec", "containers")
    containers = [] unless containers.is_a?(Array)
    migrate_container = containers.find { |container| container.is_a?(Hash) && container["name"] == "migrate" }
    image = migrate_container.is_a?(Hash) ? migrate_container["image"] : nil
    conditions = status["conditions"].is_a?(Array) ? status["conditions"] : []
    complete = conditions.any? { |condition| condition.is_a?(Hash) && condition["type"] == "Complete" && condition["status"] == "True" }
    failed = conditions.any? { |condition| condition.is_a?(Hash) && condition["type"] == "Failed" && condition["status"] == "True" }
    labels = metadata["labels"].is_a?(Hash) ? metadata["labels"] : {}
    annotations = metadata["annotations"].is_a?(Hash) ? metadata["annotations"] : {}
    lineage_labels = labels.select { |key, _value| key.start_with?("coyote.dev/") }
    lineage_annotations = annotations.select { |key, _value| key.start_with?("coyote.dev/") }
    evidence = {
      "name" => metadata["name"],
      "uid" => metadata["uid"],
      "namespace" => metadata["namespace"] || namespace,
      "completion" => {
        "complete" => complete,
        "failed" => failed,
        "timestamp" => status["completionTime"]
      },
      "migration_image" => image,
      "labels" => labels,
      "annotations" => annotations,
      "lineage_labels" => lineage_labels,
      "lineage_annotations" => lineage_annotations
    }
    job_errors = []
    ambiguity_errors = []
    job_errors << "missing Job name" unless evidence["name"].is_a?(String) && !evidence["name"].empty?
    job_errors << "missing Job UID" unless evidence["uid"].is_a?(String) && !evidence["uid"].empty?
    job_errors << "migration image must be an immutable SHA-256 reference" unless image.is_a?(String) && image.match?(/\A.+@sha256:[0-9a-f]{64}\z/)
    job_errors << "completed Job is missing completion timestamp" if complete && !(status["completionTime"].is_a?(String) && !status["completionTime"].empty?)
    ambiguity_errors << "duplicate Job name in response" if evidence["name"].is_a?(String) && name_counts[evidence["name"]] > 1
    ambiguity_errors << "duplicate Job UID in response" if evidence["uid"].is_a?(String) && uid_counts[evidence["uid"]] > 1
    lineage_status = if lineage_labels.empty? && lineage_annotations.empty?
      "missing"
    else
      "metadata-present-unverified"
    end
    classification = if !job_errors.empty?
      "malformed"
    elsif complete && failed || !ambiguity_errors.empty?
      "ambiguous"
    elsif !complete || failed
      "incomplete"
    elsif lineage_status == "missing"
      "complete-missing-lineage"
    else
      "complete-unverified-lineage"
    end
    {
      "evidence" => evidence.merge("lineage_status" => lineage_status),
      "fingerprint" => "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical(evidence)))}",
      "status" => classification,
      "errors" => job_errors + ambiguity_errors
    }
  end

  known_versions = Dir.children(migrations_dir).map do |name|
    match = name.match(/\A0*([0-9]+)_.*\.sql\z/)
    match && Integer(match[1])
  end.compact.sort
  ledger = if ledger_path.empty?
    {"status" => "unavailable", "reason" => "no read-only Goose ledger export was supplied"}
  else
    rows = parse_json(ledger_path, "Goose ledger export")
    unless rows.is_a?(Array)
      {"status" => "malformed", "errors" => ["Goose ledger export must be an array"]}
    else
      invalid = rows.reject { |row| row.is_a?(Hash) && row["id"].is_a?(Integer) && row["id"] > 0 && row["version_id"].is_a?(Integer) && [true, false].include?(row["is_applied"]) && row["tstamp"].is_a?(String) }
      duplicate_ids = rows.group_by { |row| row.is_a?(Hash) ? row["id"] : nil }.select { |id, entries| !id.nil? && entries.length > 1 }.keys
      if invalid.empty? && duplicate_ids.empty?
        latest_by_version = rows.sort_by { |row| row["id"] }.each_with_object({}) { |row, result| result[row["version_id"]] = row }
        migration_states = latest_by_version.keys.sort.map do |version|
          row = latest_by_version.fetch(version)
          {"version_id" => version, "is_applied" => row["is_applied"], "sequence_id" => row["id"], "tstamp" => row["tstamp"]}
        end
        applied = migration_states.select { |state| state["is_applied"] }.map { |state| state["version_id"] }
        {"status" => "available", "rows" => rows, "migration_states" => migration_states, "applied_versions" => applied, "known_migration_versions" => known_versions, "applied_versions_missing_from_source" => applied - known_versions, "source_versions_not_applied" => known_versions - applied}
      else
        errors = []
        errors << "Goose ledger rows require positive integer id, integer version_id, boolean is_applied, and timestamp string" unless invalid.empty?
        errors << "Goose ledger contains duplicate sequence IDs: #{duplicate_ids.sort.join(", ")}" unless duplicate_ids.empty?
        {"status" => "malformed", "errors" => errors}
      end
    end
  end

  checkpoint = if checkpoint_path.empty?
    {"status" => "not-supplied"}
  else
    state = parse_json(checkpoint_path, "rollout checkpoint")
    expected = {"target_environment" => target, "context" => expected_context, "cluster_uid" => expected_cluster_uid, "control_namespace" => namespace, "control_release" => control_release}
    mismatches = expected.map { |key, value| {"field" => key, "expected" => value, "observed" => state[key]} unless state[key] == value }.compact
    {"status" => mismatches.empty? ? "matches-target-identity-only" : "identity-mismatch", "observed" => state.slice(*expected.keys, "rollout_id", "migration_job", "migration_status"), "mismatches" => mismatches}
  end

  identity_mismatches = []
  identity_mismatches << {"field" => "context", "expected" => expected_context, "observed" => actual_context} unless actual_context == expected_context
  identity_mismatches << {"field" => "cluster_uid", "expected" => expected_cluster_uid, "observed" => actual_cluster_uid} unless actual_cluster_uid == expected_cluster_uid
  report = {
    "schema_version" => 1,
    "read_only" => true,
    "target" => target,
    "environment_identity" => {"status" => identity_mismatches.empty? ? "verified" : "mismatch", "expected_context" => expected_context, "observed_context" => actual_context, "expected_cluster_uid" => expected_cluster_uid, "observed_cluster_uid" => actual_cluster_uid, "mismatches" => identity_mismatches},
    "topology" => {"scope" => worker_release.nil? || worker_release.empty? ? "control-plane-only" : "control-plane-and-worker", "namespace" => namespace, "control_plane_release" => control_release, "worker_release" => worker_release, "control_plane_helm_status" => helm_status},
    "checkpoint_identity" => checkpoint,
    "migration_jobs" => jobs,
    "migration_job_response_errors" => errors,
    "goose_ledger" => ledger,
    "notes" => ["Job completion and Goose ledger state are distinct evidence.", "Timestamp proximity is not release-lineage evidence.", "This report does not authorize archival acknowledgement, migration execution, or rollout resumption."]
  }
  puts JSON.pretty_generate(report)
' "${report_args[@]}")" || fail "could not build migration evidence report"

if [[ -n "$output" ]]; then
  printf '%s\n' "$report" >"$output"
else
  printf '%s\n' "$report"
fi