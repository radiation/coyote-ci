#!/usr/bin/env bash
set -euo pipefail

API_URL="${API_URL:-http://localhost:8080}"
PROJECT_ID="${PROJECT_ID:-fixtures}"
FIXTURE_REPO_URL="${FIXTURE_REPO_URL:-https://github.com/radiation/coyote-ci-fixtures.git}"
FIXTURE_REF="${FIXTURE_REF:-main}"
TRIGGER_WAIT_TIMEOUT_SECONDS="${TRIGGER_WAIT_TIMEOUT_SECONDS:-120}"
POLL_INTERVAL_SECONDS="${POLL_INTERVAL_SECONDS:-2}"

SCENARIOS=(
  "success-basic"
  "failure-exit-1"
  "logs-long-running"
  "artifacts-basic"
  "multi-step-failure"
)

REAL_NETWORK_SCENARIOS=(
  "docker-image-pull-smoke"
  "maven-dependency-smoke"
  "npm-install-cache-smoke"
  "python-pip-install-smoke"
)

MANUAL_FAILURE_SCENARIOS=(
  "missing-tool-failure-smoke"
)

usage() {
  cat <<'EOF'
Queue Coyote CI fixture scenarios against one repository with different pipeline_path values.

Usage:
  scripts/run-fixtures.sh all
  scripts/run-fixtures.sh real-network
  scripts/run-fixtures.sh manual-failure
  scripts/run-fixtures.sh artifact-download-chain
  scripts/run-fixtures.sh <scenario>

Scenarios:
  success-basic
  failure-exit-1
  logs-long-running
  artifacts-basic
  multi-step-failure
  docker-image-pull-smoke
  maven-dependency-smoke
  npm-install-cache-smoke
  python-pip-install-smoke
  missing-tool-failure-smoke
  artifact-download-chain

Optional environment variables:
  API_URL            Default: http://localhost:8080
  PROJECT_ID         Default: fixtures
  FIXTURE_REPO_URL   Default: https://github.com/radiation/coyote-ci-fixtures.git
  FIXTURE_REF        Default: main
  TRIGGER_WAIT_TIMEOUT_SECONDS  Default: 120
  POLL_INTERVAL_SECONDS         Default: 2
EOF
}

require_jq() {
  if ! command -v jq >/dev/null 2>&1; then
    echo "jq is required for this command" >&2
    exit 1
  fi
}

api_get() {
  local path="$1"
  curl -sS "${API_URL}${path}"
}

queue_pipeline_build() {
  local pipeline_yaml="$1"

  local response
  response=$(jq -n \
    --arg project_id "${PROJECT_ID}" \
    --arg pipeline_yaml "${pipeline_yaml}" \
    --arg repo_url "${FIXTURE_REPO_URL}" \
    --arg ref "${FIXTURE_REF}" \
    '{project_id: $project_id, pipeline_yaml: $pipeline_yaml, source: {repository_url: $repo_url, ref: $ref}}' \
    | curl -sS -X POST "${API_URL}/builds/pipeline" \
        -H "Content-Type: application/json" \
        -d @-)

  local build_id status pipeline_name
  build_id=$(printf '%s' "$response" | jq -r '.data.id // empty')
  status=$(printf '%s' "$response" | jq -r '.data.status // empty')
  pipeline_name=$(printf '%s' "$response" | jq -r '.data.pipeline_name // empty')

  if [[ -z "$build_id" ]]; then
    echo "$response" | jq . >&2
    return 1
  fi

  echo "build_id=${build_id} status=${status} pipeline=${pipeline_name}" >&2
  printf '%s\n' "$build_id"
}

get_build_status() {
  local build_id="$1"
  api_get "/api/builds/${build_id}" | jq -r '.data.status // empty'
}

wait_for_build_terminal() {
  local build_id="$1"
  local status=""

  while true; do
    status=$(get_build_status "$build_id")
    if [[ "$status" == "success" || "$status" == "failed" || "$status" == "canceled" ]]; then
      printf '%s\n' "$status"
      return 0
    fi
    sleep "$POLL_INTERVAL_SECONDS"
  done
}

resolve_job_id() {
  local job_name="$1"
  api_get "/api/jobs/resolve?project=${PROJECT_ID}&name=${job_name}" | jq -r '.data.id // empty'
}

lookup_triggered_build_id() {
  local job_id="$1"
  local producer_build_id="$2"
  api_get "/api/jobs/${job_id}/builds" | jq -r --arg producer_build_id "$producer_build_id" '.data.builds[] | select(.trigger_producer_build_id == $producer_build_id) | .id' | head -n 1
}

wait_for_triggered_build() {
  local job_id="$1"
  local producer_build_id="$2"
  local build_id=""
  local start_time elapsed_seconds

  start_time=$(date +%s)

  while true; do
    build_id=$(lookup_triggered_build_id "$job_id" "$producer_build_id")
    if [[ -n "$build_id" ]]; then
      printf '%s\n' "$build_id"
      return 0
    fi

	elapsed_seconds=$(( $(date +%s) - start_time ))
	if (( elapsed_seconds >= TRIGGER_WAIT_TIMEOUT_SECONDS )); then
	  echo "Timed out waiting ${TRIGGER_WAIT_TIMEOUT_SECONDS}s for triggered build for consumer_job_id=${job_id} producer_build_id=${producer_build_id}" >&2
	  return 1
	fi

    sleep "$POLL_INTERVAL_SECONDS"
  done
}

