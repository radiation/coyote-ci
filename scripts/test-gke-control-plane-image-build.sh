#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT
mkdir "$temp_dir/bin"
log="$temp_dir/docker.log"

cat >"$temp_dir/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$DOCKER_LOG"
if [[ "$1 $2 $3" == "buildx imagetools inspect" ]]; then
  printf 'sha256:%064d\n' 0
fi
EOF
chmod +x "$temp_dir/bin/docker"

output="$(PATH="$temp_dir/bin:$PATH" \
  DOCKER_LOG="$log" \
  ARTIFACT_REGISTRY_REPOSITORY=example.invalid/coyote-ci \
  GKE_CONTROL_PLANE_TAG=test \
  "$repo_root/scripts/gke-control-plane-image-build.sh")"

for target in source-runtime migrate-runtime source-runtime-mcp; do
  grep -q -- "--target $target" "$log" || {
    echo "missing Docker target $target" >&2
    exit 1
  }
done
for image in coyote-server coyote-migrate coyote-mcp coyote-frontend; do
  grep -q -- "--tag example.invalid/coyote-ci/$image:test" "$log" || {
    echo "missing image build for $image" >&2
    exit 1
  }
done
for variable in COYOTE_SERVER_IMAGE COYOTE_MIGRATE_IMAGE COYOTE_MCP_IMAGE COYOTE_FRONTEND_IMAGE; do
  grep -q "^$variable=example.invalid/coyote-ci/" <<<"$output" || {
    echo "missing digest output for $variable" >&2
    exit 1
  }
done

echo "GKE control-plane image build test passed"
