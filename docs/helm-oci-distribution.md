# Helm OCI Chart Distribution

Coyote CI's public chart is `deploy/helm/coyote-ci`: control plane, optional MCP,
optional Kubernetes worker, scoped RBAC, and automatic Goose migration hooks in one
release. Workers are direct templates, not a subchart.

`deploy/helm/coyote-ci-worker` remains a temporary bridge for the existing GKE
two-release installation and recovery. Do not use it for fresh installations.
The V3 manifest already requires one `coyote-ci` chart and all five component
images. The existing Go publisher now produces the complete V3 release. Supply `images.mcp` separately when
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
`make release-publish-test` tests publication, retries and conflicts with fake
Helm/cloud responses; it does not contact a registry.

Chart `version` is the immutable package identity. Increment it whenever a
chart's packaged content changes. The unified chart starts at `0.2.0`.
The release pipeline stamps `version` and `appVersion` to match the immutable
application release; the source chart's development version is not modified.

## Publishing

Use the completed Build's existing Go publication command:

```sh
coyote release publish-coyote --build BUILD_ID \
  --release-source RELEASE_SOURCE \
  --helm-repository oci://REGISTRY/charts/coyote-ci \
  --immutable-chart-tags-confirmed
```

The command validates the exact build artifact, pushes only an absent version,
pulls it back and compares archive bytes. Docker Buildx resolves the OCI descriptor
as JSON, then Helm pulls by digest and the bytes are checked again before that
digest is recorded. Both Helm and Docker require chart-registry pull credentials.
An identical version is an idempotent retry; different bytes fail. Only explicit
`manifest unknown` or Helm 4's exact `<reference>: not found` response permits a
push. Authentication and ambiguous lookup failures fail closed.
The flag acknowledges operator-verified immutable-tag policy; it does not configure
or inspect registry policy. Writer credentials, retention and pull access must
already be provisioned. No channel is promoted by default.

See [release guarantees and prerequisites](releases.md) and the
[retirement inventory](release-planning.md). The standalone publisher and legacy
worker-only publication selections are removed. Historical packages remain
available for recovery; local legacy chart validation remains opt-in through
`HELM_PACKAGE_INCLUDE_LEGACY_WORKER=true`.

## Historical Manual OCI Installation

The manual sequence below is retained for the old chart revision used by the
existing installation. Do not apply it to new V3 releases with automatic hooks;
use the current [chart runbook](../deploy/helm/README.md) and
[database migration runbook](../deploy/docs/database-migrations.md) instead.

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