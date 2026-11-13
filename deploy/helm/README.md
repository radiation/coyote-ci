# Coyote CI Helm charts

These charts support external PostgreSQL and native Kubernetes Secrets without
cloud-provider dependencies. GKE integrations are explicit opt-ins. They do not
provision a database, identity provider, cloud infrastructure, or credentials.

- `coyote-ci` owns the control plane in its Helm release namespace.
- `coyote-ci-worker` owns execution-worker resources in its separate Helm release
  namespace. Chart consolidation and automatic migration hooks are deferred.

## Portable installation

Start with `examples/generic-control-plane-values.yaml` and
`examples/generic-worker-values.yaml`. These contain placeholder image digests
and Secret references, never credentials. Supply real digest-pinned images,
an HTTPS OIDC issuer, client ID, callback/public URLs and bootstrap admin emails.
OIDC remains required; the chart does not disable authentication for local use.

Select the namespaces using `helm --namespace`. Leave `namespace: ""` in the
values to use the release namespace; a nonempty override must match it. Set
`executionNamespace` on the control plane and `controlPlaneNamespace` on the
worker explicitly. Namespace sharing remains an explicit legacy exception.

### PostgreSQL and Secrets

Create Secrets out of band in each workload's namespace. For example, create
the database Secret from a local credential file, not Helm values:

```sh
kubectl -n "$COYOTE_NAMESPACE" create secret generic coyote-database \
  --from-file=url=/secure/path/database-url
```

The file contains a full PostgreSQL URL, including host, port, database, user,
password and TLS parameters. Use TLS according to the database's trust policy;
the chart does not disable database TLS or provision certificates. The server,
worker, MCP and migration image all support `DATABASE_URL_FILE`.

Set `secrets.existingSecrets.<credential>.name` and `.key` for `databaseUrl`,
`workspaceHelperCapability`, `oidcClient` (the client secret), and `session` in
the control-plane values. The worker requires only `databaseUrl` in its own
namespace. Projected volumes expose only the selected keys. MCP and migrations
receive only the database key, not session, OIDC or helper signing credentials.
Missing references fail rendering; missing Secret objects/keys prevent startup.
Secret value changes generally require a workload restart to reload configuration.

For other supported runtime credentials, `server.extraEnvSecrets` and
`worker.extraEnvSecrets` map environment variable names to `{name, key}` Secret
references. For example, use `GITHUB_STATUS_TOKEN` or `SMTP_PASSWORD`. These
cannot override chart-managed database, auth, storage or execution settings.
Do not put credential values in Helm values, ConfigMaps or image overlays.

### Filesystem persistence

The default `storage.provider: filesystem` creates the `coyote-data` PVC and
mounts it at `/var/lib/coyote`. Artifacts, cache and workspace revisions use
separate directories on this volume. Set `storage.filesystem.size` (default
`10Gi`), `storageClassName` (null uses the cluster default; `""` selects no
class), and `accessModes` (`ReadWriteOnce`, or `ReadWriteOncePod` with a
supporting CSI driver). A suitable provisioner or prebound volume is required.

Set `storage.filesystem.existingClaim` to use an operator-owned claim in the
control-plane namespace instead. No PVC is then created or owned by this chart.
The default filesystem pod UID/GID and fsGroup are `10001`; an existing volume
must be writable by that identity. The storage driver must honor fsGroup, or
the operator must provision compatible ownership. Existing artifact files may
be owner-readable only; changing UID on an existing volume requires deliberate
ownership migration. No privileged ownership-repair container is introduced.

Filesystem operation supports one Helm-managed server replica (zero during
migrations). It uses `Recreate` to avoid overlapping writers and attachment
conflicts, with brief upgrade downtime. External replica management and more
than one server replica are rejected. This is not an HA topology. GCS retains
the existing rolling strategy and replica-management behavior.

Chart-created PVCs carry `helm.sh/resource-policy: keep`: Helm uninstall or
switching away from filesystem does not intentionally delete the claim. For a
subsequent install, explicitly select the retained claim with `existingClaim`.
The chart never deletes an operator-owned claim. Namespace deletion or manual
PVC deletion can still destroy data according to the PV reclaim policy. Back
up the database and volume together before deleting either; Helm retention is
not a backup. Changing a claim name or provider does not migrate stored bytes.

