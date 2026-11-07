#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="${VERSION:-}"
registry="${RELEASE_REGISTRY:-}"
release_source="${RELEASE_SOURCE:-}"
previous_release="${PREVIOUS_RELEASE:-}"
release_channel="${RELEASE_CHANNEL:-latest}"
plan_only="${RELEASE_PLAN:-false}"
commit="$(git -C "$repo_root" rev-parse HEAD)"
build_date="${RELEASE_BUILD_DATE:-}"
build_date_supplied=false
[[ -n "$build_date" ]] && build_date_supplied=true
build_tag="build-${commit:0:12}-$(date -u +%Y%m%d%H%M%S)-$$-$RANDOM"
components=(server worker frontend mcp migrate)

fail() {
  echo "$*" >&2
  exit 2
}

is_semver() {
  [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]
}

is_rfc3339() {
  [[ "$1" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(Z|[+-][0-9]{2}:[0-9]{2})$ ]]
}

component_action() {
  case "$1" in
    server) printf '%s' "${COMPONENT_SERVER_ACTION:-}" ;;
    worker) printf '%s' "${COMPONENT_WORKER_ACTION:-}" ;;
    frontend) printf '%s' "${COMPONENT_FRONTEND_ACTION:-}" ;;
    mcp) printf '%s' "${COMPONENT_MCP_ACTION:-}" ;;
    migrate) printf '%s' "${COMPONENT_MIGRATE_ACTION:-}" ;;
  esac
}

component_env_prefix() {
  case "$1" in
    server) printf 'COMPONENT_SERVER' ;;
    worker) printf 'COMPONENT_WORKER' ;;
    frontend) printf 'COMPONENT_FRONTEND' ;;
    mcp) printf 'COMPONENT_MCP' ;;
    migrate) printf 'COMPONENT_MIGRATE' ;;
  esac
}

component_requested_version() {
  case "$1" in
    server) printf '%s' "${COMPONENT_SERVER_VERSION:-}" ;;
    worker) printf '%s' "${COMPONENT_WORKER_VERSION:-}" ;;
    frontend) printf '%s' "${COMPONENT_FRONTEND_VERSION:-}" ;;
    mcp) printf '%s' "${COMPONENT_MCP_VERSION:-}" ;;
    migrate) printf '%s' "${COMPONENT_MIGRATE_VERSION:-}" ;;
  esac
}

component_requested_image() {
  case "$1" in
    server) printf '%s' "${COMPONENT_SERVER_IMAGE:-}" ;;
    worker) printf '%s' "${COMPONENT_WORKER_IMAGE:-}" ;;
    frontend) printf '%s' "${COMPONENT_FRONTEND_IMAGE:-}" ;;
    mcp) printf '%s' "${COMPONENT_MCP_IMAGE:-}" ;;
    migrate) printf '%s' "${COMPONENT_MIGRATE_IMAGE:-}" ;;
  esac
}

set_component_record() {
  case "$1" in
    server) server_version="$2"; server_image="$3" ;;
    worker) worker_version="$2"; worker_image="$3" ;;
    frontend) frontend_version="$2"; frontend_image="$3" ;;
    mcp) mcp_version="$2"; mcp_image="$3" ;;
    migrate) migrate_version="$2"; migrate_image="$3" ;;
  esac
}

component_version() {
  case "$1" in
    server) printf '%s' "$server_version" ;;
    worker) printf '%s' "$worker_version" ;;
    frontend) printf '%s' "$frontend_version" ;;
    mcp) printf '%s' "$mcp_version" ;;
    migrate) printf '%s' "$migrate_version" ;;
  esac
}

component_image() {
  case "$1" in
    server) printf '%s' "$server_image" ;;
    worker) printf '%s' "$worker_image" ;;
    frontend) printf '%s' "$frontend_image" ;;
    mcp) printf '%s' "$mcp_image" ;;
    migrate) printf '%s' "$migrate_image" ;;
  esac
}

component_repository() {
  printf '%s/coyote-%s' "$registry" "$1"
}

