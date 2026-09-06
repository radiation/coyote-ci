#!/usr/bin/env bash
set -euo pipefail

cluster_name="coyote-ci"
namespace="coyote-ci"
api_url="${API_URL:-http://localhost:8080}"
marker="CROSS_WORKER_RESTORE_OK"
timeout_seconds="${KIND_SMOKE_TIMEOUT_SECONDS:-240}"
context="kind-$cluster_name"
build_id=""

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

print_diagnostics() {
  local jobs=""
  set +e
  echo "kind workspace smoke diagnostics"
  kubectl --context "$context" -n "$namespace" get jobs -o wide
  kubectl --context "$context" -n "$namespace" get pods -o wide
  if [[ -n "$build_id" ]]; then
    jobs=$(kubectl --context "$context" -n "$namespace" get jobs -l "coyote-ci.io/build-id=$build_id" -o name 2>/dev/null)
    if [[ -n "$jobs" ]]; then
      kubectl --context "$context" -n "$namespace" describe $jobs
    fi
    curl -sS "$api_url/api/builds/$build_id" | jq .
    curl -sS "$api_url/api/builds/$build_id/steps" | jq .
  fi
  kubectl --context "$context" -n "$namespace" logs deployment/coyote-kubernetes-worker --tail=200
}
trap print_diagnostics ERR

require_command curl
require_command jq
require_command kubectl
require_command kind

if ! kind get clusters | grep -Fxq "$cluster_name"; then
  echo "kind cluster $cluster_name does not exist; run make kind-up first" >&2
  exit 1
fi

kubectl --context "$context" -n "$namespace" rollout status deployment/coyote-kubernetes-worker --timeout=180s

deadline=$(( $(date +%s) + timeout_seconds ))
while (( $(date +%s) < deadline )); do
  if curl -fsS "$api_url/api/readyz" >/dev/null; then
    break
  fi
  sleep 2
done
curl -fsS "$api_url/api/readyz" >/dev/null || { echo "Coyote API is not ready" >&2; exit 1; }

project_result=$(curl -sS -w '\n%{http_code}' -X POST "$api_url/api/projects" \
  -H 'Content-Type: application/json' \
  --data '{"name":"kind workspace smoke","slug":"kind-workspace-smoke"}')
project_status="${project_result##*$'\n'}"
project_body="${project_result%$'\n'*}"
if [[ "$project_status" != "201" && "$project_status" != "409" ]]; then
  printf '%s\n' "$project_body" >&2
  exit 1
fi

pipeline_yaml=$(cat <<'YAML'
version: 1
pipeline:
  name: kind-workspace-smoke
  image: alpine:3.21
steps:
  - name: generate
    run: echo "cross-worker-marker" > /workspace/generated.txt
  - name: consume
    run: |
      test -f /workspace/generated.txt
      test "$(cat /workspace/generated.txt)" = "cross-worker-marker"
      echo CROSS_WORKER_RESTORE_OK
YAML
)
build_response=$(jq -n --arg project_id kind-workspace-smoke --arg pipeline_yaml "$pipeline_yaml" '{project_id: $project_id, pipeline_yaml: $pipeline_yaml}' \
  | curl -sS -X POST "$api_url/api/builds/pipeline" -H 'Content-Type: application/json' --data @-)
build_id=$(jq -r '.data.id // empty' <<<"$build_response")
if [[ -z "$build_id" ]]; then
  echo "$build_response" | jq . >&2
  exit 1
fi

wait_for_jobs() {
  local deadline=$(( $(date +%s) + timeout_seconds ))
  while (( $(date +%s) < deadline )); do
    if [[ "$(kubectl --context "$context" -n "$namespace" get jobs -l "coyote-ci.io/build-id=$build_id" -o json | jq '.items | length')" == "2" ]]; then
      return 0
    fi
    sleep 2
  done
  return 1
}

wait_for_jobs || { echo "timed out waiting for two Coyote Kubernetes Jobs" >&2; exit 1; }
jobs_json=$(kubectl --context "$context" -n "$namespace" get jobs -l "coyote-ci.io/build-id=$build_id" -o json)
generate_job=$(jq -r '.items[] | select([.spec.template.spec.containers[] | select(.name == "build") | .args[]] | join(" ") | contains("> /workspace/generated.txt")) | .metadata.name' <<<"$jobs_json")
consume_job=$(jq -r '.items[] | select([.spec.template.spec.containers[] | select(.name == "build") | .args[]] | join(" ") | contains("CROSS_WORKER_RESTORE_OK")) | .metadata.name' <<<"$jobs_json")
[[ -n "$generate_job" && -n "$consume_job" && "$generate_job" != "$consume_job" ]] || { echo "could not identify generate and consume jobs" >&2; exit 1; }