Kubernetes execution helpers transfer artifacts, cache and workspace revisions
through the server API. Workers and build containers do not mount the server
PVC. Generic Kubernetes execution does not require Cloud Build. The optional
managed-image Cloud Build integration requires `cloudBuild.enabled: true` and
GCS artifact storage; the chart rejects Cloud Build with server-local artifacts.

### Install and validate

Use the Phase 1 and Phase 2 commands below with the generic customer values:
first install with zero application replicas and the explicit migration Job,
then enable the control plane after Goose succeeds. Install the worker chart
separately afterward. New PVC creation is deferred until the server is enabled,
so `WaitForFirstConsumer` storage classes do not block the zero-replica migration
phase; previously created claims remain retained.
In generic mode the migration Job uses a native database
Secret with no Cloud SQL proxy or service-account token. Automatic one-operation
migrations and single-chart installation belong to later milestones.

Services are private ClusterIP endpoints. Supply your own ingress/TLS setup;
the existing `gateway` integration is GKE-specific and remains disabled by
default. Optional MCP needs a digest-pinned image, not a Google identity in
generic mode. Arbitrary service-account annotations can be configured through
`serviceAccounts.<name>.annotations` without assigning Kubernetes permissions.

Run `make helm-schema-test helm-package-validate` for provider-neutral renders
and source/package equivalence. `bash scripts/kind-helm-smoke.sh` creates a
disposable Kind cluster, builds local OCI test images without pushing them,
installs PostgreSQL/native Secrets and an HTTPS OIDC discovery fixture, runs
manual migrations, and checks authenticated artifact downloads across a server
restart plus PVC retention after uninstall. It seeds one artifact and metadata
instead of duplicating the execution suite. The fixture is not an OIDC login
test; only the test server pod receives its local CA trust mount. PostgreSQL
uses plaintext transport only inside this isolated test cluster.

The smoke script deletes its cluster by default and keeps logs and short-lived
test credentials in its reported `/tmp/coyote-helm-smoke.*` directory. Delete
that directory after inspecting results. Set `KIND_HELM_KEEP_CLUSTER=true` only
for diagnosis; delete the reported cluster afterward. Existing RBAC and build
token-isolation suites remain the execution-security coverage.

## GKE rendering examples

Use the checked-in non-secret example values only for rendering and validation:

```sh
helm lint --strict deploy/helm/coyote-ci \
  --namespace coyote-ci \
  --values deploy/helm/examples/gke-staging-control-plane-values.yaml
helm lint --strict deploy/helm/coyote-ci-worker \
  --namespace coyote-ci-staging \
  --values deploy/helm/examples/gke-staging-worker-values.yaml
scripts/helm-render-parity.sh
scripts/helm-schema-test.sh
```

## GKE first installation

Before installation, provision the external GKE/GCP dependencies described in
[`../docs/gke-gcp-deployment.md`](../docs/gke-gcp-deployment.md): Cloud SQL,
Secret Manager secrets, Workload Identity and IAM bindings, GCS buckets, OIDC,
and optional Gateway address, certificate, and DNS resources. The migration
ServiceAccount needs Cloud SQL Client plus read access only to the database URL
secret. The migration Job mounts that secret through `coyote-migrate-secrets`
and reaches Cloud SQL through its local Cloud SQL Auth Proxy sidecar.

Resolve an approved Coyote release to its immutable image overlay before
installing:

```sh
coyote release resolve \
  --release "$COYOTE_RELEASE_VERSION" \
  --release-source "$COYOTE_RELEASE_SOURCE" \
  --output coyote-release-images.yaml
```

Use the generated overlay as the final values file. It supplies the
digest-pinned server, frontend, worker, and migration images. MCP remains a
separate environment-specific image setting.

Use Helm-managed replicas for a fresh installation. For example,
`customer-values.yaml` can contain:

```yaml
server:
  replicasManagedExternally: false
  replicas: 2

frontend:
  replicasManagedExternally: false
  replicas: 1
```

