# External Workspace Verifier

Current GKE installation and upgrades use the [unified Helm chart](../deploy/helm/README.md#gke-operations).
The Helm-managed server uses in-cluster credentials and does not need this kubeconfig.

The retained external-server consumer is `docker-compose.prod.yml`, which mounts
`COYOTE_WORKSPACE_HELPER_KUBECONFIG_HOST_PATH`. Its supporting files are
`deploy/kubernetes/gke/workspace-verifier.yaml` and `scripts/gke-verifier-kubeconfig.sh`.
These are not a second GKE deployment workflow. Do not remove or rotate an external
consumer's credentials as part of Helm operations.

The external server uses a dedicated `coyote-workspace-verifier` Kubernetes identity
with only cluster-scoped `create` on TokenReviews and namespace-scoped Pod `get`
in `coyote-ci`. It is not used by workers, helpers, or build Pods. This fixed
namespace manifest is not the verifier for the unified installation's separate
execution namespace.

Apply the source-controlled verifier RBAC, then generate and install its standalone kubeconfig on the VM. The manifest deliberately creates a manually managed, long-lived ServiceAccount token Secret because the VM server needs a noninteractive credential across restarts. Its narrow RBAC is the compensating control; rotate by deleting and recreating the Secret, then regenerate the kubeconfig.

```sh
kubectl apply -f deploy/kubernetes/gke/workspace-verifier.yaml
GKE_VERIFIER_KUBECONFIG_PATH=/opt/coyote-ci/gke-verifier-kubeconfig \
	bash scripts/gke-verifier-kubeconfig.sh
chmod 600 /opt/coyote-ci/gke-verifier-kubeconfig
```

The script derives only the current cluster endpoint and CA data, writes a new standalone static-token kubeconfig, and never copies a personal kubeconfig or prints the token. Verify the intended cluster context before invoking it; the server container does not require `gke-gcloud-auth-plugin`.

Set these values in the VM `.env.prod` file. Do not commit the capability secret or kubeconfig.

```sh
COYOTE_WORKSPACE_HELPER_ENABLED=true
COYOTE_KUBERNETES_ARTIFACT_HELPER_ENABLED=true
COYOTE_WORKSPACE_HELPER_KUBECONFIG_HOST_PATH=/opt/coyote-ci/gke-verifier-kubeconfig
COYOTE_WORKSPACE_HELPER_SERVICE_ACCOUNT=coyote-workspace-helper
COYOTE_WORKSPACE_HELPER_CAPABILITY_SECRET=<at-least-32-byte-secret>
COYOTE_WORKSPACE_REVISION_STORAGE_ROOT=/var/lib/coyote-workspaces
```

`docker-compose.prod.yml` mounts the generated kubeconfig read-only at `/etc/coyote/gke-verifier-kubeconfig` and stores revisions in the persistent `workspace_revision_data` Docker volume, rather than the container filesystem. After the directory and VM environment are configured, restart `coyote-server` using the production deployment flow. Confirm the route is registered without submitting a token:

```sh
curl -i -X POST https://coyote-ci.bryanchoate.com/api/internal/workspace-helper/capabilities
```

After enablement, the endpoint must no longer return `404`; `400`, `401`, or `403`
are expected for this unauthenticated probe. Retire these credentials only after
all external consumers have been removed or switched to in-cluster configuration.