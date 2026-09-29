# Coyote CI Helm charts

These charts are the render-only Helm packaging for the proven GKE staging
architecture. They do not provision GCP infrastructure, create namespaces, or
run database migrations.

- `coyote-ci` owns steady-state control-plane resources in `coyote-ci`.
- `coyote-ci-worker` owns steady-state staging execution-worker resources in
  `coyote-ci-staging`.

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

The raw manifests and deployment scripts remain the current deployment path.
The migration Job is excluded from ordinary chart renders and Helm release
ownership. It is rendered explicitly for a unique rollout run and applied by
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
pre-adoption metadata snapshots under `.gke-helm-adoption-state/`. If patching
or release establishment fails, it restores only the metadata changed by the
attempt and never runs `helm uninstall` or deletes workloads. To explicitly
restore a retained snapshot, use:

```sh
GKE_HELM_ADOPT_ROLLBACK_STATE=.gke-helm-adoption-state/<timestamp> \
  scripts/gke-helm-adopt.sh
```

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
