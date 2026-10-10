#!/usr/bin/env bash
set -Euo pipefail

hostname="${GKE_TEMPORARY_HOSTNAME:-}"
api_token="${COYOTE_STAGING_SMOKE_API_TOKEN:-}"
control_namespace="${GKE_NAMESPACE:-coyote-ci}"
execution_namespace="${GKE_STAGING_NAMESPACE:-coyote-ci-staging}"
project_slug="${GKE_STAGING_SMOKE_PROJECT:-gke-staging-e2e}"
source_repository_url="${GKE_STAGING_SMOKE_REPOSITORY_URL:-https://github.com/radiation/coyote-ci.git}"
source_ref="${GKE_STAGING_SMOKE_SOURCE_REF:-main}"
timeout_seconds="${GKE_SMOKE_TIMEOUT_SECONDS:-900}"
api_url=""
build_id=""
execution_job=""
execution_pod=""
artifact_file=""
project_id=""
job_id=""

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

api_curl() {
  curl --fail-with-body --silent --show-error -H "Authorization: Bearer $api_token" "$@"
}

api_curl_with_status() {
  curl --silent --show-error -H "Authorization: Bearer $api_token" -w '\n%{http_code}' "$@"
}

execution_container_state() {
  local pod_name="$1"
  local container_name="$2"
  local pod
  pod="$(kubectl -n "$execution_namespace" get pod "$pod_name" -o json)" || return 1
  if jq -e --arg name "$container_name" '
    any(.spec.containers[]?, .spec.initContainers[]?; .name == $name)
  ' <<<"$pod" >/dev/null; then
    printf 'present\n'
  else
    printf 'absent\n'
  fi
}

optional_execution_container_logs() {
  local pod_name="$1"
  local container_name="$2"
  local state
  state="$(execution_container_state "$pod_name" "$container_name")" || return 1
  if [[ "$state" == "present" ]]; then
    kubectl -n "$execution_namespace" logs "$pod_name" -c "$container_name"
  else
    echo "optional execution container $container_name is absent from pod $pod_name; skipping logs" >&2
  fi
}

diagnostics() {
  set +e
  echo "GKE public E2E smoke diagnostics" >&2
  kubectl -n "$control_namespace" describe gateway coyote-temporary-public >&2
  kubectl -n "$control_namespace" describe httproute coyote-temporary-public coyote-temporary-http-redirect >&2
  kubectl -n "$control_namespace" get events --sort-by=.lastTimestamp >&2
  kubectl -n "$execution_namespace" get pods,jobs -o wide >&2
  kubectl -n "$execution_namespace" logs deployment/coyote-kubernetes-worker-staging --all-containers=true --tail=200 >&2
  if [[ -n "$execution_pod" ]]; then
    kubectl -n "$execution_namespace" describe pod "$execution_pod" >&2
    optional_execution_container_logs "$execution_pod" cache-restore >&2
  fi
  if [[ -n "$build_id" ]]; then
    api_curl "$api_url/api/builds/$build_id" | jq . >&2
    api_curl "$api_url/api/builds/$build_id/steps" | jq . >&2
  fi
}

cleanup() {
  if [[ -n "$artifact_file" ]]; then
    rm -f "$artifact_file"
  fi
}

on_error() {
  local exit_code="$1"
  local line_number="$2"
  local command="$3"
  echo "gke public E2E smoke failed: exit=$exit_code line=$line_number command=$command" >&2
  diagnostics
  exit "$exit_code"
}

gateway_ready() {
  kubectl -n "$control_namespace" get gateway coyote-temporary-public -o json | jq -e '
    def condition_true($type):
      any(.status.conditions[]?; .type == $type and .status == "True");
    condition_true("Accepted") and condition_true("Programmed")
  ' >/dev/null
}

http_route_ready() {
  local route_name="$1"
  kubectl -n "$control_namespace" get httproute "$route_name" -o json | jq -e '
    def condition_true($type):
      any(.conditions[]?; .type == $type and .status == "True");
    any(
      .status.parents[]?;
      condition_true("Accepted") and condition_true("ResolvedRefs")
    )
  ' >/dev/null
}

wait_for_build_success() {
  local candidate_build_id="$1"
  local deadline=$(( $(date +%s) + timeout_seconds ))
  while (( $(date +%s) < deadline )); do
    local build
    build="$(api_curl "$api_url/api/builds/$candidate_build_id")"
    if [[ "$(jq -r '.data.status // empty' <<<"$build")" == "success" ]]; then
      return
    fi
    sleep 3
  done
  echo "build $candidate_build_id did not reach success within ${timeout_seconds}s" >&2
  return 1
}

