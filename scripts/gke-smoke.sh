#!/usr/bin/env bash
set -euo pipefail

namespace="${GKE_NAMESPACE:-coyote-ci}"
api_url="${API_URL:-${COYOTE_INTERNAL_API_URL:-}}"
smoke_api_token="${COYOTE_SMOKE_API_TOKEN:-}"
marker="GKE_AUTOPILOT_SMOKE_OK"
timeout_seconds="${GKE_SMOKE_TIMEOUT_SECONDS:-600}"
build_id=""
job_name=""
pod_name=""

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

coyote_api_curl() {
  if [[ -n "$smoke_api_token" ]]; then
    curl -sS -H "Authorization: Bearer $smoke_api_token" "$@"
    return
  fi
  curl -sS "$@"
}

require_smoke_api_authentication() {
  local result response_status response_body
  if [[ -z "$smoke_api_token" ]]; then
    result=$(curl -sS -w '\n%{http_code}' "$api_url/api/info")
    response_status="${result##*$'\n'}"
    if [[ "$response_status" != "200" ]]; then
      echo "COYOTE_SMOKE_API_TOKEN is required when the Coyote server requires authentication (anonymous /api/info returned HTTP $response_status)" >&2
      exit 1
    fi
    return
  fi
  result=$(coyote_api_curl -w '\n%{http_code}' "$api_url/api/me")
  response_status="${result##*$'\n'}"
  response_body="${result%$'\n'*}"
  if [[ "$response_status" != "200" ]]; then
    echo "COYOTE_SMOKE_API_TOKEN was rejected by /api/me (HTTP $response_status)" >&2
    printf '%s\n' "$response_body" >&2
    exit 1
  fi
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
    coyote_api_curl "$api_url/api/builds/$build_id" | jq .
    coyote_api_curl "$api_url/api/builds/$build_id/steps" | jq .
    coyote_api_curl "$api_url/api/builds/$build_id/steps/0/logs" | jq .
  fi
}
trap print_diagnostics ERR

require_command curl
require_command jq
require_command kubectl

if [[ "$namespace" != "coyote-ci" ]]; then
  echo "GKE_NAMESPACE must be coyote-ci because the GKE worker manifest is namespace-specific" >&2
  exit 1
fi

if [[ -z "$api_url" || "$api_url" == *"localhost"* || "$api_url" == *"host.docker.internal"* ]]; then
  echo "API_URL must be a GKE-reachable Coyote server URL" >&2
  exit 1
fi

kubectl -n "$namespace" rollout status deployment/coyote-kubernetes-worker --timeout="${timeout_seconds}s"
worker_pod=$(kubectl -n "$namespace" get pods -l app.kubernetes.io/name=coyote-kubernetes-worker -o jsonpath='{.items[0].metadata.name}')
[[ -n "$worker_pod" ]] || { echo "worker Pod was not found" >&2; exit 1; }
worker_pod_json=$(kubectl -n "$namespace" get pod "$worker_pod" -o json)
jq -e '.spec.containers[] | select(.name == "worker") | [.env[]? | select(.name == "DATABASE_URL_FILE") | .value] | index("/var/run/secrets/coyote/database-url") != null' <<<"$worker_pod_json" >/dev/null
jq -e '.spec.containers[] | select(.name == "worker") | [.env[]? | select(.name == "DATABASE_URL")] | length == 0' <<<"$worker_pod_json" >/dev/null
jq -e '.spec.containers[] | select(.name == "worker") | [.env[]? | select(.name == "COYOTE_KUBERNETES_CACHE_HELPER_ENABLED" and .value == "true")] | length == 1' <<<"$worker_pod_json" >/dev/null
jq -e '.spec.containers[] | select(.name == "worker") | [.env[]? | select(.name == "COYOTE_KUBERNETES_ARTIFACT_HELPER_ENABLED" and .value == "true")] | length == 1' <<<"$worker_pod_json" >/dev/null
jq -e '.spec.volumes[] | select(.name == "database-url") | .csi.driver == "secrets-store-gke.csi.k8s.io" and .csi.volumeAttributes.secretProviderClass == "coyote-database-secrets"' <<<"$worker_pod_json" >/dev/null
jq -e '.spec.containers[] | select(.name == "worker") | .volumeMounts[] | select(.name == "database-url" and .mountPath == "/var/run/secrets/coyote" and .readOnly == true)' <<<"$worker_pod_json" >/dev/null
jq -e '.spec.containers[] | select(.name == "cloud-sql-proxy") | .ports[] | select(.name == "postgres" and .containerPort == 5432)' <<<"$worker_pod_json" >/dev/null
jq -e '.status.containerStatuses[] | select(.name == "cloud-sql-proxy") | .ready == true and .state.running != null' <<<"$worker_pod_json" >/dev/null
coyote_api_curl -f "$api_url/api/readyz" >/dev/null
require_smoke_api_authentication

