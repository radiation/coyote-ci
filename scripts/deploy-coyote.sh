#!/usr/bin/env bash
# Deploy a published Coyote product version, or publish a completed build and deploy it.
set -Eeuo pipefail

usage() {
  cat <<'USAGE'
Usage:
  ./deploy-coyote.sh --version 0.0.16
  ./deploy-coyote.sh --build <build-uuid>
  ./deploy-coyote.sh --version 0.0.16 --dry-run

Options:
  --version VERSION    Deploy an already-published product release.
  --build UUID         Publish a completed generic build as a product release,
                       then deploy the version reported by the publisher.
  --dry-run            Validate and preview without deploying (NOTE: with
                       --build, publishing still occurs).
  --approve-online-upgrade
                       Approve pending migrations for this target upgrade after
                       reviewing compatibility with all running consumers.
  --acknowledge-maintenance
                       Acknowledge that work is stopped, builds drained, backups
                       taken, and database consumers quiesced out of band.
  -h, --help           Show this help.

Migration approvals default to false on every invocation. Choose at most one
approval mode; neither option performs compatibility review or workload drain.

Environment overrides:
  COYOTE_CLI           CLI path (default: ./backend/tmp/coyote)
  COYOTE_SERVER        API URL (default: https://coyote-ci.bryanchoate.com)
  COYOTE_RELEASE_STORE Release store (default: gs://bryanchoate-coyote-ci-releases)
  COYOTE_CHART_REPO    Publication-only Helm OCI repository (default: oci://us-central1-docker.pkg.dev/bryanchoate/coyote-ci/charts/coyote-ci)
  COYOTE_HELM_RELEASE  Helm release name (default: coyote-ci)
  COYOTE_NAMESPACE     Helm namespace (default: coyote-ci)
  COYOTE_KUBE_CONTEXT  Kube context (default: gke_bryanchoate_us-central1_coyote-ci-autopilot)

Deployment uses the fetched release manifest's digest-pinned Helm reference;
COYOTE_CHART_REPO cannot override the chart selected for deployment.

Prerequisites: jq, working Coyote API token, GCS/Artifact Registry permissions,
Helm registry login, Kubernetes access, and a complete generic application
release when using --build.
USAGE
}

fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
log() { printf '\n==> %s\n' "$*" >&2; }

VERSION=''
BUILD=''
DRY_RUN=false
ONLINE_UPGRADE_APPROVED=false
MAINTENANCE_ACKNOWLEDGED=false
while (($#)); do
  case "$1" in
    --version|--build)
      (($# >= 2)) || fail "missing value for $1"
      if [[ "$1" == --version ]]; then VERSION="$2"; else BUILD="$2"; fi
      shift 2 ;;
    --dry-run) DRY_RUN=true; shift ;;
    --approve-online-upgrade) ONLINE_UPGRADE_APPROVED=true; shift ;;
    --acknowledge-maintenance) MAINTENANCE_ACKNOWLEDGED=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) fail "unknown argument: $1 (see --help)" ;;
  esac
done
[[ -n "$VERSION" && -n "$BUILD" ]] && fail 'use either --version or --build, not both'
[[ -n "$VERSION" || -n "$BUILD" ]] || { usage >&2; exit 2; }
[[ "$ONLINE_UPGRADE_APPROVED" == true && "$MAINTENANCE_ACKNOWLEDGED" == true ]] && fail 'choose either online upgrade approval or maintenance acknowledgement, not both'

COYOTE_CLI="${COYOTE_CLI:-./backend/tmp/coyote}"
COYOTE_SERVER="${COYOTE_SERVER:-https://coyote-ci.bryanchoate.com}"
COYOTE_RELEASE_STORE="${COYOTE_RELEASE_STORE:-gs://bryanchoate-coyote-ci-releases}"
COYOTE_CHART_REPO="${COYOTE_CHART_REPO:-oci://us-central1-docker.pkg.dev/bryanchoate/coyote-ci/charts/coyote-ci}"
COYOTE_HELM_RELEASE="${COYOTE_HELM_RELEASE:-coyote-ci}"
COYOTE_NAMESPACE="${COYOTE_NAMESPACE:-coyote-ci}"
COYOTE_KUBE_CONTEXT="${COYOTE_KUBE_CONTEXT:-gke_bryanchoate_us-central1_coyote-ci-autopilot}"

[[ -x "$COYOTE_CLI" ]] || fail "CLI executable not found: $COYOTE_CLI (run from repo root or set COYOTE_CLI)"
for exe in helm kubectl jq; do command -v "$exe" >/dev/null || fail "$exe not found in PATH"; done
if [[ -n "$BUILD" && "$COYOTE_CHART_REPO" != oci://*/coyote-ci ]]; then
  fail 'COYOTE_CHART_REPO must be an oci:// URL ending in /coyote-ci'
fi
[[ "$COYOTE_KUBE_CONTEXT" == "$(kubectl config current-context)" ]] || log "Using explicitly configured context: $COYOTE_KUBE_CONTEXT (current default differs)"

# Avoid leaving configuration (which may include secrets) in a world-readable /tmp file.
umask 077
WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/coyote-deploy.XXXXXXXX")"
trap 'rm -rf -- "$WORK_DIR"' EXIT

if [[ -n "$BUILD" ]]; then
  log "Publishing completed build $BUILD"
  # The publisher handles immutable release creation; do not manually push chart
  # or write manifest. Publishing is a side effect even when --dry-run is set.
  PUBLISH_OUTPUT="$WORK_DIR/publish.txt"
  if ! "$COYOTE_CLI" --server "$COYOTE_SERVER" --output human release publish-coyote \
      --build "$BUILD" \
      --helm-repository "$COYOTE_CHART_REPO" \
      --immutable-chart-tags-confirmed \
      --release-source "$COYOTE_RELEASE_STORE" >"$PUBLISH_OUTPUT" 2>&1; then
    cat "$PUBLISH_OUTPUT" >&2
    fail 'publication failed; if the build was already published, deploy with --version instead'
  fi
  cat "$PUBLISH_OUTPUT"
  VERSION="$(sed -nE 's/^Published Coyote release ([^ ]+) \(sha256:[[:xdigit:]]+\).*$/\1/p' "$PUBLISH_OUTPUT" | head -n 1)"
  [[ -n "$VERSION" ]] || fail 'publisher succeeded but its output did not contain a version; inspect the output and use --version'
fi

[[ "$VERSION" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$ ]] || fail "unexpected release version: $VERSION"
log "Preparing $COYOTE_HELM_RELEASE release $VERSION on $COYOTE_KUBE_CONTEXT / $COYOTE_NAMESPACE"

# Check that the release is installed before taking a snapshot of live values.
helm status "$COYOTE_HELM_RELEASE" --namespace "$COYOTE_NAMESPACE" --kube-context "$COYOTE_KUBE_CONTEXT" >/dev/null || \
  fail 'existing Helm release not accessible'

log 'Fetching immutable product release manifest'
RELEASE_SNAPSHOT="$WORK_DIR/release-store"
mkdir -p "$RELEASE_SNAPSHOT/releases"
MANIFEST="$RELEASE_SNAPSHOT/releases/$VERSION.json"
"$COYOTE_CLI" --server "$COYOTE_SERVER" release fetch \
  --version "$VERSION" \
  --release-source "$COYOTE_RELEASE_STORE" \
  --output "$MANIFEST"
[[ -s "$MANIFEST" ]] || fail 'release fetch did not write its manifest'

log 'Resolving immutable product release image references from the fetched manifest'
"$COYOTE_CLI" --server "$COYOTE_SERVER" release resolve \
  --release "$VERSION" \
  --release-source "$RELEASE_SNAPSHOT" \
  --output "$WORK_DIR/resolved.yaml"
[[ -s "$WORK_DIR/resolved.yaml" ]] || fail 'release resolve did not write its overlay'

CHART_REF="$(jq -er --arg version "$VERSION" '
  select(.schema_version == 3 and .release == $version and .helm.name == "coyote-ci")
  | select(.helm.repository | strings | test("^oci://[^[:space:]@?#]+/coyote-ci$"))
  | select(.helm.digest | strings | test("^sha256:[a-f0-9]{64}$"))
  | .helm.repository + "@" + .helm.digest
' "$MANIFEST")" || fail 'release manifest does not contain a valid digest-pinned coyote-ci Helm descriptor'

log 'Saving existing Helm values (including computed defaults)'
helm get values "$COYOTE_HELM_RELEASE" \
  --namespace "$COYOTE_NAMESPACE" \
  --kube-context "$COYOTE_KUBE_CONTEXT" \
  --all --output yaml > "$WORK_DIR/live-values.yaml"
[[ -s "$WORK_DIR/live-values.yaml" ]] || fail 'could not obtain live Helm values'

HELM_ARGS=(
  "$COYOTE_HELM_RELEASE" "$CHART_REF"
  --namespace "$COYOTE_NAMESPACE"
  --kube-context "$COYOTE_KUBE_CONTEXT"
  --reset-values
  --values "$WORK_DIR/live-values.yaml"
  --values "$WORK_DIR/resolved.yaml"
  --set "migration.onlineUpgradeApproved=$ONLINE_UPGRADE_APPROVED"
  --set "migration.maintenanceAcknowledged=$MAINTENANCE_ACKNOWLEDGED"
)

log 'Running Helm server-side dry run (output retained privately only on failure)'
if ! helm upgrade "${HELM_ARGS[@]}" --dry-run=server --hide-secret > "$WORK_DIR/dry-run.txt" 2>&1; then
  # The dry-run may include non-Secret sensitive configuration; avoid dumping it.
  tail -n 30 "$WORK_DIR/dry-run.txt" >&2
  fail 'Helm dry-run failed; no upgrade was performed'
fi
if ! grep -q 'DESCRIPTION: Dry run complete' "$WORK_DIR/dry-run.txt"; then
  fail 'Helm dry-run did not report completion'
fi
log 'Helm dry run succeeded'

if [[ "$DRY_RUN" == true ]]; then
  log "Preview successful: $VERSION. No Kubernetes upgrade was performed."
  exit 0
fi

log "Upgrading $COYOTE_HELM_RELEASE to $VERSION"
helm upgrade "${HELM_ARGS[@]}" --wait --timeout 15m

log 'Confirming deployment status'
helm status "$COYOTE_HELM_RELEASE" --namespace "$COYOTE_NAMESPACE" --kube-context "$COYOTE_KUBE_CONTEXT"
log "Finished: $COYOTE_HELM_RELEASE $VERSION"
