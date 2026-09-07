# GKE Autopilot Smoke

This is the smallest cloud deployment path for the Kubernetes execution controller. It assumes the existing `coyote-ci-autopilot` cluster in `us-central1`, namespace `coyote-ci`, and the temporary bootstrap image `us-central1-docker.pkg.dev/bryanchoate/coyote-ci/coyote-worker:gke-smoke`. The image may later be replaced by Coyote-managed publication and deployment.

Build and push the bootstrap image as a portable OCI image index before deployment:

```sh
bash scripts/gke-image-build.sh
docker buildx imagetools inspect us-central1-docker.pkg.dev/bryanchoate/coyote-ci/coyote-worker:gke-smoke
```

The build publishes `linux/amd64` and `linux/arm64`; do not add an Arm node selector to accommodate a host-architecture-only image.

## Prerequisites

`kubectl` must target the existing GKE cluster. The Coyote server must be externally reachable by Pods, and must be configured with `COYOTE_WORKSPACE_HELPER_ENABLED=true`, a GKE-capable `COYOTE_WORKSPACE_HELPER_KUBECONFIG`, `COYOTE_WORKSPACE_HELPER_SERVICE_ACCOUNT=coyote-workspace-helper`, and its existing non-secret workspace-helper capability secret. Its filesystem workspace revision store remains server-side for this smoke; execution Pods never mount it.

Create the controller database secret once, without committing it:

```sh
kubectl -n coyote-ci create secret generic coyote-gke-worker \
  --from-literal=DATABASE_URL="$DATABASE_URL"
```

Set the externally reachable server URL. It must not be `localhost`, `host.docker.internal`, or a kind-only service address:

```sh
export COYOTE_INTERNAL_API_URL=https://coyote.example.com
export API_URL="$COYOTE_INTERNAL_API_URL"
```

## Deploy And Verify

```sh
make gke-deploy
make gke-smoke
```

`gke-deploy` applies [the GKE worker manifest](../deploy/kubernetes/gke/worker.yaml), writes only the non-secret helper URL ConfigMap, waits for the controller rollout, and prints its image. `gke-smoke` submits a checkout-free one-step Alpine pipeline through Coyote. It waits through initial Autopilot Pending/capacity provisioning, then verifies the deterministic `coyote-exec-<execution-id>` Job, assigned node, terminal build Pod, durable build/step success, and persisted `GKE_AUTOPILOT_SMOKE_OK` log.

The controller Role is namespace-scoped to Jobs and Pods. The helper Role is namespace-scoped to reading its own Pod status. Build containers retain `automountServiceAccountToken: false` and receive only `/workspace` plus pipeline-configured environment; they are not bound to a Google service account and receive no database, helper, Kubernetes API, or workspace-store credentials.

Inspect admission/defaulted requests after a run instead of overriding Autopilot defaults prematurely:

```sh
kubectl -n coyote-ci get pod -o wide
kubectl -n coyote-ci describe pod <worker-or-execution-pod>
```

On failure the smoke prints nodes, Pods, Jobs, events, controller logs, relevant Pod details, Coyote build state, step state, and persisted logs. To clean up application workloads only, delete the deployment and Coyote-managed Jobs; do not delete the cluster.