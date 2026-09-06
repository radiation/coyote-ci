# kind Kubernetes Smoke Test

This repository-owned kind environment verifies the initial Kubernetes execution backend against a real API server, scheduler, Job controller, and Pod lifecycle. kind is development/test infrastructure, not a Coyote runtime dependency. Kubernetes execution is not production-ready.

Prerequisites: Docker Desktop, `kind`, `kubectl`, `jq`, and `curl`.

```sh
make kind-up
make kind-load
make kind-smoke
make kind-workspace-smoke
make kind-down
```

The `coyote-ci` kind cluster has one control-plane and two worker nodes. `kind-up` starts the existing Compose Postgres/server dependencies and applies the namespace-scoped worker service account, Role, RoleBinding, and deployment. The worker runs in kind with in-cluster Kubernetes configuration; it reaches the Compose Postgres instance through Docker Desktop's `host.docker.internal` bridge. No kubeconfig or workspace-revision storage is mounted into the worker.

`kind-load` performs a normal local ARM64 Docker build and loads `coyote-ci-worker:kind` into kind. The worker image pull policy is `Never`, so no registry is required.

`kind-down` deletes the cluster and stops the Compose database, migration, and server services started for the harness. It preserves the normal Compose volume for subsequent local development.

`kind-smoke` keeps the checkout-free, single-step baseline. `kind-workspace-smoke` submits a normal two-step pipeline through the Coyote API: `generate` publishes its workspace revision on one kind worker node, and `consume` restores that revision on the other node before checking its generated file. The local-only worker configuration pins the first two step indices to the two kind worker nodes; public pipeline syntax does not expose Kubernetes node selection. It verifies the deterministic Coyote Jobs, helper lifecycle, independent `emptyDir` workspaces, distinct Pod nodes, persisted build/step state, persisted terminal logs, Coyote labels, no claim tokens in metadata, and `automountServiceAccountToken: false`. It also verifies the controller identity has only the declared namespace-scoped permissions and cannot read secrets.

Cancellation and restart/reclaim are intentionally deferred to a dedicated, non-brittle follow-up smoke slice. This harness does not add Helm, cloud Kubernetes deployment, workspace restore/publication helpers, cache, artifacts, or multi-step execution.