For an optional MCP installation, configure its desired replica count the same
way:

```yaml
mcp:
  enabled: true
  replicasManagedExternally: false
  replicas: 1
  auth:
    allowedOrigins:
      - https://mcp.example.invalid
```

The MCP image and GSA remain required environment-specific settings. Fresh
installations must not copy `replicasManagedExternally: true` from the
established GKE integration deployment. External replica management is
reserved for workflows that explicitly manage the Deployment scale
subresource.

Independent Coyote installations must use separate Kubernetes namespaces. The
TokenReview name helper prevents collisions for its cluster-scoped RBAC
objects, but the chart does not make every namespaced resource safe for
multiple Helm releases in one namespace.

### Phase 1: install at zero replicas and migrate

Use a unique DNS-label-compatible migration ID. Do not use `--atomic`: if the
Job fails, it must remain available for inspection. This command requires a
Helm version that supports `--wait-for-jobs`.

```sh
INSTALL_MIGRATION_ID="install-20261021-001"

helm upgrade --install coyote-ci deploy/helm/coyote-ci \
  --namespace "$COYOTE_NAMESPACE" \
  --create-namespace \
  --values customer-values.yaml \
  --values coyote-release-images.yaml \
  --set server.replicasManagedExternally=false \
  --set server.replicas=0 \
  --set frontend.replicasManagedExternally=false \
  --set frontend.replicas=0 \
  --set migration.render=true \
  --set "migration.runID=$INSTALL_MIGRATION_ID" \
  --wait \
  --wait-for-jobs \
  --timeout 15m
```

When enabling MCP, supply its separately published digest-pinned image and GSA
in `customer-values.yaml`, and add:

```sh
--set mcp.replicasManagedExternally=false \
--set mcp.replicas=0
```

The first revision temporarily overrides the desired customer replica counts
to zero. It creates the migration ServiceAccount, SecretProviderClass, and
Job while the server, frontend, and optional MCP database consumers remain
unavailable. Kubernetes object creation does not guarantee operational
readiness; `--wait --wait-for-jobs` waits for the Job's result and reports its
failure. The Job image runs Goose against an empty database, applies only
remaining migrations against a partially migrated database, and succeeds
without changes against an already-current database.

If the migration fails or times out, inspect the retained Job and its logs,
correct the cause, and repeat Phase 1 with a new migration ID. Do not retry
while a prior Job might still be running or has ambiguous status. Do not run
Goose down migrations automatically.

### Phase 2: enable the control plane

After Phase 1 succeeds, enable the desired replicas from
`customer-values.yaml`:

```sh
helm upgrade coyote-ci deploy/helm/coyote-ci \
  --namespace "$COYOTE_NAMESPACE" \
  --values customer-values.yaml \
  --values coyote-release-images.yaml \
  --set migration.render=false \
  --wait \
  --timeout 15m
```

This removes the completed migration Job from the Helm release and restores
the configured nonzero server, frontend, and optional MCP replicas from
`customer-values.yaml`. Capture successful Job status or logs before Phase 2
if they are needed for installation evidence. If Phase 2 fails, correct the
workload or configuration issue and repeat only Phase 2; a completed migration
does not need to be rerun.

### Phase 3: optionally install the Kubernetes worker

The control plane can be installed without a Kubernetes execution worker. To
enable Kubernetes job execution after the control plane is ready:

```sh
helm upgrade --install coyote-ci-worker deploy/helm/coyote-ci-worker \
  --namespace "$COYOTE_EXECUTION_NAMESPACE" \
  --create-namespace \
  --values worker-values.yaml \
  --values coyote-release-images.yaml \
  --wait \
  --timeout 15m
```

The worker is installed only after the database is migrated and does not run
its own migration Job.

### Upgrades

The two-phase workflow is for first installation only. A version change does
not by itself establish whether a database migration is required. Until
migrations are backwards-compatible across supported application versions,
release notes must explicitly state either:

- **No database migration required:** use ordinary `helm upgrade --install`.
- **Database migration required:** use the controlled quiesce/migrate/restore
  procedure for the established environment.