has_build_actions() {
  local component
  for component in "${components[@]}"; do
    [[ "$(component_action "$component")" == "build" ]] && return 0
  done
  return 1
}

is_immutable_image() {
  [[ "$1" =~ ^[^@]+/[^@/:]+@sha256:[a-f0-9]{64}$ ]]
}

inspect_digest() {
  docker buildx imagetools inspect --format '{{.Manifest.Digest}}' "$1"
}

validate_configuration() {
  is_semver "$version" || fail "VERSION must be an exact semantic version without build metadata"
  [[ -n "$registry" && -n "$release_source" && -n "$previous_release" ]] || fail "RELEASE_REGISTRY, RELEASE_SOURCE, and PREVIOUS_RELEASE are required"
  [[ "$release_channel" == "" || "$release_channel" == "latest" || "$release_channel" == "stable" ]] || fail "RELEASE_CHANNEL must be latest, stable, or empty"
  [[ "$plan_only" == "true" || "$plan_only" == "false" ]] || fail "RELEASE_PLAN must be true or false"
  [[ -z "${RELEASE_COMMIT:-}" || "$RELEASE_COMMIT" == "$commit" ]] || fail "RELEASE_COMMIT must match the checked-out HEAD"
  [[ -z "$(git -C "$repo_root" status --porcelain --untracked-files=all)" ]] || fail "release builds require a clean worktree, including untracked files"
  command -v jq >/dev/null || fail "jq is required"
  command -v docker >/dev/null || fail "docker is required"

  for component in "${components[@]}"; do
    action="$(component_action "$component")"
    [[ "$action" == "build" || "$action" == "reuse" ]] || fail "$(component_env_prefix "$component")_ACTION must be build or reuse"
  done
  [[ "$(component_action server)" == "$(component_action worker)" ]] || fail "server and worker must both build or both reuse while they share source-runtime"
  [[ -z "$build_date" ]] || is_rfc3339 "$build_date" || fail "RELEASE_BUILD_DATE must be RFC3339 with seconds"
  if [[ "$plan_only" == "false" ]] && has_build_actions; then
    [[ -n "$build_date" ]] || fail "RELEASE_BUILD_DATE is required when building components"
  fi
}

load_previous_component() {
  local component="$1" action requested_version requested_image prior_version prior_image
  action="$(component_action "$component")"
  requested_version="$(component_requested_version "$component")"
  requested_image="$(component_requested_image "$component")"

  if [[ "$action" == "build" ]]; then
    [[ -z "$requested_image" ]] || fail "$component build must not set $(component_env_prefix "$component")_IMAGE"
    is_semver "$requested_version" || fail "$component build requires an explicit $(component_env_prefix "$component")_VERSION"
    set_component_record "$component" "$requested_version" ""
    return
  fi

  if [[ "$previous_schema" == "2" ]]; then
    prior_version="$(jq -er --arg component "$component" '.components[$component].version' "$previous_manifest")" || fail "previous release is missing $component component version"
    prior_image="$(jq -er --arg component "$component" '.components[$component].image' "$previous_manifest")" || fail "previous release is missing $component image"
    [[ -z "$requested_image" ]] || fail "$component reuse must not set $(component_env_prefix "$component")_IMAGE for a v2 predecessor"
    [[ -z "$requested_version" || "$requested_version" == "$prior_version" ]] || fail "$component reuse version conflicts with the v2 predecessor"
  elif [[ "$component" == "mcp" ]]; then
    prior_version="$requested_version"
    prior_image="$requested_image"
    is_semver "$prior_version" || fail "v1-to-v2 MCP reuse requires COMPONENT_MCP_VERSION"
    [[ -n "$prior_image" ]] || fail "v1-to-v2 MCP reuse requires COMPONENT_MCP_IMAGE"
  else
    prior_version="$requested_version"
    prior_image="$(jq -er --arg component "$component" '.images[$component]' "$previous_manifest")" || fail "previous v1 release is missing $component image"
    is_semver "$prior_version" || fail "v1-to-v2 $component reuse requires $(component_env_prefix "$component")_VERSION"
    [[ -z "$requested_image" ]] || fail "v1-to-v2 $component reuse must not set $(component_env_prefix "$component")_IMAGE"
  fi
  is_semver "$prior_version" || fail "invalid resolved $component version"
  is_immutable_image "$prior_image" || fail "invalid resolved $component image reference"
  set_component_record "$component" "$prior_version" "$prior_image"
}

