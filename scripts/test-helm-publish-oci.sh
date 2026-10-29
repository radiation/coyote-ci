#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
publisher="$repo_root/scripts/helm-publish-oci.sh"
temp_dir="$(mktemp -d)"
fake_helm="$temp_dir/helm"
fake_gcloud="$temp_dir/gcloud"
fake_validator="$temp_dir/validator"

cleanup() {
  rm -rf "$temp_dir"
}
trap cleanup EXIT

cat >"$fake_validator" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod 0644 "$fake_validator"

cat >"$fake_helm" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$FAKE_HELM_LOG"
case "$1" in
  show)
    case "${FAKE_HELM_LOOKUP:-missing}" in
      exists) exit 0 ;;
      error) echo "unauthorized: authentication required" >&2; exit 1 ;;
      ambiguous) echo "chart not found" >&2; exit 1 ;;
      repository) echo "name unknown: repository name not known to registry" >&2; exit 1 ;;
      connectivity) echo "dial tcp: connection refused" >&2; exit 1 ;;
      *) echo "manifest unknown: manifest unknown" >&2; exit 1 ;;
    esac
    ;;
  package)
    chart_dir="$2"
    destination=""
    shift 2
    while [[ $# -gt 0 ]]; do
      if [[ "$1" == "--destination" ]]; then destination="$2"; shift 2; else shift; fi
    done
    name="$(awk -F ': *' '$1 == "name" { print $2; exit }' "$chart_dir/Chart.yaml")"
    version="$(awk -F ': *' '$1 == "version" { print $2; exit }' "$chart_dir/Chart.yaml")"
    touch "$destination/$name-$version.tgz"
    ;;
  push)
    if [[ "${FAKE_HELM_FAIL_WORKER_PUSH:-false}" == "true" && "$2" == *coyote-ci-worker-* ]]; then
      echo "simulated worker push failure" >&2
      exit 1
    fi
    printf 'Pushed\nDigest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'
    ;;
  pull)
    reference="$2"
    version=""
    destination=""
    shift 2
    while [[ $# -gt 0 ]]; do
      case "$1" in
        --version) version="$2"; shift 2 ;;
        --destination) destination="$2"; shift 2 ;;
        *) shift ;;
      esac
    done
    name="${reference##*/}"
    touch "$destination/$name-$version.tgz"
    if [[ "${FAKE_HELM_PULL_DIGEST:-match}" == "mismatch" ]]; then
      printf 'Pulled\nDigest: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n'
    else
      printf 'Pulled\nDigest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'
    fi
    ;;
  template) exit 0 ;;
  *) echo "unexpected helm command: $1" >&2; exit 1 ;;
esac
EOF
chmod +x "$fake_helm"

cat >"$fake_gcloud" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$FAKE_GCLOUD_LOG"
case "$1 $2 $3" in
  "artifacts repositories describe")
    case "${FAKE_GCLOUD_REPOSITORY:-present}" in
      present) printf 'DOCKER\n' ;;
      wrong-format) printf 'MAVEN\n' ;;
      *) echo "repository was not found" >&2; exit 1 ;;
    esac
    ;;
  "artifacts docker images")
    case "${FAKE_GCLOUD_IMAGES:-present}" in
      present) printf '[{"package":"us-central1-docker.pkg.dev/example-project/coyote-ci/charts/coyote-ci","tags":["%s"]}]\n' "${FAKE_GCLOUD_TAGS:-unused}" ;;
      empty) printf '[]\n' ;;
      unrelated) printf '[{"package":"us-central1-docker.pkg.dev/example-project/coyote-ci/charts/other","tags":["0.1.0"]}]\n' ;;
      untagged) printf '[{"package":"us-central1-docker.pkg.dev/example-project/coyote-ci/charts/untagged"}]\n' ;;
      malformed) printf '[not-json\n' ;;
      unexpected) printf '[{"package":1,"tags":["0.1.0"]}]\n' ;;
      error) echo "request failed" >&2; exit 1 ;;
    esac
    ;;
  *) echo "unexpected gcloud command: $*" >&2; exit 1 ;;
esac
EOF
chmod +x "$fake_gcloud"

run_publisher() {
  : >"$temp_dir/helm.log"
  HELM_BIN="$fake_helm" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" OCI_REGISTRY="registry.example/coyote-ci" OCI_IMMUTABLE_TAGS_CONFIRMED=true "$@" bash "$publisher"
}