create_staging_job() {
  local pipeline_yaml="$1"
  local response
  response="$(jq -n \
    --arg project_id "$project_id" \
    --arg name "gke-staging-public-e2e-$(date -u +%Y%m%d%H%M%S)" \
    --arg repository_url "$source_repository_url" \
    --arg default_ref "$source_ref" \
    --arg pipeline_yaml "$pipeline_yaml" \
    '{project_id: $project_id, name: $name, repository_url: $repository_url, default_ref: $default_ref, pipeline_yaml: $pipeline_yaml, enabled: true}' |
    api_curl -X POST "$api_url/api/jobs" -H 'Content-Type: application/json' --data @-)"
  jq -r '.data.id // empty' <<<"$response"
}

wait_for_initial_job_build() {
  local candidate_job_id="$1"
  local deadline=$(( $(date +%s) + timeout_seconds ))
  while (( $(date +%s) < deadline )); do
    local builds
    builds="$(api_curl "$api_url/api/jobs/$candidate_job_id/builds")"
    build_id="$(jq -r '.data.builds[0].id // empty' <<<"$builds")"
    [[ -n "$build_id" ]] && return
    sleep 3
  done
  echo "initial build was not queued for staging job $candidate_job_id" >&2
  return 1
}

run_staging_job() {
  local candidate_job_id="$1"
  local response
  response="$(api_curl -X POST "$api_url/api/jobs/$candidate_job_id/run" -H 'Content-Type: application/json' --data '{}')"
  jq -r '.data.id // empty' <<<"$response"
}

wait_for_execution_job() {
  local candidate_build_id="$1"
  local deadline=$(( $(date +%s) + timeout_seconds ))
  while (( $(date +%s) < deadline )); do
    execution_job="$(kubectl -n "$execution_namespace" get jobs -l "coyote-ci.io/build-id=$candidate_build_id" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
    [[ -n "$execution_job" ]] && return
    sleep 3
  done
  echo "staging execution Job was not created for build $candidate_build_id" >&2
  return 1
}

trap cleanup EXIT
trap 'on_error "$?" "$LINENO" "$BASH_COMMAND"' ERR

require_command curl
require_command dig
require_command jq
require_command kubectl
require_command shasum