load_previous_release() {
  release_root="${release_source#file://}"
  previous_manifest="$release_root/releases/$previous_release.json"
  [[ -f "$previous_manifest" ]] || fail "previous release manifest not found: $previous_release"
  previous_schema="$(jq -er '.schema_version' "$previous_manifest")" || fail "could not read previous release schema version"
  [[ "$previous_schema" == "1" || "$previous_schema" == "2" ]] || fail "unsupported previous release schema version: $previous_schema"
  for component in "${components[@]}"; do
    load_previous_component "$component"
  done
}

load_existing_release() {
  local component actual_version actual_image expected_image existing_commit existing_created_at
  existing_manifest="$release_root/releases/$version.json"
  retry_manifest=false
  [[ -e "$existing_manifest" ]] || return 0
  [[ -r "$existing_manifest" ]] || fail "existing release manifest is not readable: $version"
  existing_schema="$(jq -er '.schema_version' "$existing_manifest")" || fail "could not read existing release manifest: $version"
  [[ "$existing_schema" == "2" ]] || fail "existing release manifest must use schema v2: $version"
  [[ "$(jq -er '.release' "$existing_manifest")" == "$version" ]] || fail "existing release manifest version conflicts with requested release: $version"
  existing_commit="$(jq -er '.source.commit' "$existing_manifest")" || fail "existing release manifest is missing source commit: $version"
  [[ "$existing_commit" == "$commit" ]] || fail "existing release manifest source commit conflicts with the checked-out HEAD"
  existing_created_at="$(jq -er '.source.created_at' "$existing_manifest")" || fail "existing release manifest is missing source created_at: $version"
  if [[ "$build_date_supplied" == true ]]; then
    [[ "$existing_created_at" == "$build_date" ]] || fail "existing release manifest build date conflicts with RELEASE_BUILD_DATE"
  else
    build_date="$existing_created_at"
  fi

  for component in "${components[@]}"; do
    actual_version="$(jq -er --arg component "$component" '.components[$component].version' "$existing_manifest")" || fail "existing release manifest is missing $component component version"
    actual_image="$(jq -er --arg component "$component" '.components[$component].image' "$existing_manifest")" || fail "existing release manifest is missing $component image"
    [[ "$actual_version" == "$(component_version "$component")" ]] || fail "existing release manifest $component version conflicts with requested configuration"
    is_immutable_image "$actual_image" || fail "existing release manifest $component image is not immutable"
    if [[ "$(component_action "$component")" == "reuse" ]]; then
      expected_image="$(component_image "$component")"
      [[ "$actual_image" == "$expected_image" ]] || fail "existing release manifest $component image conflicts with the predecessor reuse record"
    fi
    set_component_record "$component" "$actual_version" "$actual_image"
    verify_reused_image "$component"
  done
  retry_manifest=true
}

verify_reused_image() {
  local component="$1" image actual_digest expected_digest
  image="$(component_image "$component")"
  expected_digest="${image##*@}"
  actual_digest="$(inspect_digest "$image")" || fail "reused $component image is not available: $image"
  [[ "$actual_digest" == "$expected_digest" ]] || fail "reused $component image digest changed: $image"
}

resolve_built_image() {
  local component="$1" repository digest
  repository="$(component_repository "$component")"
  digest="$(inspect_digest "$repository:$build_tag")" || fail "could not resolve $component digest"
  [[ "$digest" =~ ^sha256:[a-f0-9]{64}$ ]] || fail "could not resolve $component digest"
  set_component_record "$component" "$(component_version "$component")" "$repository@$digest"
}