The existing GKE rollout-recovery path remains the schema-changing upgrade
procedure. Do not use the first-install commands to replace its safeguards.

### Existing GKE installations

Before upgrading from cloud-enabled chart defaults, explicitly add the following
to **both** operator values files (and preserve all existing cloud references):

```yaml
gke:
  enabled: true
  secretManagerCSI: {enabled: true}
  workloadIdentity: {enabled: true}
cloudSql:
  enabled: true
storage:
  provider: gcs
```

Also set `cloudBuild.enabled: true` on workers that use the existing managed-image
integration. The checked-in GKE examples and staging-values generator emit these
opt-ins. The generator must still only be run by an authorized operator; no live
resource changes are part of this chart update.

Cloud SQL connection names, Secret Manager references, Google service-account
identities, GCS settings and Cloud Build configuration left behind without the
matching opt-ins fail rendering. Do not use `--reuse-values` as a substitute for
reviewing the complete desired values. Run `helm template` with the operator
values and immutable image overlay before upgrading. Intentionally disabling
an integration requires removing its old settings and supplying the replacement
native Secret/storage configuration. Disabling GCS does not copy existing blobs.

With the opt-ins supplied, names, identities, mounts, RBAC, migration behavior
and GCS deployment strategies remain unchanged. No Helm ownership migration is
performed by this change.

New releases generate the cluster-scoped TokenReview RBAC name from the Helm
release and namespace. Existing installations that already use
`coyote-server-tokenreview` must explicitly set:

```yaml
rbac:
  tokenReviewNameOverride: coyote-server-tokenreview
```

Verify the live ClusterRole and ClusterRoleBinding name before upgrading. Do
not rely on a chart default to preserve a historical name. The checked-in GKE
examples do not establish the actual operator-local configuration: inspect the
local values used by the existing deployment and preserve its verified live
name before upgrading. Do not modify operator-local values automatically.

## Optional MCP deployment

The MCP Deployment is disabled by default and has no Gateway or public ingress
route. Enable it only after publishing the dedicated MCP image:

```sh
make gke-control-plane-image-build
# Export the emitted digest-pinned COYOTE_MCP_IMAGE value.
```

Set `mcp.enabled: true`, `images.mcp: <COYOTE_MCP_IMAGE>`, and a dedicated
`serviceAccounts.mcp.gsaEmail` in the environment-specific control-plane
values. When using `gke-helm-rollout.sh`, also set
`mcp.replicasManagedExternally: true` so the wrapper can scale MCP to zero
before migrations and restore the configured `mcp.replicas` afterward. The MCP
GSA needs Cloud SQL Client and read access only to the
database-url secret. It does not need worker, artifact-storage, or Kubernetes
API permissions. Its service-account token is mounted only because the Cloud
SQL Auth Proxy requires it for GKE Workload Identity; the MCP KSA receives no
Kubernetes RBAC. The chart mounts only that database secret and runs a local
Cloud SQL Auth Proxy sidecar. It currently reuses the control-plane database
account for compatibility; a future dedicated restricted database role can be
introduced through a separate MCP database-url secret.

MCP remains ClusterIP-only; add an authenticated TLS gateway integration only
after selecting a suitable private ingress policy and explicit allowed origins.

The migration Job is excluded from ordinary chart renders. For established
GKE installations, it is rendered explicitly for a unique rollout run and applied by
[`scripts/gke-helm-rollout.sh`](../../scripts/gke-helm-rollout.sh):

```sh
helm template coyote-ci deploy/helm/coyote-ci \
  --namespace coyote-ci \
  --values deploy/helm/examples/gke-staging-control-plane-values.yaml \
  --show-only templates/migration-job.yaml \
  --set migration.render=true \
  --set migration.runID=example-20260928
```

The wrapper first scales Helm-managed control-plane and worker Deployments to
zero, applies and waits for that exact migration Job, then restores the
control plane before the worker and checks Gateway conditions. It never uses
Helm hooks or attempts a database downgrade. Failed Jobs remain for diagnosis;
a retry uses a different `HELM_ROLLOUT_ID`. Use
`GKE_HELM_DRY_RUN=true scripts/gke-helm-rollout.sh` to inspect the mutation
sequence after local chart rendering succeeds.