[[ -n "$hostname" && "$hostname" != *"://"* && "$hostname" != */* ]] || { echo "GKE_TEMPORARY_HOSTNAME must be a hostname" >&2; exit 1; }
[[ "$hostname" != "coyote-ci.bryanchoate.com" ]] || { echo "GKE_TEMPORARY_HOSTNAME must not be the production hostname" >&2; exit 1; }
[[ -n "$api_token" ]] || { echo "COYOTE_STAGING_SMOKE_API_TOKEN must be a staging token with build:read, build:logs, and build:run" >&2; exit 1; }
[[ "$source_repository_url" == https://* || "$source_repository_url" == http://* ]] || { echo "GKE_STAGING_SMOKE_REPOSITORY_URL must be an HTTP(S) Git repository URL" >&2; exit 1; }
[[ -n "$source_ref" ]] || { echo "GKE_STAGING_SMOKE_SOURCE_REF must not be empty" >&2; exit 1; }
api_url="https://${hostname}"

gateway_ready
http_route_ready coyote-temporary-public
http_route_ready coyote-temporary-http-redirect
[[ -n "$(dig +short A "$hostname")" ]] || { echo "$hostname does not resolve to an IPv4 address" >&2; exit 1; }
curl --fail --silent --show-error "$api_url/" >/dev/null
curl --fail --silent --show-error "$api_url/api/readyz" >/dev/null
curl --fail --silent --show-error "$api_url/readyz" >/dev/null
curl --fail --silent --show-error "$api_url/swagger/" >/dev/null
auth_status="$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' "$api_url/auth/login")"
[[ "$auth_status" == "302" || "$auth_status" == "303" ]] || { echo "/auth/login returned HTTP $auth_status instead of an OIDC redirect" >&2; exit 1; }

kubectl -n "$execution_namespace" rollout status deployment/coyote-kubernetes-worker-staging --timeout="${timeout_seconds}s"
kubectl -n "$control_namespace" get deployment coyote-kubernetes-worker >/dev/null
worker_pod="$(kubectl -n "$execution_namespace" get pods -l app.kubernetes.io/name=coyote-kubernetes-worker-staging -o jsonpath='{.items[0].metadata.name}')"
[[ -n "$worker_pod" ]] || { echo "staging worker Pod was not found" >&2; exit 1; }
workers="$(api_curl "$api_url/api/workers")"
jq -e --arg before "$(date -u -v-5M +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '5 minutes ago' +%Y-%m-%dT%H:%M:%SZ)" \
  '[.data.workers[]? | select(.last_heartbeat_at != null and .last_heartbeat_at >= $before)] | length > 0' <<<"$workers" >/dev/null ||
  { echo "no current staging worker heartbeat was returned by the staging control plane" >&2; exit 1; }

project_response="$(api_curl_with_status -X POST "$api_url/api/projects" -H 'Content-Type: application/json' \
  --data "{\"name\":\"GKE staging E2E\",\"slug\":\"$project_slug\"}")"
project_status="${project_response##*$'\n'}"
project_body="${project_response%$'\n'*}"
if [[ "$project_status" == "201" ]]; then
  project_id="$(jq -r '.data.id // empty' <<<"$project_body")"
elif [[ "$project_status" == "409" ]]; then
  projects="$(api_curl "$api_url/api/projects")"
  project_id="$(jq -r --arg slug "$project_slug" '[.data.projects[]? | select(.slug == $slug) | .id][0] // empty' <<<"$projects")"
else
  printf '%s\n' "$project_body" >&2
  exit 1
fi
[[ -n "$project_id" ]] || { echo "could not resolve durable ID for staging project $project_slug" >&2; exit 1; }

pipeline_yaml=$(cat <<'YAML'
version: 1
pipeline:
  name: gke-staging-public-e2e
  image: golang:1.27.2
steps:
  - name: prepare
    run: |
      mkdir -p output
      printf 'workspace-handoff\n' > handoff.txt
      printf 'example.invalid/gke-staging-e2e v0.0.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n' > go.sum
      printf 'artifact-content\n' > output/staging-e2e.txt
    artifacts:
      - output/staging-e2e.txt
  - name: consume-workspace
    run: |
      test "$(cat handoff.txt)" = "workspace-handoff"
      test -f go.sum
      mkdir -p /root/.cache/go-build /go/pkg/mod
      printf 'cache-content\n' > /root/.cache/go-build/staging-e2e
      echo WORKSPACE_HANDOFF_OK
    cache:
      preset: go
      policy: pull-push
YAML
)

job_id="$(create_staging_job "$pipeline_yaml")"
[[ -n "$job_id" ]] || { echo "staging job creation returned no job ID" >&2; exit 1; }
wait_for_initial_job_build "$job_id"
wait_for_execution_job "$build_id"
[[ "$(kubectl -n "$control_namespace" get jobs -l "coyote-ci.io/build-id=$build_id" -o json | jq '.items | length')" == "0" ]] ||
  { echo "staging build created an execution Job in the control-plane namespace" >&2; exit 1; }
kubectl -n "$execution_namespace" wait --for=condition=complete "job/$execution_job" --timeout="${timeout_seconds}s"
execution_pod="$(kubectl -n "$execution_namespace" get pods -l "job-name=$execution_job" -o jsonpath='{.items[0].metadata.name}')"
wait_for_build_success "$build_id"
logs="$(api_curl "$api_url/api/builds/$build_id/steps/1/logs")"
jq -e '[.data.chunks[].chunk_text] | join("") | contains("WORKSPACE_HANDOFF_OK")' <<<"$logs" >/dev/null

artifacts="$(api_curl "$api_url/api/builds/$build_id/artifacts")"
artifact_id="$(jq -r '.data.artifacts[0].id // empty' <<<"$artifacts")"
[[ -n "$artifact_id" ]] || { echo "artifact upload did not produce a build artifact" >&2; exit 1; }
artifact_file="$(mktemp)"
api_curl "$api_url/api/builds/$build_id/artifacts/$artifact_id/download" > "$artifact_file"
[[ "$(cat "$artifact_file")" == "artifact-content" ]] || { echo "downloaded artifact content did not match" >&2; exit 1; }
expected_digest="$(printf 'artifact-content\n' | shasum -a 256 | awk '{print $1}')"
actual_digest="$(shasum -a 256 "$artifact_file" | awk '{print $1}')"
[[ "$actual_digest" == "$expected_digest" ]] || { echo "downloaded artifact checksum did not match" >&2; exit 1; }
rm -f "$artifact_file"
artifact_file=""

second_build_id="$(run_staging_job "$job_id")"
[[ -n "$second_build_id" ]] || { echo "second staging job run returned no build ID" >&2; exit 1; }
wait_for_execution_job "$second_build_id"
kubectl -n "$execution_namespace" wait --for=condition=complete "job/$execution_job" --timeout="${timeout_seconds}s"
execution_pod="$(kubectl -n "$execution_namespace" get pods -l "job-name=$execution_job" -o jsonpath='{.items[0].metadata.name}')"
wait_for_build_success "$second_build_id"
cache_restore_state="$(execution_container_state "$execution_pod" cache-restore)"
if [[ "$cache_restore_state" == "present" ]]; then
  kubectl -n "$execution_namespace" logs "$execution_pod" -c cache-restore | grep -q 'cache_transfer operation=restore_client outcome=hit'
else
  echo "cache restore is not configured for repeat build $second_build_id; skipping cache-hit assertion" >&2
fi

echo "gke public E2E smoke passed: host=$hostname job=$job_id build=$build_id repeat_build=$second_build_id staging_worker=$worker_pod"
