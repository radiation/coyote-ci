# Coyote CI Helm chart

The public `coyote-ci` chart supports external PostgreSQL and native Kubernetes Secrets without
cloud-provider dependencies. GKE integrations are explicit opt-ins. They do not
provision a database, identity provider, cloud infrastructure, or credentials.

- `coyote-ci` owns the control plane, optional MCP, automatic migration hooks,
  and optional Kubernetes worker in one release. Worker resources live in
  `executionNamespace`; release metadata stays in the control-plane namespace.
- GKE uses the same chart and one `coyote-ci` release. The maintained environment
  settings are in [`gke-values.yaml`](gke-values.yaml); images come from a resolved
  release overlay. Recovery evidence and historical packages are archived outside Git.

## Portable installation

Start with `examples/generic-values.yaml`. It contains placeholder image digests
and Secret references, never credentials. Supply real digest-pinned images,
an HTTPS OIDC issuer, client ID, callback/public URLs and bootstrap admin emails.
OIDC remains required; the chart does not disable authentication for local use.

Select the control-plane namespace using `helm --namespace`. Leave `namespace: ""`
to use the release namespace; a nonempty override must match it. Set
`executionNamespace` explicitly and provision it before enabling workers.
The chart never creates or deletes Namespace resources; `--create-namespace`
only handles the release namespace. Native Secrets must preexist in each
consumer namespace. Namespace sharing remains an explicit legacy exception.

### Worker configuration and isolation

Set `worker.enabled: true` to include execution resources (the chart default is
false; the generic example enables it). Configure `worker.replicas`, `resources`,
`maxInFlightJobs`, `nodeSelector`, `affinity`, and `tolerations` independently.
Scheduling applies to the controller Deployment, not build Pods. Fresh installs
should leave `worker.replicasManagedExternally: false`.

`serviceAccounts.worker` configures the controller account name and optional
identity annotations. `serviceAccounts.workspaceHelperName` configures the
separate helper account consistently on server and worker. The helper has no
cloud identity. Helper containers use projected tokens; build containers do not
receive those mounts. Execution accounts receive no RBAC in the control-plane
namespace when execution uses its own namespace. The server receives Pod get in
the execution namespace and cluster-scoped TokenReview create for verification.

The installer needs resource-management and RoleBinding permissions in both
namespaces and authority to install the narrow cluster RBAC. The worker needs
neither namespace-management nor cluster-wide permissions. Job-create permission
makes the controller trusted within its namespace; namespace separation is not
network isolation.

The worker/helper image is `images.worker`; no independent helper image is
accepted. The internal API URL is derived from the release namespace, with
`worker.internalAPIURL` available for routing overrides. Storage, Cloud SQL, and
GKE settings are shared. Cloud Build uses the optional `cloudBuild` settings.

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
the control-plane values. Set `worker.secrets.existingSecrets.databaseUrl.name`
and `.key` for the worker's namespace-local database Secret. For GKE CSI instead,
use `worker.secrets.project` and `worker.secrets.databaseUrl`. Do not configure
both native and CSI references. Volumes expose only selected keys. MCP and migrations
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
maintenance). It uses `Recreate` to avoid overlapping writers and attachment
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

Use the single Helm operation below with the desired application replicas.
The blocking migration hook completes before ordinary application resources
are created or updated. The Job does not mount the artifact PVC, so
`WaitForFirstConsumer` storage does not block migrations. In generic mode it
uses a native database Secret without a Cloud SQL proxy or service-account token.

Services are private ClusterIP endpoints. Supply your own ingress/TLS setup;
the existing `gateway` integration is GKE-specific and remains disabled by
default. Optional MCP needs a digest-pinned image, not a Google identity in
generic mode. Arbitrary service-account annotations can be configured through
`serviceAccounts.<name>.annotations` without assigning Kubernetes permissions.

Run `make helm-package-validate` for the consolidated structural checks and
package-integrity self-tests, including source/package equivalence and rejection
of secret-like archive paths. Helm and Ruby are required for these local checks;
Ruby parses rendered YAML only in `scripts/helm-schema-test.sh`. Use
`make helm-schema-test` for a focused structural check.
`bash scripts/kind-helm-smoke.sh` creates a
disposable Kind cluster, builds local OCI test images without pushing them,
installs PostgreSQL/native Secrets and an HTTPS OIDC discovery fixture, and checks
automatic installation, no-op upgrade, failed migration/retry, rollout ordering, worker
enable/disable, scoped RBAC, authenticated artifact downloads across a server
restart, and Secret/PVC/namespace retention after uninstall. It seeds one artifact and metadata
instead of duplicating the execution suite. The fixture is not an OIDC login
test; only the test server image includes its local CA. PostgreSQL
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
  --values deploy/helm/examples/gke-staging-control-plane-values.yaml \
  --values deploy/helm/examples/gke-staging-unified-worker-values.yaml
