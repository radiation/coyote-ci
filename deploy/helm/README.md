# Coyote CI Helm charts

These charts are the render-only Helm packaging for the proven GKE staging
architecture. They do not provision GCP infrastructure, create namespaces,
adopt existing resources, or run database migrations.

- `coyote-ci` owns steady-state control-plane resources in `coyote-ci`.
- `coyote-ci-worker` owns steady-state staging execution-worker resources in
  `coyote-ci-staging`.

Use the non-secret example values only for rendering and validation:

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

Slice 3 must still adopt the existing raw-managed resources before this
wrapper can be used against staging. It deliberately refuses to install or
adopt releases, and existing applications must be stopped before migration
because this repository does not assert that every Goose migration is
backward-compatible.

The charts preserve GKE Workload Identity, Secret Manager CSI, Gateway API,
and GKE HealthCheckPolicy integration behind values switches. Disabling those
switches only omits their Kubernetes resources; it does not make a portable
deployment profile bootable.