## Staging ownership adoption

The existing staging resources were created by raw manifests. Before the
normal rollout wrapper can be used, adopt exactly those steady-state resources
without recreating them:

First generate Git-ignored, environment-specific values from the currently
running staging resources. This reads Deployments, ServiceAccounts, ConfigMaps,
SecretProviderClass resource references, and Gateway metadata; it never reads
Kubernetes Secrets or Secret Manager payloads. The migration Job is not a
steady-state resource, so its digest-pinned image must come from the existing
operator deployment input:

```sh
COYOTE_MIGRATE_IMAGE=us-central1-docker.pkg.dev/<project>/coyote-ci/coyote-migrate@sha256:<digest> \
  make gke-helm-staging-values
```

This writes the Git-ignored local files:

```text
.local/helm/gke-staging-control-plane-values.yaml
.local/helm/gke-staging-worker-values.yaml
```

It refuses to overwrite them unless
`GKE_HELM_VALUES_OVERWRITE=true` is supplied. Use that explicit refresh after
a reviewed staging deployment change. The adoption and rollout scripts use
these local paths by default; CI and render-parity checks continue to use the
tracked examples.

Then lint the exact local values and run the non-mutating preflight:

```sh
helm lint --strict deploy/helm/coyote-ci \
  --namespace coyote-ci \
  --values .local/helm/gke-staging-control-plane-values.yaml
helm lint --strict deploy/helm/coyote-ci-worker \
  --namespace coyote-ci-staging \
  --values .local/helm/gke-staging-worker-values.yaml
make gke-helm-adopt-dry-run
```

Only a zero-drift dry-run can proceed to reviewed ownership adoption:

```sh
make gke-helm-adopt
```

The default is non-mutating. It renders both charts, verifies every rendered
object exists live, compares its desired structure to the live object while
ignoring only server-populated fields, rejects partial or conflicting Helm
ownership, and lists the exact resources that would be adopted. Review this
output before continuing.

After a reviewed dry-run, the explicit mutation command adds only Helm's
standard metadata (`app.kubernetes.io/managed-by=Helm`,
`meta.helm.sh/release-name`, and `meta.helm.sh/release-namespace`) and then
establishes `coyote-ci` in `coyote-ci` and `coyote-ci-worker` in
`coyote-ci-staging`:

```sh
make gke-helm-adopt
```

The Namespace and migration Job are intentionally excluded. The script writes
separate control-plane and worker metadata snapshots under
`.gke-helm-adoption-state/`. Each release is transactional: a worker failure
restores only worker metadata and leaves an established control-plane release
untouched. If patching or release establishment fails, it restores only the
metadata changed by that release attempt and never runs `helm uninstall` or
deletes workloads. To explicitly restore a retained snapshot, use:

```sh
GKE_HELM_ADOPT_ROLLBACK_STATE=.gke-helm-adoption-state/<timestamp> \
  scripts/gke-helm-adopt.sh
```

### Failed adoption inspection and recovery

Do not retry adoption, run `helm uninstall`, delete release storage, or remove
ownership metadata until the failed attempt has been inspected:

```sh
helm status coyote-ci -n coyote-ci
helm status coyote-ci-worker -n coyote-ci-staging
helm list -n coyote-ci
helm list -n coyote-ci-staging
kubectl -n coyote-ci get secret,configmap -l owner=helm,name=coyote-ci -o name
kubectl -n coyote-ci get httproute coyote-temporary-public \
  -o jsonpath='{.metadata.labels.app\.kubernetes\.io/managed-by}{"\t"}{.metadata.annotations.meta\.helm\.sh/release-name}{"\n"}'
find .gke-helm-adoption-state -maxdepth 2 -type f -print
```

After a reviewed inspection, a retained snapshot can restore only the three
Helm ownership metadata keys; it does not delete or recreate workloads:

```sh
GKE_HELM_ADOPT_ROLLBACK_STATE=.gke-helm-adoption-state/<timestamp> \
  scripts/gke-helm-adopt.sh
```