build_source_runtime() {
  docker buildx build --platform linux/amd64,linux/arm64 --push \
    --tag "$(component_repository server):$build_tag" \
    --tag "$(component_repository worker):$build_tag" \
    --file "$repo_root/backend/Dockerfile" --target source-runtime \
    --build-arg "COYOTE_SERVER_VERSION=$(component_version server)" \
    --build-arg "COYOTE_WORKER_VERSION=$(component_version worker)" \
    --build-arg "COYOTE_COMMIT=$commit" --build-arg "COYOTE_BUILD_DATE=$build_date" "$repo_root/backend"
  resolve_built_image server
  resolve_built_image worker
}

build_component() {
  local component="$1" repository
  repository="$(component_repository "$component")"
  case "$component" in
    frontend)
      docker buildx build --platform linux/amd64,linux/arm64 --push --tag "$repository:$build_tag" --file "$repo_root/frontend/Dockerfile" "$repo_root/frontend"
      ;;
    mcp)
      docker buildx build --platform linux/amd64,linux/arm64 --push --tag "$repository:$build_tag" --file "$repo_root/backend/Dockerfile" --target source-runtime-mcp \
        --build-arg "COYOTE_APPLICATION_VERSION=$(component_version mcp)" --build-arg "COYOTE_COMMIT=$commit" --build-arg "COYOTE_BUILD_DATE=$build_date" "$repo_root/backend"
      ;;
    migrate)
      docker buildx build --platform linux/amd64,linux/arm64 --push --tag "$repository:$build_tag" --file "$repo_root/backend/Dockerfile" --target migrate-runtime \
        --build-arg "COYOTE_APPLICATION_VERSION=$(component_version migrate)" --build-arg "COYOTE_COMMIT=$commit" --build-arg "COYOTE_BUILD_DATE=$build_date" "$repo_root/backend"
      ;;
  esac
  resolve_built_image "$component"
}

lookup_tag_digest() {
  local tag_reference="$1" output lookup_status
  if output="$(inspect_digest "$tag_reference" 2>&1)"; then
    lookup_status=0
  else
    lookup_status=$?
  fi
  if [[ "$lookup_status" == 0 ]]; then
    printf '%s' "$output"
    return 0
  fi
  if printf '%s' "$output" | grep -Eqi 'manifest unknown|name unknown|not found'; then
    return 1
  fi
  echo "could not inspect component version tag $tag_reference: $output" >&2
  return 2
}

ensure_tag_is_available() {
  local component="$1" image="$2" tag_reference tag_digest lookup_status
  tag_reference="$(component_repository "$component"):$(component_version "$component")"
  if tag_digest="$(lookup_tag_digest "$tag_reference")"; then
    [[ "$tag_digest" == "${image##*@}" ]] || fail "$component version tag already points to different content: $tag_reference"
    return 0
  else
    lookup_status=$?
  fi
  [[ "$lookup_status" == 1 ]] || fail "could not determine whether component version tag is available: $tag_reference"
}

promote_image() {
  local component="$1" image="$2" tag_reference tag_digest lookup_status
  tag_reference="$(component_repository "$component"):$(component_version "$component")"
  if tag_digest="$(lookup_tag_digest "$tag_reference")"; then
    [[ "$tag_digest" == "${image##*@}" ]] || fail "$component version tag already points to different content: $tag_reference"
    return 0
  else
    lookup_status=$?
  fi
  [[ "$lookup_status" == 1 ]] || fail "could not determine whether component version tag is available: $tag_reference"
  docker buildx imagetools create --tag "$tag_reference" "$image"
}

verify_component_tag() {
  local component="$1" image="$2" tag_reference tag_digest
  tag_reference="$(component_repository "$component"):$(component_version "$component")"
  tag_digest="$(lookup_tag_digest "$tag_reference")" || fail "component version tag was not promoted: $tag_reference"
  [[ "$tag_digest" == "${image##*@}" ]] || fail "component version tag resolved to different content after promotion: $tag_reference"
}

publish_manifest() {
  (
    cd "$repo_root/backend"
    go run ./cmd/coyote release publish --manifest "$manifest" --release-source "$release_source"
  )
}

