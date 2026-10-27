#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
publisher="$repo_root/scripts/helm-publish-oci.sh"
temp_dir="$(mktemp -d)"
fake_helm="$temp_dir/helm"
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

run_publisher() {
  : >"$temp_dir/helm.log"
  HELM_BIN="$fake_helm" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" OCI_REGISTRY="registry.example/coyote-ci" OCI_IMMUTABLE_TAGS_CONFIRMED=true "$@" bash "$publisher"
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
expect_failure "duplicate chart version" env FAKE_HELM_LOOKUP=exists HELM_BIN="$fake_helm" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" OCI_REGISTRY="registry.example/coyote-ci" OCI_IMMUTABLE_TAGS_CONFIRMED=true bash "$publisher"
expect_failure "authorization lookup error" env FAKE_HELM_LOOKUP=error HELM_BIN="$fake_helm" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" OCI_REGISTRY="registry.example/coyote-ci" OCI_IMMUTABLE_TAGS_CONFIRMED=true bash "$publisher"
expect_failure "ambiguous not found lookup error" env FAKE_HELM_LOOKUP=ambiguous HELM_BIN="$fake_helm" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" OCI_REGISTRY="registry.example/coyote-ci" OCI_IMMUTABLE_TAGS_CONFIRMED=true bash "$publisher"
expect_failure "missing repository lookup error" env FAKE_HELM_LOOKUP=repository HELM_BIN="$fake_helm" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" OCI_REGISTRY="registry.example/coyote-ci" OCI_IMMUTABLE_TAGS_CONFIRMED=true bash "$publisher"
expect_failure "connectivity lookup error" env FAKE_HELM_LOOKUP=connectivity HELM_BIN="$fake_helm" HELM_PACKAGE_VALIDATOR="$fake_validator" FAKE_HELM_LOG="$temp_dir/helm.log" OCI_REGISTRY="registry.example/coyote-ci" OCI_IMMUTABLE_TAGS_CONFIRMED=true bash "$publisher"

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