project_result=$(coyote_api_curl -w '\n%{http_code}' -X POST "$api_url/api/projects" -H 'Content-Type: application/json' --data '{"name":"gke autopilot smoke","slug":"gke-autopilot-smoke"}')
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
    cache:
      preset: go
      policy: pull-push
YAML
)
build_response=$(jq -n --arg project_id gke-autopilot-smoke --arg pipeline_yaml "$pipeline_yaml" '{project_id: $project_id, pipeline_yaml: $pipeline_yaml}' | coyote_api_curl -X POST "$api_url/api/builds/pipeline" -H 'Content-Type: application/json' --data @-)
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
jq -e '.spec.containers[] | select(.name == "build") | [.volumeMounts[].name] | index("artifact-collect-token") == null' <<<"$pod_json" >/dev/null
jq -e '.spec.initContainers[] | select(.name == "workspace-prepare") | .volumeMounts[] | select(.name == "workspace-prepare-token")' <<<"$pod_json" >/dev/null
jq -e '.spec.containers[] | select(.name == "workspace-publish") | .volumeMounts[] | select(.name == "workspace-publish-token")' <<<"$pod_json" >/dev/null
jq -e '.spec.containers[] | select(.name == "artifact-collect") | [.volumeMounts[].name] | index("workspace") != null and index("artifact-collect-token") != null and index("workspace-kubernetes-api") != null and index("workspace-prepare-token") == null and index("workspace-publish-token") == null and index("cache-restore-token") == null and index("cache-save-token") == null' <<<"$pod_json" >/dev/null
jq -e '.spec.volumes[] | select(.name == "artifact-collect-token") | .projected.sources[0].serviceAccountToken.audience == "coyote-ci-workspace-helper-artifact-collect"' <<<"$pod_json" >/dev/null
jq -e '.spec.volumes[] | select(.name == "cache" and .emptyDir != null)' <<<"$pod_json" >/dev/null
jq -e '.spec.initContainers[] | select(.name == "cache-restore") | [.volumeMounts[].name] | index("cache") != null and index("cache-restore-token") != null and index("cache-save-token") == null and index("workspace-kubernetes-api") == null' <<<"$pod_json" >/dev/null
jq -e '.spec.containers[] | select(.name == "cache-save") | [.volumeMounts[].name] | index("cache") != null and index("cache-save-token") != null and index("cache-restore-token") == null and index("workspace-kubernetes-api") != null' <<<"$pod_json" >/dev/null
jq -e '.spec.containers[] | select(.name == "build") | [.volumeMounts[].name] | index("cache") != null and index("cache-restore-token") == null and index("cache-save-token") == null and index("workspace-kubernetes-api") == null' <<<"$pod_json" >/dev/null

deadline=$(( $(date +%s) + timeout_seconds ))
while (( $(date +%s) < deadline )); do
  build_response=$(coyote_api_curl "$api_url/api/builds/$build_id")
  steps_response=$(coyote_api_curl "$api_url/api/builds/$build_id/steps")
  if [[ "$(jq -r '.data.status // empty' <<<"$build_response")" == "success" ]] && [[ "$(jq -r '.data.steps[0].status // empty' <<<"$steps_response")" == "success" ]]; then
    break
  fi
  sleep 3
