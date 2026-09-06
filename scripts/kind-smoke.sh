#!/usr/bin/env bash
set -euo pipefail

cluster_name="coyote-ci"
namespace="coyote-ci"
api_url="${API_URL:-http://localhost:8080}"
marker="COYOTE_KIND_SMOKE_MARKER"
timeout_seconds="${KIND_SMOKE_TIMEOUT_SECONDS:-180}"

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

require_command curl
require_command jq
require_command kubectl
require_command kind

if ! kind get clusters | grep -Fxq "$cluster_name"; then
  echo "kind cluster $cluster_name does not exist; run make kind-up first" >&2
  exit 1
fi

kubectl --context "kind-$cluster_name" -n "$namespace" rollout status deployment/coyote-kubernetes-worker --timeout=180s

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
  --data '{"name":"kind smoke","slug":"kind-smoke"}')
project_response="${project_result##*$'\n'}"
project_body="${project_result%$'\n'*}"
if [[ "$project_response" != "201" && "$project_response" != "409" ]]; then
  printf '%s\n' "$project_body" >&2
  exit 1
fi

pipeline_yaml=$(cat <<'YAML'
version: 1
pipeline:
  name: kind-smoke
  image: alpine:3.21
steps:
  - name: kind-smoke
    run: test ! -e /var/run/secrets/kubernetes.io/serviceaccount/token && echo COYOTE_KIND_SMOKE_MARKER
YAML
)
build_response=$(jq -n --arg project_id kind-smoke --arg pipeline_yaml "$pipeline_yaml" '{project_id: $project_id, pipeline_yaml: $pipeline_yaml}' \
  | curl -sS -X POST "$api_url/api/builds/pipeline" -H 'Content-Type: application/json' --data @-)
build_id=$(jq -r '.data.id // empty' <<<"$build_response")
if [[ -z "$build_id" ]]; then
  echo "$build_response" | jq . >&2
  exit 1
fi

deadline=$(( $(date +%s) + timeout_seconds ))
job_name=""
while (( $(date +%s) < deadline )); do
  job_name=$(kubectl --context "kind-$cluster_name" -n "$namespace" get jobs -l "coyote-ci.io/build-id=$build_id" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
  if [[ -n "$job_name" ]]; then
    break
  fi
  sleep 2
done
[[ -n "$job_name" ]] || { echo "timed out waiting for Coyote Kubernetes Job" >&2; exit 1; }

job_json=$(kubectl --context "kind-$cluster_name" -n "$namespace" get job "$job_name" -o json)
[[ "$job_name" == coyote-exec-* ]] || { echo "unexpected job name: $job_name" >&2; exit 1; }
[[ "$(jq -r '.spec.backoffLimit' <<<"$job_json")" == "0" ]] || { echo "Job backoffLimit must be 0" >&2; exit 1; }
[[ "$(jq -r '.metadata.labels["app.kubernetes.io/managed-by"]' <<<"$job_json")" == "coyote-ci" ]]
[[ "$(jq -r '.metadata.labels["coyote-ci.io/execution-job-id"] // empty' <<<"$job_json")" != "" ]]
[[ "$(jq -r '.metadata.labels["coyote-ci.io/build-id"]' <<<"$job_json")" == "$build_id" ]]
[[ "$(jq -r '.spec.template.spec.automountServiceAccountToken' <<<"$job_json")" == "false" ]]
if jq -e '[.metadata.labels, .spec.template.metadata.labels] | tostring | test("claim"; "i")' <<<"$job_json" >/dev/null; then
  echo "Kubernetes Job or Pod template labels contain claim metadata" >&2
  exit 1
fi
jq -e '(.spec.template.metadata.annotations["coyote-ci.io/execution-claim-digest"] // "") | test("^[0-9a-f]{64}$")' <<<"$job_json" >/dev/null

kubectl --context "kind-$cluster_name" -n "$namespace" wait --for=condition=complete "job/$job_name" --timeout="${timeout_seconds}s"
pod_name=$(kubectl --context "kind-$cluster_name" -n "$namespace" get pods -l "job-name=$job_name" -o jsonpath='{.items[0].metadata.name}')
pod_json=$(kubectl --context "kind-$cluster_name" -n "$namespace" get pod "$pod_name" -o json)
[[ "$(jq -r '.spec.automountServiceAccountToken' <<<"$pod_json")" == "false" ]]
[[ "$(jq -r '.status.containerStatuses[] | select(.name == "build") | .state.terminated.exitCode' <<<"$pod_json")" == "0" ]]

deadline=$(( $(date +%s) + timeout_seconds ))
while (( $(date +%s) < deadline )); do
  build_response=$(curl -sS "$api_url/api/builds/$build_id")
  steps_response=$(curl -sS "$api_url/api/builds/$build_id/steps")
  if [[ "$(jq -r '.data.status // empty' <<<"$build_response")" == "success" ]] && [[ "$(jq -r '.data.steps[0].status // empty' <<<"$steps_response")" == "success" ]]; then
    break
  fi
  sleep 2
done
[[ "$(jq -r '.data.status // empty' <<<"$build_response")" == "success" ]] || { echo "$build_response" | jq . >&2; exit 1; }
[[ "$(jq -r '.data.steps[0].status // empty' <<<"$steps_response")" == "success" ]] || { echo "$steps_response" | jq . >&2; exit 1; }
logs=$(curl -sS "$api_url/api/builds/$build_id/steps/0/logs")
jq -e --arg marker "$marker" '[.data.chunks[].chunk_text] | join("") | contains($marker)' <<<"$logs" >/dev/null

for permission in 'get jobs.batch' 'list jobs.batch' 'watch jobs.batch' 'create jobs.batch' 'delete jobs.batch' 'get pods' 'list pods' 'watch pods' 'get pods/log'; do
  verb=${permission%% *}
  resource=${permission#* }
  [[ "$(kubectl --context "kind-$cluster_name" -n "$namespace" auth can-i "$verb" "$resource" --as="system:serviceaccount:$namespace:coyote-kubernetes-worker")" == "yes" ]]
done
[[ "$(kubectl --context "kind-$cluster_name" -n "$namespace" auth can-i get secrets --as="system:serviceaccount:$namespace:coyote-kubernetes-worker")" == "no" ]]
[[ "$(kubectl --context "kind-$cluster_name" -n "$namespace" auth can-i update jobs.batch --as="system:serviceaccount:$namespace:coyote-kubernetes-worker")" == "no" ]]
[[ "$(kubectl --context "kind-$cluster_name" -n "$namespace" auth can-i patch jobs.batch --as="system:serviceaccount:$namespace:coyote-kubernetes-worker")" == "no" ]]
[[ "$(kubectl --context "kind-$cluster_name" -n default auth can-i list jobs.batch --as="system:serviceaccount:$namespace:coyote-kubernetes-worker")" == "no" ]]

echo "kind smoke passed: build=$build_id job=$job_name pod=$pod_name"