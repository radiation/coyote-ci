# Workspace Helper Capabilities

Kubernetes workspace helpers will exchange a projected ServiceAccount token for a short-lived Coyote capability bound to one execution job, Pod UID, and helper role. The exchange validates the projected token with Kubernetes TokenReview for a role-specific audience and confirms the bound Pod has the matching Coyote execution-job label.

The capability is an HMAC-signed, stateless server token. It is not an execution claim token and is not a Kubernetes controller credential. Prepare and publish use different roles and projected-token audiences. Future Pod wiring must mount the projected identity and issued capability only into the trusted helper container; the build container must receive neither.

The server enables this exchange only when `COYOTE_WORKSPACE_HELPER_ENABLED=true`, and then requires `COYOTE_WORKSPACE_HELPER_CAPABILITY_SECRET` (at least 32 bytes). `COYOTE_WORKSPACE_HELPER_KUBECONFIG` is the optional server-side Kubernetes identity-verification configuration; in-cluster configuration remains supported. This is independent of `WORKER_EXECUTION_BACKEND` because server and worker processes may be deployed separately.

When workspace helper support is enabled, the server also requires `WORKSPACE_REVISION_STORAGE_ROOT`. This is intentional for the current single prepare capability: source and predecessor inputs share one trusted endpoint, and predecessor preparation requires the authoritative revision archive store. Separating source-only prepare availability from predecessor storage is deferred until there is a concrete deployment need.

Current Kubernetes workload identity verification assumes the Coyote server can verify identities against the relevant Kubernetes cluster. Multi-cluster identity routing is a future concern and is not part of this slice.

## Prepare Transport

`coyote-worker workspace prepare` exchanges its projected identity for the `prepare` capability, then calls the internal prepare endpoint with only its execution-job ID and Pod UID. The server resolves the workspace input from the durable execution-job plan: it prepares an exact pinned source checkout with server-owned SCM credentials, or opens the authoritative predecessor revision archive. The helper never receives repository URLs, SCM credentials, storage keys, database configuration, or Kubernetes client configuration.

The endpoint returns a gzip archive with its SHA-256 `Content-Digest` and byte length. The helper verifies both values and safely extracts into a staging directory before atomically promoting the requested local workspace destination. Unsafe archive entries, corrupt streams, integrity failures, and existing destinations fail without promoting a workspace.

Kubernetes controller Pod/Job wiring and workspace publication remain deferred. Fan-in workspace inputs are also intentionally unsupported by this transport.