done
[[ "$(jq -r '.data.status // empty' <<<"$build_response")" == "success" ]]
[[ "$(jq -r '.data.steps[0].status // empty' <<<"$steps_response")" == "success" ]]
logs=$(coyote_api_curl "$api_url/api/builds/$build_id/steps/0/logs")
jq -e --arg marker "$marker" '[.data.chunks[].chunk_text] | join("") | contains($marker)' <<<"$logs" >/dev/null

fan_in_pipeline_yaml=$(cat <<'YAML'
version: 1
pipeline:
  name: gke-fan-in-workspace-smoke
  image: alpine:3.21
steps:
  - name: root
    run: printf 'root baseline\n' > root.txt
  - group:
      name: branches
      steps:
        - name: branch-a
          run: test -f root.txt && printf 'branch a\n' > branch-a.txt
        - name: branch-b
          run: test -f root.txt && printf 'branch b\n' > branch-b.txt
  - name: join
    run: test -f root.txt && test ! -e branch-a.txt && test ! -e branch-b.txt && echo GKE_FAN_IN_WORKSPACE_OK
YAML
)
fan_in_build_response=$(jq -n --arg project_id gke-autopilot-smoke --arg pipeline_yaml "$fan_in_pipeline_yaml" '{project_id: $project_id, pipeline_yaml: $pipeline_yaml}' | coyote_api_curl -X POST "$api_url/api/builds/pipeline" -H 'Content-Type: application/json' --data @-)
fan_in_build_id=$(jq -r '.data.id // empty' <<<"$fan_in_build_response")
[[ -n "$fan_in_build_id" ]] || { echo "$fan_in_build_response" | jq . >&2; exit 1; }

deadline=$(( $(date +%s) + timeout_seconds ))
while (( $(date +%s) < deadline )); do
  fan_in_build_response=$(coyote_api_curl "$api_url/api/builds/$fan_in_build_id")
  fan_in_steps_response=$(coyote_api_curl "$api_url/api/builds/$fan_in_build_id/steps")
  if [[ "$(jq -r '.data.status // empty' <<<"$fan_in_build_response")" == "success" ]] && [[ "$(jq -r '[.data.steps[].status] | length == 4 and all(.[]; . == "success")' <<<"$fan_in_steps_response")" == "true" ]]; then
    break
  fi
  sleep 3
done
[[ "$(jq -r '.data.status // empty' <<<"$fan_in_build_response")" == "success" ]]
[[ "$(jq -r '[.data.steps[].status] | length == 4 and all(.[]; . == "success")' <<<"$fan_in_steps_response")" == "true" ]]
fan_in_job_count=$(kubectl -n "$namespace" get jobs -l "coyote-ci.io/build-id=$fan_in_build_id" -o json | jq '[.items[] | select(.status.succeeded == 1)] | length')
[[ "$fan_in_job_count" == "4" ]] || { echo "expected four successful fan-in Jobs, got $fan_in_job_count" >&2; exit 1; }
fan_in_join_logs=$(coyote_api_curl "$api_url/api/builds/$fan_in_build_id/steps/3/logs")
jq -e '[.data.chunks[].chunk_text] | join("") | contains("GKE_FAN_IN_WORKSPACE_OK")' <<<"$fan_in_join_logs" >/dev/null

for permission in 'get jobs.batch' 'list jobs.batch' 'watch jobs.batch' 'create jobs.batch' 'delete jobs.batch' 'get pods' 'list pods' 'watch pods' 'get pods/log'; do
  verb=${permission%% *}
  resource=${permission#* }
  [[ "$(kubectl -n "$namespace" auth can-i "$verb" "$resource" --as="system:serviceaccount:$namespace:coyote-kubernetes-worker")" == "yes" ]]
done
[[ "$(kubectl -n "$namespace" auth can-i get secrets --as="system:serviceaccount:$namespace:coyote-kubernetes-worker")" == "no" ]]
[[ "$(kubectl -n default auth can-i list jobs.batch --as="system:serviceaccount:$namespace:coyote-kubernetes-worker")" == "no" ]]

echo "gke smoke passed: worker=$worker_pod build=$build_id job=$job_name pod=$pod_name node=$node_name"