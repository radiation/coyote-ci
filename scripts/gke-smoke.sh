#!/usr/bin/env bash
set -euo pipefail

namespace="${GKE_NAMESPACE:-coyote-ci}"
api_url="${API_URL:-${COYOTE_INTERNAL_API_URL:-}}"
marker="GKE_AUTOPILOT_SMOKE_OK"
timeout_seconds="${GKE_SMOKE_TIMEOUT_SECONDS:-600}"
build_id=""
job_name=""
pod_name=""

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

print_diagnostics() {
  set +e
  echo "GKE Autopilot smoke diagnostics"
  kubectl get nodes -o wide
  kubectl -n "$namespace" get pods -o wide
  kubectl -n "$namespace" get jobs -o wide
  kubectl -n "$namespace" get events --sort-by=.lastTimestamp
  kubectl -n "$namespace" logs deployment/coyote-kubernetes-worker --tail=200
  if [[ -n "$pod_name" ]]; then
    kubectl -n "$namespace" describe pod "$pod_name"
  fi
  if [[ -n "$build_id" ]]; then
    curl -sS "$api_url/api/builds/$build_id" | jq .
    curl -sS "$api_url/api/builds/$build_id/steps" | jq .
    curl -sS "$api_url/api/builds/$build_id/steps/0/logs" | jq .
  fi
}
trap print_diagnostics ERR

require_command curl
require_command jq
require_command kubectl

if [[ -z "$api_url" || "$api_url" == *"localhost"* || "$api_url" == *"host.docker.internal"* ]]; then
  echo "API_URL must be a GKE-reachable Coyote server URL" >&2
  exit 1
fi

kubectl -n "$namespace" rollout status deployment/coyote-kubernetes-worker --timeout="${timeout_seconds}s"
worker_pod=$(kubectl -n "$namespace" get pods -l app.kubernetes.io/name=coyote-kubernetes-worker -o jsonpath='{.items[0].metadata.name}')
[[ -n "$worker_pod" ]] || { echo "worker Pod was not found" >&2; exit 1; }
curl -fsS "$api_url/api/readyz" >/dev/null

project_result=$(curl -sS -w '\n%{http_code}' -X POST "$api_url/api/projects" -H 'Content-Type: application/json' --data '{"name":"gke autopilot smoke","slug":"gke-autopilot-smoke"}')
project_status="${project_result##*$'\n'}"
project_body="${project_result%$'\n'*}"
if [[ "$project_status" != "201" && "$project_status" != "409" ]]; then
  printf '%s\n' "$project_body" >&2
  exit 1
fi

pipeline_yaml=$(cat <<'YAML'
version: 1
pipeline:
  name: gke-autopilot-smoke
  image: alpine:3.21
steps:
  - name: gke-autopilot-smoke
    run: test ! -e /var/run/secrets/kubernetes.io/serviceaccount/token && echo GKE_AUTOPILOT_SMOKE_OK
YAML
)
build_response=$(jq -n --arg project_id gke-autopilot-smoke --arg pipeline_yaml "$pipeline_yaml" '{project_id: $project_id, pipeline_yaml: $pipeline_yaml}' | curl -sS -X POST "$api_url/api/builds/pipeline" -H 'Content-Type: application/json' --data @-)
build_id=$(jq -r '.data.id // empty' <<<"$build_response")
[[ -n "$build_id" ]] || { echo "$build_response" | jq . >&2; exit 1; }

deadline=$(( $(date +%s) + timeout_seconds ))
while (( $(date +%s) < deadline )); do
  job_name=$(kubectl -n "$namespace" get jobs -l "coyote-ci.io/build-id=$build_id" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
  [[ -n "$job_name" ]] && break
  sleep 3
done
[[ "$job_name" == coyote-exec-* ]] || { echo "timed out waiting for deterministic Coyote Job; got $job_name" >&2; exit 1; }

kubectl -n "$namespace" wait --for=condition=complete "job/$job_name" --timeout="${timeout_seconds}s"
pod_name=$(kubectl -n "$namespace" get pods -l "job-name=$job_name" -o jsonpath='{.items[0].metadata.name}')
pod_json=$(kubectl -n "$namespace" get pod "$pod_name" -o json)
node_name=$(jq -r '.spec.nodeName // empty' <<<"$pod_json")
[[ -n "$node_name" ]] || { echo "Autopilot did not assign an execution node" >&2; exit 1; }
[[ "$(jq -r '.spec.automountServiceAccountToken' <<<"$pod_json")" == "false" ]]
[[ "$(jq -r '.status.containerStatuses[] | select(.name == "build") | .state.terminated.exitCode' <<<"$pod_json")" == "0" ]]
jq -e '.spec.containers[] | select(.name == "build") | [.volumeMounts[].name] | index("workspace") != null and index("workspace-prepare-token") == null and index("workspace-publish-token") == null and index("workspace-kubernetes-api") == null' <<<"$pod_json" >/dev/null
jq -e '.spec.initContainers[] | select(.name == "workspace-prepare") | .volumeMounts[] | select(.name == "workspace-prepare-token")' <<<"$pod_json" >/dev/null
jq -e '.spec.containers[] | select(.name == "workspace-publish") | .volumeMounts[] | select(.name == "workspace-publish-token")' <<<"$pod_json" >/dev/null

deadline=$(( $(date +%s) + timeout_seconds ))
while (( $(date +%s) < deadline )); do
  build_response=$(curl -sS "$api_url/api/builds/$build_id")
  steps_response=$(curl -sS "$api_url/api/builds/$build_id/steps")
  if [[ "$(jq -r '.data.status // empty' <<<"$build_response")" == "success" ]] && [[ "$(jq -r '.data.steps[0].status // empty' <<<"$steps_response")" == "success" ]]; then
    break
  fi
  sleep 3
done
[[ "$(jq -r '.data.status // empty' <<<"$build_response")" == "success" ]]
[[ "$(jq -r '.data.steps[0].status // empty' <<<"$steps_response")" == "success" ]]
logs=$(curl -sS "$api_url/api/builds/$build_id/steps/0/logs")
jq -e --arg marker "$marker" '[.data.chunks[].chunk_text] | join("") | contains($marker)' <<<"$logs" >/dev/null

for permission in 'get jobs.batch' 'list jobs.batch' 'watch jobs.batch' 'create jobs.batch' 'delete jobs.batch' 'get pods' 'list pods' 'watch pods' 'get pods/log'; do
  verb=${permission%% *}
  resource=${permission#* }
  [[ "$(kubectl -n "$namespace" auth can-i "$verb" "$resource" --as="system:serviceaccount:$namespace:coyote-kubernetes-worker")" == "yes" ]]
done
[[ "$(kubectl -n "$namespace" auth can-i get secrets --as="system:serviceaccount:$namespace:coyote-kubernetes-worker")" == "no" ]]
[[ "$(kubectl -n default auth can-i list jobs.batch --as="system:serviceaccount:$namespace:coyote-kubernetes-worker")" == "no" ]]

echo "gke smoke passed: worker=$worker_pod build=$build_id job=$job_name pod=$pod_name node=$node_name"