helm lint --strict deploy/helm/coyote-ci \
  --namespace coyote-ci \
  --values deploy/helm/examples/gke-production-control-plane-values.yaml \
  --values deploy/helm/examples/gke-production-unified-worker-values.yaml
make helm-package-validate
```

The examples cover isolated and shared execution namespaces. They are render
fixtures, not the current installation's values. New installations should use
isolated execution namespaces and Helm-managed replicas.

## First installation

For GKE integrations, provision external Cloud SQL, Secret Manager secrets,
Workload Identity and IAM bindings, GCS buckets, OIDC, and optional Gateway
address, certificate, and DNS resources. See the [Cloud SQL](../docs/gcp-cloud-sql-postgres.md)
and [GCS](../docs/gcp-gcs-artifacts.md) guides for prerequisites. The migration
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
digest-pinned server, frontend, worker, migration, and MCP images for V3 releases.

Use Helm-managed replicas for a fresh installation. For example,
`customer-values.yaml` can contain (filesystem storage requires server replicas 1):

```yaml
server:
  replicasManagedExternally: false
  replicas: 1

frontend:
  replicasManagedExternally: false
  replicas: 1

worker:
  enabled: true
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

The MCP image remains required; a GSA is required only with GKE Workload Identity.
Use Helm-managed replicas unless an external controller manages scaling. External replica management is
reserved for workflows that explicitly manage the Deployment scale
subresource.

Independent Coyote installations must use separate Kubernetes namespaces. The
TokenReview name helper prevents collisions for its cluster-scoped RBAC
objects, but the chart does not make every namespaced resource safe for
multiple Helm releases in one namespace.

### Install in one operation

Provision the external PostgreSQL database, namespace-local Secrets, image-pull
access, and network access before installing. With GKE CSI, provision the CSI
driver, secret versions, Workload Identity bindings and IAM first. Cloud SQL uses
a restartable init sidecar and requires Kubernetes 1.29+ with native sidecars
enabled. The chart does not provision these external prerequisites.

The image overlay must supply `compatibility.schema_target`, matching the exact
migration image. V3 release resolution supplies it and `compatibility.upgrade_from`.
For other overlays, supply these explicitly; the example target `57` is illustrative,
not a default for future images. Keep application replicas at their desired counts.

```sh
helm upgrade --install coyote-ci deploy/helm/coyote-ci \
  --namespace "$COYOTE_NAMESPACE" \
  --create-namespace \
  --values customer-values.yaml \
  --values coyote-release-images.yaml \
  --wait \
  --timeout 15m
```

Helm waits for the `pre-install,pre-upgrade` Job even without `--wait-for-jobs`.
Its ServiceAccount (weight -30) and optional SecretProviderClass (-20) are earlier
hooks; the Job is weight -10. Deployments remain ordinary resources. The runner
validates the image target and Goose history, and serializes database writers
with a PostgreSQL session advisory lock. Fresh databases migrate; current ones
succeed without rerunning SQL. Existing databases with pending changes require
the upgrade approvals below. The supported Make/Compose/image entry points use
the same runner; a raw Goose CLI or other SQL writer does not participate.

`migration.activeDeadlineSeconds` defaults to 600 (minimum 60). The runner stops
30 seconds earlier; lock acquisition is bounded to approximately 120 seconds
and by the runner deadline. Use a Helm timeout longer than the Job deadline and
application startup; 15 minutes is the default recommendation. Avoid `--atomic`,
`--cleanup-on-fail`, and `--no-hooks` in this workflow. None can undo database changes.

