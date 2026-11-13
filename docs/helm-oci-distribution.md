# Helm OCI Chart Distribution

Coyote CI's public chart is `deploy/helm/coyote-ci`: control plane, optional MCP,
optional Kubernetes worker, scoped RBAC, and manual migration resources in one
release. Workers are direct templates, not a subchart.

`deploy/helm/coyote-ci-worker` remains a temporary bridge for the existing GKE
two-release installation and recovery. Do not use it for fresh installations.
The V3 manifest already requires one `coyote-ci` chart and all five component
images. Completing V3 assembly is separate from chart consolidation; legacy
product-release workflows remain unchanged. Supply `images.mcp` separately when
using a legacy image overlay that does not include MCP.

## Local Validation

Run the existing chart checks plus package validation without cloud credentials:

```sh
make helm-render-parity helm-schema-test helm-migration-parity helm-first-install-test
make helm-package-validate
```

The package validator checks the chart name and SemVer version, packages only
`coyote-ci` by default, enforces an archive allowlist, and compares source/package
renders for portable and GKE unified configurations. It never publishes artifacts.
`bash scripts/test-helm-publish-oci.sh` tests publication with fake Helm/GCP commands,
including the existing explicit legacy retry paths.

Chart `version` is the immutable package identity. Increment it whenever a
chart's packaged content changes. The unified chart starts at `0.2.0`.
`appVersion` is informational. V3 release assembly must stamp chart `version`
to match the immutable application release; legacy chart packaging remains
independent until that separate assembly milestone is complete.

## Publishing

Publishing requires an explicit OCI destination and an operator confirmation
that the target registry enforces immutable tags:

```sh
make helm-publish-oci \
  OCI_REGISTRY=us-central1-docker.pkg.dev/PROJECT/coyote-ci \
  OCI_IMMUTABLE_TAGS_CONFIRMED=true
```

The default `unified` selection publishes only:

```text
oci://us-central1-docker.pkg.dev/PROJECT/coyote-ci/charts/coyote-ci
```

For the existing two-release GKE installation only, explicit `all`, `control-plane`,
and `worker` selections retain their previous behavior. For example, an approved
legacy worker-only publication retry remains:

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
ambiguous lookup errors fail closed. In explicit legacy `all` mode, pushes remain
independent: repair and retry only the unpublished chart after partial failure.
Keep historical packages available for recovery. Use
`HELM_PACKAGE_INCLUDE_LEGACY_WORKER=true bash scripts/helm-package-validate.sh`
when validating both packages for that temporary bridge. Retire legacy selections
and chart source once the existing GKE ownership migration resolves their users.

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

The unified chart can be pulled and rendered without cloning the repository:

```sh
helm pull oci://REGISTRY/charts/coyote-ci --version CHART_VERSION
```

Generate a digest-pinned product release overlay with the existing resolver:

```sh
coyote release resolve \
  --release PRODUCT_RELEASE \
  --release-source RELEASE_SOURCE \
  --output release-overlay.yaml
```

For a first installation, start with `deploy/helm/examples/generic-values.yaml`:
external PostgreSQL, OIDC, native Secrets, and filesystem storage work without
GCP dependencies. Replace placeholder images and configuration with real values.
Provision the execution namespace and namespace-local Secrets first; the chart
does not create or delete namespaces. Worker database references live under
`worker.secrets.existingSecrets.databaseUrl`.

Follow the same manual migration sequence as the source chart. Supply complete
desired operator values with Helm-managed replicas and the release overlay
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
  --set worker.replicasManagedExternally=false \
  --set worker.replicas=0 \
  --set migration.render=true \
  --set "migration.runID=$INSTALL_MIGRATION_ID" \
  --wait --wait-for-jobs --timeout 15m
```

When MCP is enabled, also set its replicas to zero in Phase 1. Do not proceed
until the migration Job succeeds.

### Phase 2: restore the application

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

The operator values restore the desired server, frontend, optional worker, and
MCP replicas only after the database is migrated. Filesystem storage supports
one server replica. There is no separate worker installation phase.

### Compatible upgrades and retention

For releases requiring no database migration, one `helm upgrade --install` with
complete desired values and the immutable release overlay upgrades the application.
Schema-changing upgrades keep the controlled quiesce/migrate/restore workflow.
Automatic migration hooks are deferred.

Drain execution before disabling workers or uninstalling. Native Secrets,
namespaces, and chart-created PVCs survive uninstall; select retained PVCs with
`storage.filesystem.existingClaim` on reinstall. Retention is not a backup.

Existing two-release GKE installations must retain their original values and
`worker.enabled: false` in the control-plane release until a separately approved
ownership migration. Unified worker overlays are rendering/compatibility examples,
not an ownership transfer. Changing annotations does not remove resources from
the old release's stored manifest. This change introduces no migration framework
and does not perform or rehearse ownership transfer.