for job_name in "$generate_job" "$consume_job"; do
  job_json=$(jq -c --arg name "$job_name" '.items[] | select(.metadata.name == $name)' <<<"$jobs_json")
  [[ "$job_name" == coyote-exec-* ]] || { echo "unexpected job name: $job_name" >&2; exit 1; }
  [[ "$(jq -r '.spec.backoffLimit' <<<"$job_json")" == "0" ]] || { echo "Job backoffLimit must be 0" >&2; exit 1; }
  [[ "$(jq -r '.metadata.labels["app.kubernetes.io/managed-by"]' <<<"$job_json")" == "coyote-ci" ]]
  [[ "$(jq -r '.metadata.labels["coyote-ci.io/build-id"]' <<<"$job_json")" == "$build_id" ]]
  [[ "$(jq -r '.spec.template.spec.automountServiceAccountToken' <<<"$job_json")" == "false" ]]
  jq -e '.spec.template.spec.volumes[] | select(.name == "workspace" and .emptyDir != null)' <<<"$job_json" >/dev/null
  jq -e '.spec.template.spec.initContainers[] | select(.name == "workspace-prepare")' <<<"$job_json" >/dev/null
  jq -e '.spec.template.spec.containers[] | select(.name == "build")' <<<"$job_json" >/dev/null
  jq -e '.spec.template.spec.containers[] | select(.name == "workspace-publish")' <<<"$job_json" >/dev/null
  jq -e '[.metadata.labels, .spec.template.metadata.labels] | tostring | test("claim"; "i") | not' <<<"$job_json" >/dev/null
  jq -e '(.spec.template.metadata.annotations["coyote-ci.io/execution-claim-digest"] // "") | test("^[0-9a-f]{64}$")' <<<"$job_json" >/dev/null
  kubectl --context "$context" -n "$namespace" wait --for=condition=complete "job/$job_name" --timeout="${timeout_seconds}s"
done

generate_pod=$(kubectl --context "$context" -n "$namespace" get pods -l "job-name=$generate_job" -o jsonpath='{.items[0].metadata.name}')
consume_pod=$(kubectl --context "$context" -n "$namespace" get pods -l "job-name=$consume_job" -o jsonpath='{.items[0].metadata.name}')
generate_node=$(kubectl --context "$context" -n "$namespace" get pod "$generate_pod" -o jsonpath='{.spec.nodeName}')
consume_node=$(kubectl --context "$context" -n "$namespace" get pod "$consume_pod" -o jsonpath='{.spec.nodeName}')
[[ -n "$generate_node" && -n "$consume_node" && "$generate_node" != "$consume_node" ]] || { echo "expected different worker nodes; generate=$generate_node consume=$consume_node" >&2; exit 1; }
echo "cross-worker placement: generate=$generate_node consume=$consume_node"

for pod_name in "$generate_pod" "$consume_pod"; do
  pod_json=$(kubectl --context "$context" -n "$namespace" get pod "$pod_name" -o json)
  [[ "$(jq -r '.spec.automountServiceAccountToken' <<<"$pod_json")" == "false" ]]
  [[ "$(jq -r '.status.containerStatuses[] | select(.name == "build") | .state.terminated.exitCode' <<<"$pod_json")" == "0" ]]
  jq -e '.spec.initContainers[] | select(.name == "workspace-prepare") | .volumeMounts[] | select(.name == "workspace-prepare-token")' <<<"$pod_json" >/dev/null
  jq -e '.spec.containers[] | select(.name == "workspace-publish") | .volumeMounts[] | select(.name == "workspace-publish-token")' <<<"$pod_json" >/dev/null
  jq -e '.spec.containers[] | select(.name == "build") | [.volumeMounts[].name] | index("workspace-prepare-token") == null and index("workspace-publish-token") == null and index("workspace-kubernetes-api") == null' <<<"$pod_json" >/dev/null
done

deadline=$(( $(date +%s) + timeout_seconds ))
while (( $(date +%s) < deadline )); do
  build_response=$(curl -sS "$api_url/api/builds/$build_id")
  steps_response=$(curl -sS "$api_url/api/builds/$build_id/steps")
  if [[ "$(jq -r '.data.status // empty' <<<"$build_response")" == "success" ]] \
    && [[ "$(jq -r '.data.steps[] | select(.name == "generate") | .status' <<<"$steps_response")" == "success" ]] \
    && [[ "$(jq -r '.data.steps[] | select(.name == "consume") | .status' <<<"$steps_response")" == "success" ]]; then
    break
  fi
  sleep 2
done
[[ "$(jq -r '.data.status // empty' <<<"$build_response")" == "success" ]] || { echo "$build_response" | jq . >&2; exit 1; }
[[ "$(jq -r '.data.steps[] | select(.name == "generate") | .status' <<<"$steps_response")" == "success" ]] || { echo "$steps_response" | jq . >&2; exit 1; }
[[ "$(jq -r '.data.steps[] | select(.name == "consume") | .status' <<<"$steps_response")" == "success" ]] || { echo "$steps_response" | jq . >&2; exit 1; }

logs=$(curl -sS "$api_url/api/builds/$build_id/steps/1/logs")
jq -e --arg marker "$marker" '[.data.chunks[].chunk_text] | join("") | contains($marker)' <<<"$logs" >/dev/null

for permission in 'get jobs.batch' 'list jobs.batch' 'watch jobs.batch' 'create jobs.batch' 'delete jobs.batch' 'get pods' 'list pods' 'watch pods' 'get pods/log'; do
  verb=${permission%% *}
  resource=${permission#* }
  [[ "$(kubectl --context "$context" -n "$namespace" auth can-i "$verb" "$resource" --as="system:serviceaccount:$namespace:coyote-kubernetes-worker")" == "yes" ]]
done
[[ "$(kubectl --context "$context" -n "$namespace" auth can-i get secrets --as="system:serviceaccount:$namespace:coyote-kubernetes-worker")" == "no" ]]
[[ "$(kubectl --context "$context" -n default auth can-i list jobs.batch --as="system:serviceaccount:$namespace:coyote-kubernetes-worker")" == "no" ]]

trap - ERR
echo "kind workspace smoke passed: build=$build_id generate_job=$generate_job generate_pod=$generate_pod generate_node=$generate_node consume_job=$consume_job consume_pod=$consume_pod consume_node=$consume_node"