advance_channel() {
  [[ -n "$release_channel" ]] || return
  (
    cd "$repo_root/backend"
    go run ./cmd/coyote release publish --manifest "$manifest" --release-source "$release_source" --channel "$release_channel"
  )
}

print_plan() {
  local component action image
  printf 'Release plan\n'
  printf 'platform_version=%s\nprevious_release=%s\nmode=%s\n' "$version" "$previous_release" "$([[ "$plan_only" == true ]] && printf plan || printf publish)"
  for component in "${components[@]}"; do
    action="$(component_action "$component")"
    image="$(component_image "$component")"
    if [[ "$action" == "build" ]]; then
      printf 'component=%s action=build version=%s repository=%s image=to-build\n' "$component" "$(component_version "$component")" "$(component_repository "$component")"
    else
      printf 'component=%s action=reuse version=%s image=%s\n' "$component" "$(component_version "$component")" "$image"
    fi
  done
  printf 'constraint=server-worker actions are coupled while source-runtime is shared\n'
  printf 'validation=previous-release schema-v%s resolved\n' "$previous_schema"
  printf 'validation=reused immutable image digests verified\n'
  if [[ "$plan_only" == true ]]; then
    printf 'validation=build version-tag collision checks deferred until build digests exist\n'
    if has_build_actions && [[ -z "$build_date" ]]; then
      printf 'blocker=RELEASE_BUILD_DATE is required to publish newly built components\n'
    fi
  fi
}

validate_configuration
load_previous_release

for component in "${components[@]}"; do
  if [[ "$(component_action "$component")" == "reuse" ]]; then
    verify_reused_image "$component"
  fi
done

load_existing_release

if [[ "$retry_manifest" == "false" && -z "$build_date" && ! has_build_actions ]]; then
  build_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
fi

print_plan
if [[ "$plan_only" == "true" ]]; then
  exit 0
fi

if [[ "$retry_manifest" == "false" ]]; then
  if [[ "$(component_action server)" == "build" ]]; then
    build_source_runtime
  fi
  for component in frontend mcp migrate; do
    if [[ "$(component_action "$component")" == "build" ]]; then
      build_component "$component"
    fi
  done

  for component in "${components[@]}"; do
    if [[ "$(component_action "$component")" == "build" ]]; then
      ensure_tag_is_available "$component" "$(component_image "$component")"
    fi
  done

  manifest="$(mktemp)"
  trap 'rm -f "$manifest"' EXIT
  jq -n \
    --arg release "$version" --arg commit "$commit" --arg created_at "$build_date" \
    --arg server_version "$server_version" --arg server_image "$server_image" \
    --arg worker_version "$worker_version" --arg worker_image "$worker_image" \
    --arg frontend_version "$frontend_version" --arg frontend_image "$frontend_image" \
    --arg mcp_version "$mcp_version" --arg mcp_image "$mcp_image" \
    --arg migrate_version "$migrate_version" --arg migrate_image "$migrate_image" \
    '{schema_version: 2, release: $release, source: {commit: $commit, created_at: $created_at}, components: {server: {version: $server_version, image: $server_image}, worker: {version: $worker_version, image: $worker_image}, frontend: {version: $frontend_version, image: $frontend_image}, mcp: {version: $mcp_version, image: $mcp_image}, migrate: {version: $migrate_version, image: $migrate_image}}}' >"$manifest"

  if ! publish_manifest; then
    echo "release manifest publication failed; no component version tags or channel were promoted. Retry with the same RELEASE_BUILD_DATE." >&2
    exit 1
  fi
else
  manifest="$existing_manifest"
fi

for component in "${components[@]}"; do
  if [[ "$(component_action "$component")" == "build" ]]; then
    promote_image "$component" "$(component_image "$component")"
    verify_component_tag "$component" "$(component_image "$component")"
  fi
done

if ! advance_channel; then
  echo "channel advancement failed after component tags were promoted. Retry with the same RELEASE_BUILD_DATE." >&2
  exit 1
fi