Rollback is resumable. For each retained snapshot, the script restores metadata
only when the live resource still has that adoption attempt's exact Helm
ownership metadata. If an interrupted rollback has already restored a resource,
the script reports it as already restored and makes no patch. If a snapshot is
missing, it proceeds only when all three ownership fields are already absent;
remaining adoption metadata with no snapshot fails closed because the original
metadata cannot be reconstructed safely.

Snapshot paths are canonicalized beneath the snapshot's `original/` directory.
Traversal, sibling-prefix paths, and symlink escapes are rejected.

If a failed Helm release record remains after metadata restoration, do not use
`helm uninstall`: it can delete adopted resources. Instead, inspect the
Helm-storage Secret or ConfigMap shown above and, only after review, remove
that release-storage object directly. This removes Helm's failed history
record, not the live application resources.

Helm 4 establishes adopted releases with server-side apply. The adoption
script uses `--server-side=true --force-conflicts` only for the one-time
release establishment after structural parity, exact inventory, and ownership
checks pass. It never uses `--force-replace`, `--force`, or
`--take-ownership`; manual metadata snapshots retain deterministic preflight
and metadata-only rollback. Normal rollout upgrades intentionally do not use
the conflict-transfer flag.

### One-time replica ownership normalization

Historical `kubectl scale` operations can retain `.spec.replicas` ownership
after adoption. GKE staging instead treats replicas as operational state:
`replicasManagedExternally: true` omits `spec.replicas` from the server,
frontend, and worker Deployment manifests. The desired steady-state values
remain in the staging values files and
[`gke-helm-rollout.sh`](../../scripts/gke-helm-rollout.sh) manages the scale
subresource with ordinary merge patches under the `coyote-rollout` field
manager. Normal Helm upgrades therefore do not modify replicas or use
`--force-conflicts`.

After both releases are deployed and verified, first inspect the reviewed
one-time normalization without mutating the cluster:

```sh
make gke-helm-normalize-ownership-dry-run
```

The normalization verifies that both releases are deployed and that strict
render/live adoption parity holds. It reads the configured desired counts,
patches the server, frontend, and worker `/scale` subresources with
`coyote-rollout`, then performs ordinary Helm upgrades whose rendered
Deployments omit `spec.replicas`. This makes Helm relinquish the field without
deleting or replacing any Deployment. It verifies that Helm does not own
replicas on the main Deployment resource and that all three live replica
counts match their configured desired values before checking Deployment health.
Historical scale-subresource field managers are informational only: ordinary
merge patches do not use SSA conflict semantics, so their presence does not
block normalization.

Run the mutating command only after reviewing a successful dry run:

```sh
make gke-helm-normalize-ownership
```

This is a one-time migration operation, not a normal rollout. The normal
[`gke-helm-rollout.sh`](../../scripts/gke-helm-rollout.sh) sequence uses
`coyote-rollout` merge patches on the `/scale` subresource to scale all
database consumers to zero, waits for each Deployment to finish scaling down,
runs the migration Job, then restores the configured counts. Its Helm upgrades
omit `spec.replicas`; it does not use Helm replica overrides or
`--force-conflicts`.

Verify a completed adoption without mutation:

```sh
GKE_HELM_ADOPT_VERIFY_ONLY=true scripts/gke-helm-adopt.sh
helm status coyote-ci -n coyote-ci
helm status coyote-ci-worker -n coyote-ci-staging
helm get manifest coyote-ci -n coyote-ci
helm get manifest coyote-ci-worker -n coyote-ci-staging
GKE_HELM_DRY_RUN=true scripts/gke-helm-rollout.sh
```

Adoption establishes release ownership only. A normal rollout is a separate,
intentional operation: it scales consumers down and runs the migration Job, so
it must not be used as part of ownership adoption.

The charts preserve GKE Workload Identity, Secret Manager CSI, Gateway API,
and GKE HealthCheckPolicy integration behind values switches. Disabling those
switches only omits their Kubernetes resources; it does not make a portable
deployment profile bootable.
