#!/usr/bin/env bash
set -euo pipefail

cluster_name="coyote-ci"
namespace="coyote-ci"
context="kind-$cluster_name"
api_url="${API_URL:-http://localhost:8080}"
timeout_seconds="${KIND_RELIABILITY_SMOKE_TIMEOUT_SECONDS:-180}"
lease_seconds=""
build_id=""

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

print_diagnostics() {
  local jobs=""
  set +e
  echo "kind reliability smoke diagnostics"
  kubectl --context "$context" -n "$namespace" get jobs -o wide
  kubectl --context "$context" -n "$namespace" get pods -o wide
  if [[ -n "$build_id" ]]; then
    jobs=$(kubectl --context "$context" -n "$namespace" get jobs -l "coyote-ci.io/build-id=$build_id" -o name 2>/dev/null)
    if [[ -n "$jobs" ]]; then kubectl --context "$context" -n "$namespace" describe $jobs; fi
    curl -sS "$api_url/api/builds/$build_id" | jq .
    curl -sS "$api_url/api/builds/$build_id/steps" | jq .
    curl -sS "$api_url/api/builds/$build_id/steps/0/logs" | jq .
  fi
  kubectl --context "$context" -n "$namespace" logs deployment/coyote-kubernetes-worker --tail=200
}
trap print_diagnostics ERR

wait_until() {
  local description="$1"
  shift
  local deadline=$(( $(date +%s) + timeout_seconds ))
  while (( $(date +%s) < deadline )); do
    if "$@"; then return 0; fi
    sleep 2
  done
  echo "timed out waiting for $description" >&2
  return 1
}