queue_one_build_id() {
  local scenario="$1"
  local pipeline_path="scenarios/${scenario}/coyote.yml"

  echo "=== Queueing scenario: ${scenario} ===" >&2

  local payload
  payload=$(cat <<EOF
{
  "project_id": "${PROJECT_ID}",
  "repo_url": "${FIXTURE_REPO_URL}",
  "ref": "${FIXTURE_REF}",
  "pipeline_path": "${pipeline_path}"
}
EOF
)

  local response
  response=$(curl -sS -X POST "${API_URL}/builds/repo" \
    -H "Content-Type: application/json" \
    -d "${payload}")

  local build_id status source path
  build_id=$(printf '%s' "$response" | jq -r '.data.id // empty')
  status=$(printf '%s' "$response" | jq -r '.data.status // empty')
  source=$(printf '%s' "$response" | jq -r '.data.pipeline_source // empty')
  path=$(printf '%s' "$response" | jq -r '.data.pipeline_path // empty')

  if [[ -n "$build_id" ]]; then
    echo "build_id=${build_id} status=${status} pipeline_source=${source} pipeline_path=${path}" >&2
    printf '%s\n' "$build_id"
    return 0
  fi

  echo "$response" | jq . >&2
  echo "Failed to queue scenario: ${scenario}" >&2
  return 1
}

queue_artifact_download_chain() {
  require_jq

  local producer_scenario="npm-install-cache-smoke"
  local consumer_job_name="npm-artifact-download-consumer"
  local consumer_job_id
  consumer_job_id=$(resolve_job_id "$consumer_job_name")
  if [[ -z "$consumer_job_id" ]]; then
    echo "Failed to resolve consumer job ${consumer_job_name}. Bootstrap fixture jobs first." >&2
    return 1
  fi

  echo "=== Queueing producer scenario: ${producer_scenario} ==="
  local producer_build_id
  producer_build_id=$(queue_one_build_id "$producer_scenario")

  echo "=== Waiting for producer build: ${producer_build_id} ==="
  local producer_status
  producer_status=$(wait_for_build_terminal "$producer_build_id")
  echo "producer_status=${producer_status}"
  if [[ "$producer_status" != "success" ]]; then
    echo "Producer build did not succeed: ${producer_build_id}" >&2
    return 1
  fi

  echo "=== Waiting for triggered consumer build: ${consumer_job_name} ==="
  local consumer_build_id
  consumer_build_id=$(wait_for_triggered_build "$consumer_job_id" "$producer_build_id")

  echo "=== Waiting for consumer build: ${consumer_build_id} ==="
  local consumer_status
  consumer_status=$(wait_for_build_terminal "$consumer_build_id")

  echo "producer_build_id=${producer_build_id} consumer_job_id=${consumer_job_id} consumer_build_id=${consumer_build_id} consumer_status=${consumer_status}"
}

scenario_exists() {
  local wanted="$1"
  local s
  for s in "${SCENARIOS[@]}"; do
    if [[ "$s" == "$wanted" ]]; then
      return 0
    fi
  done
  for s in "${REAL_NETWORK_SCENARIOS[@]}"; do
    if [[ "$s" == "$wanted" ]]; then
      return 0
    fi
  done
  for s in "${MANUAL_FAILURE_SCENARIOS[@]}"; do
    if [[ "$s" == "$wanted" ]]; then
      return 0
    fi
  done
  return 1
}

queue_one() {
  local scenario="$1"
  local pipeline_path="scenarios/${scenario}/coyote.yml"

  echo "=== Queueing scenario: ${scenario} ==="

  local payload
  payload=$(cat <<EOF
{
  "project_id": "${PROJECT_ID}",
  "repo_url": "${FIXTURE_REPO_URL}",
  "ref": "${FIXTURE_REF}",
  "pipeline_path": "${pipeline_path}"
}
EOF
)

  local response
  response=$(curl -sS -X POST "${API_URL}/builds/repo" \
    -H "Content-Type: application/json" \
    -d "${payload}")

  if command -v jq >/dev/null 2>&1; then
    local build_id status source path
    build_id=$(printf '%s' "$response" | jq -r '.data.id // empty')
    status=$(printf '%s' "$response" | jq -r '.data.status // empty')
    source=$(printf '%s' "$response" | jq -r '.data.pipeline_source // empty')
    path=$(printf '%s' "$response" | jq -r '.data.pipeline_path // empty')

    if [[ -n "$build_id" ]]; then
      echo "build_id=${build_id} status=${status} pipeline_source=${source} pipeline_path=${path}"
    else
      echo "$response" | jq .
      echo "Failed to queue scenario: ${scenario}" >&2
      return 1
    fi
  else
    # Fallback when jq is unavailable.
    echo "$response"
  fi
}

main() {
  if [[ $# -ne 1 ]]; then
    usage
    exit 1
  fi

  local target="$1"

  if [[ "$target" == "all" ]]; then
    local s
    for s in "${SCENARIOS[@]}"; do
      queue_one "$s"
    done
    return 0
  fi

  if [[ "$target" == "real-network" ]]; then
    local s
    for s in "${REAL_NETWORK_SCENARIOS[@]}"; do
      queue_one "$s"
    done
    return 0
  fi

  if [[ "$target" == "manual-failure" ]]; then
    local s
    for s in "${MANUAL_FAILURE_SCENARIOS[@]}"; do
      queue_one "$s"
    done
    return 0
  fi

  if [[ "$target" == "artifact-download-chain" ]]; then
    queue_artifact_download_chain
    return 0
  fi

  if ! scenario_exists "$target"; then
    echo "Unknown scenario: ${target}" >&2
    usage
    exit 1
  fi

  queue_one "$target"
}

main "$@"
