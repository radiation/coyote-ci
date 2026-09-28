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
The migration Job is intentionally excluded from both normal chart renders;
its explicit blocking rollout semantics are a later slice.

The charts preserve GKE Workload Identity, Secret Manager CSI, Gateway API,
and GKE HealthCheckPolicy integration behind values switches. Disabling those
switches only omits their Kubernetes resources; it does not make a portable
deployment profile bootable.
