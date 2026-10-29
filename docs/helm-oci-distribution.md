# Helm OCI Chart Distribution

Coyote CI keeps chart source in this monorepo. The charts are packaged and
published independently of Coyote product-release creation:

- `deploy/helm/coyote-ci`
- `deploy/helm/coyote-ci-worker`

Product releases continue to contain four immutable image references: server,
frontend, worker, and migrate. The MCP image remains independently versioned.

## Local Validation

Run the existing chart checks plus package validation without cloud credentials:

```sh
make helm-render-parity helm-schema-test helm-migration-parity helm-first-install-test
make helm-package-validate
```

The package validator checks chart names and SemVer versions, packages both
charts in a temporary directory, enforces an archive allowlist, and compares
source-chart and packaged-chart renders using checked-in non-secret example
values. It never publishes artifacts.

Chart `version` is the immutable package identity. Increment it whenever a
chart's packaged content changes. `appVersion` is informational and does not
need to match either the chart version or the Coyote product-release version.

## Publishing

Publishing requires an explicit OCI destination and an operator confirmation
that the target registry enforces immutable tags:

```sh
make helm-publish-oci \
  OCI_REGISTRY=us-central1-docker.pkg.dev/PROJECT/coyote-ci \
  OCI_IMMUTABLE_TAGS_CONFIRMED=true
```

This publishes to these references:

```text
oci://us-central1-docker.pkg.dev/PROJECT/coyote-ci/charts/coyote-ci
oci://us-central1-docker.pkg.dev/PROJECT/coyote-ci/charts/coyote-ci-worker
```

To publish only one chart, for example after a worker publication failure:

```sh
make helm-publish-oci \
  OCI_REGISTRY=us-central1-docker.pkg.dev/PROJECT/coyote-ci \
  OCI_IMMUTABLE_TAGS_CONFIRMED=true \
  HELM_CHART_SELECTION=worker
```

The publisher validates locally, rejects existing versions, pushes without
force/overwrite behavior, captures the OCI digest, then pulls the version,
compares the pull digest to the push digest, and renders the published chart.
It treats only Helm's explicit OCI `manifest unknown` response as evidence that
a chart version is absent; authorization, repository, connectivity, and
ambiguous lookup errors fail closed. Pushes are independent. If one succeeds
and another fails, the successful chart remains published; repair and retry
only the unpublished chart.

For Artifact Registry, `OCI_REGISTRY` must be exactly
`<location>-docker.pkg.dev/<project>/<repository>`. The publisher verifies
that the repository exists and has `DOCKER` format, then uses authenticated,
read-only Docker image listings before publication. It treats a matching chart
tag as an existing version and fails closed on listing or authentication
errors. Other `*.pkg.dev` destinations, including nested paths, are rejected.

Before publishing, operators must verify Artifact Registry writer IAM,
immutable-tag enforcement, and cleanup/retention rules. Public anonymous pull
is an explicit policy decision and is not changed by these scripts.

## OCI Installation

Charts can be pulled and rendered without cloning the Coyote repository:

```sh
helm pull oci://REGISTRY/charts/coyote-ci --version CHART_VERSION
helm pull oci://REGISTRY/charts/coyote-ci-worker --version WORKER_CHART_VERSION
```

Generate a digest-pinned product release overlay with the existing resolver:

```sh
coyote release resolve \
  --release PRODUCT_RELEASE \
  --release-source RELEASE_SOURCE \
  --output release-overlay.yaml
```

For a first installation, follow the same migration-safe sequence as the
source chart. Provide the GKE-specific operator values and the release overlay
as the final values file. Use a unique DNS-label-compatible migration ID and
do not use `--atomic`, so a failed Job remains available for diagnosis.

### Phase 1: install at zero replicas and migrate

```sh
INSTALL_MIGRATION_ID="install-20261027-001"

helm upgrade --install coyote-ci \
  oci://REGISTRY/charts/coyote-ci \
  --version CHART_VERSION \
  --namespace "$COYOTE_NAMESPACE" --create-namespace \
  --values operator-values.yaml \
  --values release-overlay.yaml \
  --set server.replicasManagedExternally=false \
  --set server.replicas=0 \
  --set frontend.replicasManagedExternally=false \
  --set frontend.replicas=0 \
  --set migration.render=true \
  --set "migration.runID=$INSTALL_MIGRATION_ID" \
  --wait --wait-for-jobs --timeout 15m
```

When MCP is enabled, also set its replicas to zero in Phase 1. Do not proceed
until the migration Job succeeds.

### Phase 2: restore the control plane

```sh
helm upgrade coyote-ci \
  oci://REGISTRY/charts/coyote-ci \
  --version CHART_VERSION \
  --namespace "$COYOTE_NAMESPACE" \
  --values operator-values.yaml \
  --values release-overlay.yaml \
  --set migration.render=false \
  --wait --timeout 15m
```

The operator values restore the desired nonzero server, frontend, and optional
MCP replicas only after the database is migrated.

### Phase 3: optionally install the worker

```sh
helm upgrade --install coyote-ci-worker \
  oci://REGISTRY/charts/coyote-ci-worker \
  --version WORKER_CHART_VERSION \
  --namespace "$COYOTE_EXECUTION_NAMESPACE" --create-namespace \
  --values worker-values.yaml \
  --values release-overlay.yaml \
  --wait --timeout 15m
```

The current charts are GKE-specific and require environment-specific Google
Cloud, Workload Identity, Cloud SQL, storage, and secret configuration. OCI
distribution does not make them generic Kubernetes charts.