Hooks retain success and failure evidence without a TTL. Job names include the
release revision; retries with the same revision can replace that Job only after
it is terminal. Earlier hook identities are replaced on the next operation, not
deleted immediately after success. Helm uninstall does not manage hook cleanup;
archive and remove old Jobs and unused hook identities deliberately. See the
[recovery runbook](../docs/database-migrations.md#failure-and-retry) before retrying.
The installer needs ConfigMap get and Job list permissions for preflight checks.
Serialize Helm operations: the active-Job check is a guard, not a distributed
deployment lock. Offline `helm template` cannot establish live source history.

### Worker lifecycle

No second Helm release is needed. Set `worker.enabled` in the complete desired
values and upgrade `coyote-ci`. Before disabling workers or uninstalling, stop new
execution and drain active Jobs. Disabling the worker removes its Deployment,
accounts and RBAC; it does not delete runtime-created Jobs or their namespace.
Changing `executionNamespace` or resource names changes identities and requires
a deliberate drain/reconfiguration, not an ordinary upgrade.

### Upgrades

Every install/upgrade runs the hook, including same-schema upgrades. Goose state
is authoritative; application history comes from the control-plane ConfigMap's
release annotations, or the operator's verified `migration.sourceVersion` when
those annotations are missing. Conflicting source assertions, application
downgrades, and changed manifest digests for a recorded release version fail.
Fresh databases and current-schema retries do not require application history.

For pending changes on an existing database, choose one of these explicitly:

- **Online:** release notes/tests must certify backward compatibility with all
  running consumers. Set `migration.onlineUpgradeApproved: true`. A known source
  version within `compatibility.upgrade_from` and a target `release.version` are
  required. A version range alone is not evidence that DDL is online-safe.
- **Maintenance:** stop new work, drain active builds, back up, and quiesce all
  database consumers out of band before setting
  `migration.maintenanceAcknowledged: true`. A pre-upgrade hook runs before
  desired zero replicas are applied; setting replicas to zero in that same Helm
  command does not quiesce existing workloads. Restore only compatible consumers
  after migration succeeds. Maintenance can acknowledge unknown/unsupported
  application history, but cannot bypass schema-ahead or image-target checks.

Historical migrations include breaking changes and are not universally online-safe.
V3 `compatibility.maintenance` is a support-lifecycle label, not a downtime flag.
Helm rollback changes application resources only: it never runs Goose `down`.
Only roll back binaries certified compatible with the resulting schema; otherwise
use a forward fix or a separately planned database-and-artifact restore.

### GKE operations

Use the maintained environment values and the resolved immutable image overlay:

```sh
helm lint --strict deploy/helm/coyote-ci --namespace coyote-ci \
  --values deploy/helm/gke-values.yaml --values coyote-release-images.yaml
helm template coyote-ci deploy/helm/coyote-ci --namespace coyote-ci \
  --values deploy/helm/gke-values.yaml --values coyote-release-images.yaml
helm upgrade --install coyote-ci deploy/helm/coyote-ci --namespace coyote-ci \
  --values deploy/helm/gke-values.yaml --values coyote-release-images.yaml \
  --wait --timeout 15m
helm status coyote-ci --namespace coyote-ci
helm history coyote-ci --namespace coyote-ci
```

Review migration compatibility and approvals as described above before an upgrade.
Use complete desired values, not `--reuse-values`. GKE prerequisites are managed
outside Helm; disabling an integration does not migrate its data or cloud resources.
The checked-in resource names, including the TokenReview override and execution
namespace, are current identities and must not be casually renamed.

## Optional MCP deployment

The MCP Deployment is disabled by default and has no Gateway or public ingress
route. Enable it only after publishing the dedicated MCP image:

```sh
make gke-control-plane-image-build
# Export the emitted digest-pinned COYOTE_MCP_IMAGE value.
```

Set `mcp.enabled: true`, `images.mcp: <COYOTE_MCP_IMAGE>`, and a dedicated
`serviceAccounts.mcp.gsaEmail` in the environment-specific control-plane
values. Keep replicas Helm-managed. The MCP GSA needs Cloud SQL Client and read access only to the
database-url secret. It does not need worker, artifact-storage, or Kubernetes
API permissions. Its service-account token is mounted only because the Cloud
SQL Auth Proxy requires it for GKE Workload Identity; the MCP KSA receives no
Kubernetes RBAC. The chart mounts only that database secret and runs a local
Cloud SQL Auth Proxy sidecar. It currently reuses the control-plane database
account for compatibility; a future dedicated restricted database role can be
introduced through a separate MCP database-url secret.

MCP remains ClusterIP-only; add an authenticated TLS gateway integration only
after selecting a suitable private ingress policy and explicit allowed origins.

The automatic migration hook is included in ordinary chart renders:

```sh
helm template coyote-ci deploy/helm/coyote-ci \
  --namespace coyote-ci \
  --values deploy/helm/examples/gke-staging-control-plane-values.yaml \
  --show-only templates/migration-job.yaml
```
See the [migration runbook](../docs/database-migrations.md) for failure and retry handling.