wait_for_api() { curl -fsS "$api_url/api/readyz" >/dev/null; }
wait_for_running_pod() {
  local job_name="$1"
  [[ "$(kubectl --context "$context" -n "$namespace" get pods -l "job-name=$job_name" -o json | jq -r '.items[0].status.phase // empty')" == "Running" ]]
}
wait_for_job() {
  local candidate
  candidate=$(kubectl --context "$context" -n "$namespace" get jobs -l "coyote-ci.io/build-id=$build_id" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
  [[ -n "$candidate" ]]
}
wait_for_terminal_state() {
  local expected="$1" build_response steps_response
  build_response=$(curl -sS "$api_url/api/builds/$build_id")
  steps_response=$(curl -sS "$api_url/api/builds/$build_id/steps")
  [[ "$(jq -r '.data.status // empty' <<<"$build_response")" == "$expected" ]] && [[ "$(jq -r '.data.steps[0].status // empty' <<<"$steps_response")" == "$expected" ]]
}
create_project() {
  local slug="$1" result status body
  result=$(curl -sS -w '\n%{http_code}' -X POST "$api_url/api/projects" -H 'Content-Type: application/json' --data "{\"name\":\"$slug\",\"slug\":\"$slug\"}")
  status="${result##*$'\n'}"
  body="${result%$'\n'*}"
  if [[ "$status" != "201" && "$status" != "409" ]]; then printf '%s\n' "$body" >&2; return 1; fi
}
submit_build() {
  local project_id="$1" pipeline_yaml="$2" response
  response=$(jq -n --arg project_id "$project_id" --arg pipeline_yaml "$pipeline_yaml" '{project_id: $project_id, pipeline_yaml: $pipeline_yaml}' | curl -sS -X POST "$api_url/api/builds/pipeline" -H 'Content-Type: application/json' --data @-)
  build_id=$(jq -r '.data.id // empty' <<<"$response")
  [[ -n "$build_id" ]] || { echo "$response" | jq . >&2; return 1; }
}

require_command curl
require_command jq
require_command kubectl
require_command kind
if ! kind get clusters | grep -Fxq "$cluster_name"; then echo "kind cluster $cluster_name does not exist; run make kind-up first" >&2; exit 1; fi
kubectl --context "$context" -n "$namespace" rollout status deployment/coyote-kubernetes-worker --timeout=180s
lease_seconds=$(kubectl --context "$context" -n "$namespace" get deployment coyote-kubernetes-worker -o json | jq -r '.spec.template.spec.containers[] | select(.name == "worker") | .env[] | select(.name == "WORKER_STEP_LEASE_SECONDS") | .value // empty')
if ! [[ "$lease_seconds" =~ ^[1-9][0-9]*$ ]]; then
  echo "could not resolve WORKER_STEP_LEASE_SECONDS from the deployed worker" >&2
  exit 1
fi
wait_until "Coyote API readiness" wait_for_api

create_project "kind-cancel-smoke"
cancel_pipeline=$(cat <<'YAML'
version: 1
pipeline:
  name: kind-cancel-smoke
  image: alpine:3.21
steps:
  - name: long-running
    run: |
      echo CANCEL_SMOKE_STARTED
      sleep 300
YAML
)
submit_build "kind-cancel-smoke" "$cancel_pipeline"
cancel_build_id="$build_id"
wait_until "cancellation Job creation" wait_for_job
cancel_job=$(kubectl --context "$context" -n "$namespace" get jobs -l "coyote-ci.io/build-id=$build_id" -o jsonpath='{.items[0].metadata.name}')
[[ "$cancel_job" == coyote-exec-* ]] || { echo "unexpected cancellation Job: $cancel_job" >&2; exit 1; }
wait_until "cancellation Pod running" wait_for_running_pod "$cancel_job"
cancel_pod=$(kubectl --context "$context" -n "$namespace" get pods -l "job-name=$cancel_job" -o jsonpath='{.items[0].metadata.name}')
curl -fsS -X POST "$api_url/api/builds/$build_id/cancel" >/dev/null
wait_until "durable cancellation" wait_for_terminal_state "canceled"
wait_until "cancellation Job deletion" bash -c "! kubectl --context '$context' -n '$namespace' get job '$cancel_job' >/dev/null 2>&1"
wait_until "cancellation Pod deletion" bash -c "! kubectl --context '$context' -n '$namespace' get pod '$cancel_pod' >/dev/null 2>&1"
sleep 15
wait_for_terminal_state "canceled" || { echo "canceled state was overwritten after reconciliation" >&2; exit 1; }
echo "cancellation passed: build=$cancel_build_id job=$cancel_job pod=$cancel_pod"

create_project "kind-reclaim-smoke"
reclaim_pipeline=$(cat <<'YAML'
version: 1
pipeline:
  name: kind-reclaim-smoke
  image: alpine:3.21
steps:
  - name: reclaim
    run: |
      echo RECLAIM_SMOKE_STARTED
      sleep 60
      echo RECLAIM_SMOKE_COMPLETED
YAML
)
submit_build "kind-reclaim-smoke" "$reclaim_pipeline"
reclaim_build_id="$build_id"
wait_until "reclaim Job creation" wait_for_job
reclaim_job=$(kubectl --context "$context" -n "$namespace" get jobs -l "coyote-ci.io/build-id=$build_id" -o jsonpath='{.items[0].metadata.name}')
reclaim_execution_id=$(kubectl --context "$context" -n "$namespace" get job "$reclaim_job" -o jsonpath='{.metadata.labels.coyote-ci\.io/execution-job-id}')
[[ "$reclaim_job" == "coyote-exec-$reclaim_execution_id" ]] || { echo "Job identity does not match execution: job=$reclaim_job execution=$reclaim_execution_id" >&2; exit 1; }
wait_until "reclaim Pod running" wait_for_running_pod "$reclaim_job"
reclaim_pod=$(kubectl --context "$context" -n "$namespace" get pods -l "job-name=$reclaim_job" -o jsonpath='{.items[0].metadata.name}')
kubectl --context "$context" -n "$namespace" scale deployment/coyote-kubernetes-worker --replicas=0
kubectl --context "$context" -n "$namespace" wait --for=delete pod -l app.kubernetes.io/name=coyote-kubernetes-worker --timeout=90s
sleep $(( lease_seconds + 2 ))
[[ "$(kubectl --context "$context" -n "$namespace" get jobs -l "coyote-ci.io/build-id=$build_id" -o json | jq '.items | length')" == "1" ]] || { echo "controller loss created or removed a reclaim Job" >&2; exit 1; }
[[ "$(kubectl --context "$context" -n "$namespace" get pods -l "job-name=$reclaim_job" -o jsonpath='{.items[0].metadata.name}')" == "$reclaim_pod" ]] || { echo "build Pod did not survive controller loss" >&2; exit 1; }
kubectl --context "$context" -n "$namespace" scale deployment/coyote-kubernetes-worker --replicas=1
kubectl --context "$context" -n "$namespace" rollout status deployment/coyote-kubernetes-worker --timeout=180s
wait_until "reclaimed build success" wait_for_terminal_state "success"
[[ "$(kubectl --context "$context" -n "$namespace" get jobs -l "coyote-ci.io/build-id=$build_id" -o json | jq '.items | length')" == "1" ]] || { echo "reclaim created a duplicate Job" >&2; exit 1; }
[[ "$(kubectl --context "$context" -n "$namespace" get jobs -l "coyote-ci.io/build-id=$build_id" -o jsonpath='{.items[0].metadata.name}')" == "$reclaim_job" ]] || { echo "reclaim did not adopt deterministic Job $reclaim_job" >&2; exit 1; }
reclaim_logs=$(kubectl --context "$context" -n "$namespace" logs "$reclaim_pod" -c build)
[[ "$(grep -Fc RECLAIM_SMOKE_STARTED <<<"$reclaim_logs")" == "1" ]] || { echo "reclaim build command did not run exactly once" >&2; exit 1; }
[[ "$(grep -Fc RECLAIM_SMOKE_COMPLETED <<<"$reclaim_logs")" == "1" ]] || { echo "reclaim build did not complete" >&2; exit 1; }
persisted_logs=$(curl -sS "$api_url/api/builds/$build_id/steps/0/logs")
jq -e '[.data.chunks[].chunk_text] | join("") | contains("RECLAIM_SMOKE_COMPLETED")' <<<"$persisted_logs" >/dev/null
echo "reclaim passed: build=$reclaim_build_id execution=$reclaim_execution_id job=$reclaim_job pod=$reclaim_pod lease=${lease_seconds}s"
trap - ERR
echo "kind reliability smoke passed"