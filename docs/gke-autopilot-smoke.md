# GKE Autopilot Smoke

This is the smallest cloud deployment path for the Kubernetes execution controller. It assumes the existing `coyote-ci-autopilot` cluster in `us-central1`, namespace `coyote-ci`, and the temporary bootstrap image `us-central1-docker.pkg.dev/bryanchoate/coyote-ci/coyote-worker:gke-smoke`. The image may later be replaced by Coyote-managed publication and deployment.

Build and push the bootstrap image as a portable OCI image index before deployment:

```sh
make gke-image-build
docker buildx imagetools inspect us-central1-docker.pkg.dev/bryanchoate/coyote-ci/coyote-worker:gke-smoke
```

The build publishes `linux/amd64` and `linux/arm64`; do not add an Arm node selector to accommodate a host-architecture-only image.

## Prerequisites

`kubectl` must target the existing GKE cluster. The Coyote server must be externally reachable by Pods and must use the VM verifier bridge described below. Its filesystem workspace revision store remains server-side for this smoke; execution Pods never mount it.

The worker Kubernetes ServiceAccount `coyote-kubernetes-worker` is externally bootstrapped with its Workload Identity annotation and Cloud SQL Client IAM access. The namespace must also contain the externally bootstrapped Secret Manager secret `coyote-database-url` and `SecretProviderClass/coyote-database-secrets`. The worker Deployment mounts that provider class read-only at `/var/run/secrets/coyote`, reads `DATABASE_URL_FILE=/var/run/secrets/coyote/database-url`, and connects to Cloud SQL only through its loopback Cloud SQL Auth Proxy sidecar at `127.0.0.1:5432` for `bryanchoate:us-central1:bryanchoate-postgres`.

## VM Workspace Verifier Bridge

Until `coyote-server` runs in GKE, the VM-hosted server uses a dedicated `coyote-workspace-verifier` Kubernetes identity to verify helper tokens. It has only cluster-scoped `create` on `authentication.k8s.io/tokenreviews` and namespace-scoped `get` on Pods in `coyote-ci`. It is not used by the worker controller, workspace helpers, build Pods, or any other Coyote process.

Apply the source-controlled verifier RBAC, then generate and install its standalone kubeconfig on the VM. The manifest deliberately creates a manually managed, long-lived ServiceAccount token Secret because the VM server needs a noninteractive credential across restarts. Its narrow RBAC is the compensating control; rotate by deleting and recreating the Secret, then regenerate the kubeconfig.

```sh
kubectl apply -f deploy/kubernetes/gke/workspace-verifier.yaml
GKE_VERIFIER_KUBECONFIG_PATH=/opt/coyote-ci/gke-verifier-kubeconfig \
	bash scripts/gke-verifier-kubeconfig.sh
chmod 600 /opt/coyote-ci/gke-verifier-kubeconfig
```

The script derives only the current cluster endpoint and CA data, writes a new standalone static-token kubeconfig, and never copies a personal kubeconfig or prints the token. It requires `kubectl` to target `coyote-ci-autopilot`; the server container does not require `gke-gcloud-auth-plugin`.

Set these values in the VM `.env.prod` file. Do not commit the capability secret or kubeconfig.

```sh
COYOTE_WORKSPACE_HELPER_ENABLED=true
COYOTE_WORKSPACE_HELPER_KUBECONFIG_HOST_PATH=/opt/coyote-ci/gke-verifier-kubeconfig
COYOTE_WORKSPACE_HELPER_SERVICE_ACCOUNT=coyote-workspace-helper
COYOTE_WORKSPACE_HELPER_CAPABILITY_SECRET=<at-least-32-byte-secret>
COYOTE_WORKSPACE_REVISION_STORAGE_ROOT=/var/lib/coyote-workspaces
```

`docker-compose.prod.yml` mounts the generated kubeconfig read-only at `/etc/coyote/gke-verifier-kubeconfig` and stores revisions in the persistent `workspace_revision_data` Docker volume, rather than the container filesystem. After the directory and VM environment are configured, restart `coyote-server` using the production deployment flow. Confirm the route is registered without submitting a token:

```sh
curl -i -X POST https://coyote-ci.bryanchoate.com/api/internal/workspace-helper/capabilities
```

After enablement, the endpoint must no longer return `404`; `400`, `401`, or `403` are expected for this unauthenticated probe. Remove this VM bridge when `coyote-server` moves into GKE and can use in-cluster configuration.

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

`gke-deploy` verifies the externally bootstrapped worker ServiceAccount, its expected Workload Identity annotation, and `SecretProviderClass/coyote-database-secrets`; it then applies [the GKE worker manifest](../deploy/kubernetes/gke/worker.yaml), writes only the non-secret helper URL ConfigMap, waits for the controller rollout, and prints its image. `gke-smoke` verifies the mounted database URL path and Cloud SQL proxy sidecar without printing secret contents, then submits a checkout-free one-step Alpine pipeline through Coyote. It waits through initial Autopilot Pending/capacity provisioning, then verifies the deterministic `coyote-exec-<execution-id>` Job, assigned node, terminal build Pod, durable build/step success, and persisted `GKE_AUTOPILOT_SMOKE_OK` log.

The controller Role is namespace-scoped to Jobs and Pods. The helper Role is namespace-scoped to reading its own Pod status. Build containers retain `automountServiceAccountToken: false` and receive only `/workspace` plus pipeline-configured environment; they are not bound to a Google service account and receive no database, helper, Kubernetes API, or workspace-store credentials.

Inspect admission/defaulted requests after a run instead of overriding Autopilot defaults prematurely:

```sh
kubectl -n coyote-ci get pod -o wide
kubectl -n coyote-ci describe pod <worker-or-execution-pod>
```

On failure the smoke prints nodes, Pods, Jobs, events, controller logs, relevant Pod details, Coyote build state, step state, and persisted logs. To clean up application workloads only, delete the deployment and Coyote-managed Jobs; do not delete the cluster.