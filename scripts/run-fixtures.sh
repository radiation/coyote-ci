#!/usr/bin/env bash
set -euo pipefail

API_URL="${API_URL:-http://localhost:8080}"
PROJECT_ID="${PROJECT_ID:-fixtures}"
FIXTURE_REPO_URL="${FIXTURE_REPO_URL:-https://github.com/radiation/coyote-ci-fixtures.git}"
FIXTURE_REF="${FIXTURE_REF:-main}"

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

Optional environment variables:
  API_URL            Default: http://localhost:8080
  PROJECT_ID         Default: fixtures
  FIXTURE_REPO_URL   Default: https://github.com/radiation/coyote-ci-fixtures.git
  FIXTURE_REF        Default: main
EOF
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

  if ! scenario_exists "$target"; then
    echo "Unknown scenario: ${target}" >&2
    usage
    exit 1
  fi

  queue_one "$target"
}

main "$@"