run_google_publisher() {
  : >"$temp_dir/helm.log"
  : >"$temp_dir/gcloud.log"
  HELM_BIN="$fake_helm" GCLOUD_BIN="$fake_gcloud" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" FAKE_GCLOUD_LOG="$temp_dir/gcloud.log" OCI_REGISTRY="us-central1-docker.pkg.dev/example-project/coyote-ci" OCI_IMMUTABLE_TAGS_CONFIRMED=true "$@" bash "$publisher"
}

expect_failure() {
  local description="$1"
  shift
  if "$@" >"$temp_dir/output" 2>&1; then
    echo "expected failure: $description" >&2
    exit 1
  fi
}

expect_failure "missing OCI registry" env HELM_BIN="$fake_helm" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" OCI_IMMUTABLE_TAGS_CONFIRMED=true bash "$publisher"
expect_failure "missing immutable-tag confirmation" env HELM_BIN="$fake_helm" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" OCI_REGISTRY="registry.example/coyote-ci" bash "$publisher"
expect_failure "nested Artifact Registry destination" env HELM_BIN="$fake_helm" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" OCI_REGISTRY="us-central1-docker.pkg.dev/example-project/coyote-ci/charts" OCI_IMMUTABLE_TAGS_CONFIRMED=true bash "$publisher"
expect_failure "duplicate chart version" env FAKE_HELM_LOOKUP=exists HELM_BIN="$fake_helm" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" OCI_REGISTRY="registry.example/coyote-ci" OCI_IMMUTABLE_TAGS_CONFIRMED=true bash "$publisher"
expect_failure "authorization lookup error" env FAKE_HELM_LOOKUP=error HELM_BIN="$fake_helm" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" OCI_REGISTRY="registry.example/coyote-ci" OCI_IMMUTABLE_TAGS_CONFIRMED=true bash "$publisher"
expect_failure "ambiguous not found lookup error" env FAKE_HELM_LOOKUP=ambiguous HELM_BIN="$fake_helm" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" OCI_REGISTRY="registry.example/coyote-ci" OCI_IMMUTABLE_TAGS_CONFIRMED=true bash "$publisher"
expect_failure "missing repository lookup error" env FAKE_HELM_LOOKUP=repository HELM_BIN="$fake_helm" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" OCI_REGISTRY="registry.example/coyote-ci" OCI_IMMUTABLE_TAGS_CONFIRMED=true bash "$publisher"
expect_failure "connectivity lookup error" env FAKE_HELM_LOOKUP=connectivity HELM_BIN="$fake_helm" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" OCI_REGISTRY="registry.example/coyote-ci" OCI_IMMUTABLE_TAGS_CONFIRMED=true bash "$publisher"

run_google_publisher env FAKE_GCLOUD_IMAGES=empty HELM_CHART_SELECTION=control-plane >"$temp_dir/google-success"
grep -q 'artifacts repositories describe coyote-ci --project example-project --location us-central1' "$temp_dir/gcloud.log"
grep -q 'artifacts docker images list us-central1-docker.pkg.dev/example-project/coyote-ci --include-tags --format=json' "$temp_dir/gcloud.log"
if grep -q '^show chart ' "$temp_dir/helm.log"; then
  echo "Artifact Registry lookup fell back to Helm" >&2
  exit 1
fi

run_google_publisher env FAKE_GCLOUD_IMAGES=unrelated HELM_CHART_SELECTION=control-plane >"$temp_dir/google-unrelated-image-success"
run_google_publisher env FAKE_GCLOUD_IMAGES=untagged HELM_CHART_SELECTION=control-plane >"$temp_dir/google-untagged-image-success"

: >"$temp_dir/helm.log"
: >"$temp_dir/gcloud.log"
expect_failure "existing Artifact Registry chart version" env FAKE_GCLOUD_TAGS=0.1.0 HELM_BIN="$fake_helm" GCLOUD_BIN="$fake_gcloud" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" FAKE_GCLOUD_LOG="$temp_dir/gcloud.log" OCI_REGISTRY="us-central1-docker.pkg.dev/example-project/coyote-ci" OCI_IMMUTABLE_TAGS_CONFIRMED=true HELM_CHART_SELECTION=control-plane bash "$publisher"
if grep -q '^push ' "$temp_dir/helm.log"; then
  echo "duplicate Artifact Registry version reached Helm push" >&2
  exit 1
fi

: >"$temp_dir/helm.log"
: >"$temp_dir/gcloud.log"
expect_failure "missing Artifact Registry repository" env FAKE_GCLOUD_REPOSITORY=missing HELM_BIN="$fake_helm" GCLOUD_BIN="$fake_gcloud" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" FAKE_GCLOUD_LOG="$temp_dir/gcloud.log" OCI_REGISTRY="us-central1-docker.pkg.dev/example-project/coyote-ci" OCI_IMMUTABLE_TAGS_CONFIRMED=true HELM_CHART_SELECTION=control-plane bash "$publisher"
if grep -q '^artifacts docker images list ' "$temp_dir/gcloud.log"; then
  echo "missing Artifact Registry repository reached tag listing" >&2
  exit 1
