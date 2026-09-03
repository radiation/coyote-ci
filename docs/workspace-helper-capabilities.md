# Workspace Helper Capabilities

Kubernetes workspace helpers will exchange a projected ServiceAccount token for a short-lived Coyote capability bound to one execution job, Pod UID, and helper role. The exchange validates the projected token with Kubernetes TokenReview for a role-specific audience and confirms the bound Pod has the matching Coyote execution-job label.

The capability is an HMAC-signed, stateless server token. It is not an execution claim token and is not a Kubernetes controller credential. Prepare and publish use different roles and projected-token audiences. Future Pod wiring must mount the projected identity and issued capability only into the trusted helper container; the build container must receive neither.

The server enables this exchange only when `COYOTE_WORKSPACE_HELPER_ENABLED=true`, and then requires `COYOTE_WORKSPACE_HELPER_CAPABILITY_SECRET` (at least 32 bytes). `COYOTE_WORKSPACE_HELPER_KUBECONFIG` is the optional server-side Kubernetes identity-verification configuration; in-cluster configuration remains supported. This is independent of `WORKER_EXECUTION_BACKEND` because server and worker processes may be deployed separately.

Current Kubernetes workload identity verification assumes the Coyote server can verify identities against the relevant Kubernetes cluster. Multi-cluster identity routing is a future concern and is not part of this slice. No helper commands, workspace transport, or Pod wiring are implemented by this boundary.