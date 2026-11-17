# Helm OCI Chart Distribution

Coyote CI's public chart is `deploy/helm/coyote-ci`: control plane, optional MCP,
optional Kubernetes worker, scoped RBAC, and automatic Goose migration hooks in one
release. Workers are direct templates, not a subchart.

The V3 manifest already requires one `coyote-ci` chart and all five component
images. The existing Go publisher now produces the complete V3 release. Supply `images.mcp` separately when
using a legacy image overlay that does not include MCP.

## Local Validation

Run structural and package-integrity checks once, without cloud credentials:

```sh
make helm-package-validate
```

The package validator checks the chart name and SemVer version, packages only
`coyote-ci`, enforces an archive allowlist, and compares source/package
renders for portable and GKE unified configurations. It never publishes artifacts.
The Make target first runs `helm-schema-test`, then package self-tests including
invalid versions and secret-like archive paths. Helm and Ruby are required;
Ruby remains confined to the existing structural YAML assertions. No cluster
or registry is contacted by these checks.
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
worker-only publication selections are removed. Historical packages and recovery
evidence are archived outside Git.

## OCI Installation

Use the same automatic migration lifecycle as the [source chart](../deploy/helm/README.md)
and follow the [database migration runbook](../deploy/docs/database-migrations.md)
for upgrade approvals and failure recovery.

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

Supply complete desired operator values with Helm-managed replicas and the release
overlay as the final values file. Avoid `--atomic`, `--cleanup-on-fail`, and
`--no-hooks`; Helm cannot roll back database changes.

```sh
helm upgrade --install coyote-ci \
  oci://REGISTRY/charts/coyote-ci \
  --version CHART_VERSION \
  --namespace "$COYOTE_NAMESPACE" --create-namespace \
  --values operator-values.yaml \
  --values release-overlay.yaml \
  --wait --timeout 15m
```

Blocking hooks complete migrations before application resources are installed or
updated. Filesystem storage supports one server replica. There is no separate
worker installation or manual migration phase.

### Compatible upgrades and retention

Every install/upgrade runs the automatic hook. Pending schema changes on an existing
database require explicit online compatibility approval or maintenance acknowledgment
after quiescing consumers. Setting replicas to zero in the same Helm upgrade does
not quiesce existing workloads before its pre-upgrade hook.

Drain execution before disabling workers or uninstalling. Native Secrets,
namespaces, and chart-created PVCs survive uninstall; select retained PVCs with
`storage.filesystem.existingClaim` on reinstall. Retention is not a backup.

GKE uses this same single-release workflow with maintained environment values;
see [GKE operations](../deploy/helm/README.md#gke-operations).