fi

: >"$temp_dir/gcloud.log"
expect_failure "non-Docker Artifact Registry repository" env FAKE_GCLOUD_REPOSITORY=wrong-format HELM_BIN="$fake_helm" GCLOUD_BIN="$fake_gcloud" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" FAKE_GCLOUD_LOG="$temp_dir/gcloud.log" OCI_REGISTRY="us-central1-docker.pkg.dev/example-project/coyote-ci" OCI_IMMUTABLE_TAGS_CONFIRMED=true HELM_CHART_SELECTION=control-plane bash "$publisher"
if grep -q '^artifacts docker images list ' "$temp_dir/gcloud.log"; then
  echo "non-Docker Artifact Registry repository reached image listing" >&2
  exit 1
fi
expect_failure "Artifact Registry image listing failure" env FAKE_GCLOUD_IMAGES=error HELM_BIN="$fake_helm" GCLOUD_BIN="$fake_gcloud" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" FAKE_GCLOUD_LOG="$temp_dir/gcloud.log" OCI_REGISTRY="us-central1-docker.pkg.dev/example-project/coyote-ci" OCI_IMMUTABLE_TAGS_CONFIRMED=true HELM_CHART_SELECTION=control-plane bash "$publisher"
expect_failure "malformed Artifact Registry tag JSON" env FAKE_GCLOUD_IMAGES=malformed HELM_BIN="$fake_helm" GCLOUD_BIN="$fake_gcloud" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" FAKE_GCLOUD_LOG="$temp_dir/gcloud.log" OCI_REGISTRY="us-central1-docker.pkg.dev/example-project/coyote-ci" OCI_IMMUTABLE_TAGS_CONFIRMED=true HELM_CHART_SELECTION=control-plane bash "$publisher"
expect_failure "unexpected Artifact Registry tag JSON" env FAKE_GCLOUD_IMAGES=unexpected HELM_BIN="$fake_helm" GCLOUD_BIN="$fake_gcloud" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" FAKE_GCLOUD_LOG="$temp_dir/gcloud.log" OCI_REGISTRY="us-central1-docker.pkg.dev/example-project/coyote-ci" OCI_IMMUTABLE_TAGS_CONFIRMED=true HELM_CHART_SELECTION=control-plane bash "$publisher"

run_publisher env HELM_CHART_SELECTION=all >"$temp_dir/success"
grep -q 'published chart=coyote-ci version=0.1.0 reference=oci://registry.example/coyote-ci/charts/coyote-ci digest=sha256:' "$temp_dir/success"
grep -q 'published chart=coyote-ci-worker version=0.1.0 reference=oci://registry.example/coyote-ci/charts/coyote-ci-worker digest=sha256:' "$temp_dir/success"
grep -q 'show chart oci://registry.example/coyote-ci/charts/coyote-ci --version 0.1.0' "$temp_dir/helm.log"
grep -q 'push .*oci://registry.example/coyote-ci/charts' "$temp_dir/helm.log"
grep -q 'pull oci://registry.example/coyote-ci/charts/coyote-ci-worker --version 0.1.0' "$temp_dir/helm.log"

expect_failure "pulled digest mismatch" env FAKE_HELM_PULL_DIGEST=mismatch HELM_BIN="$fake_helm" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" OCI_REGISTRY="registry.example/coyote-ci" OCI_IMMUTABLE_TAGS_CONFIRMED=true HELM_CHART_SELECTION=control-plane bash "$publisher"

expect_failure "partial publication" env FAKE_HELM_FAIL_WORKER_PUSH=true HELM_BIN="$fake_helm" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" OCI_REGISTRY="registry.example/coyote-ci" OCI_IMMUTABLE_TAGS_CONFIRMED=true HELM_CHART_SELECTION=all bash "$publisher"
grep -q 'succeeded=control-plane failed=worker' "$temp_dir/output"

run_publisher env HELM_CHART_SELECTION=worker >"$temp_dir/retry"
grep -q 'published chart=coyote-ci-worker' "$temp_dir/retry"
if grep -q 'coyote-ci-0.1.0.tgz' "$temp_dir/helm.log"; then
  echo "worker retry attempted control-plane publication" >&2
  exit 1
fi

echo "Helm OCI publishing tests passed"