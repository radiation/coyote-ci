# GKE Control Plane Staging

This deployment is an additive, private control-plane copy in the existing
`coyote-ci` namespace. It does not create an Ingress, alter
`coyote-ci.bryanchoate.com`, modify the VM deployment, or repoint the existing
Kubernetes worker. It must use a dedicated staging database on the existing
Cloud SQL instance; never point this deployment at the VM production database.

## Required secrets

Create these Secret Manager secrets outside source control:

- `coyote-staging-database-url`: a staging-database DSN using
  `127.0.0.1:5432` as the host, for the Cloud SQL Auth Proxy sidecar.
- `coyote-workspace-helper-capability-secret`: the capability secret shared
  with workspace helper execution Pods for the staging control plane.
- `coyote-staging-oidc-client-secret`: the OIDC client secret for this staging
  deployment.
- `coyote-staging-session-secret`: a high-entropy, independent session-signing
  secret for this staging deployment.

The server `SecretProviderClass/coyote-server-secrets` mounts all four server
files. The migration `SecretProviderClass/coyote-migrate-secrets` mounts only
the database URL, so the migration workload cannot request workspace-helper or
OIDC secrets. The database URL is read by the server and migration image
through `DATABASE_URL_FILE`. The workspace-helper secret is read through
`COYOTE_WORKSPACE_HELPER_CAPABILITY_SECRET_FILE`; OIDC and session secrets are
read through `OIDC_CLIENT_SECRET_FILE` and `SESSION_SECRET_FILE`. None are
placed in a manifest or ConfigMap.

The staging control plane requires `AUTH_MODE=oidc`. It does not support
`disabled` mode because untrusted execution Pods share the namespace and can
resolve the ClusterIP Service. `header` mode is also rejected because its
caller-controlled headers are not an authentication boundary for workloads.
Workspace-helper routes retain their independent capability authorization.

## External IAM setup

Set these variables before running the commands:

```sh
export GCP_PROJECT=bryanchoate
export NAMESPACE=coyote-ci
export SERVER_GSA=coyote-gke-server@${GCP_PROJECT}.iam.gserviceaccount.com
export MIGRATE_GSA=coyote-gke-migrate@${GCP_PROJECT}.iam.gserviceaccount.com
export ARTIFACT_BUCKET=<artifact-bucket>
export CACHE_BUCKET=<cache-bucket>
export WORKSPACE_REVISION_BUCKET=<workspace-revision-bucket>
```

Create the dedicated Google service accounts and allow their Kubernetes service
accounts to impersonate them:

```sh
gcloud iam service-accounts create coyote-gke-server --project="$GCP_PROJECT"
gcloud iam service-accounts create coyote-gke-migrate --project="$GCP_PROJECT"

gcloud iam service-accounts add-iam-policy-binding "$SERVER_GSA" \
  --project="$GCP_PROJECT" \
  --role=roles/iam.workloadIdentityUser \
  --member="serviceAccount:${GCP_PROJECT}.svc.id.goog[${NAMESPACE}/coyote-server]"
gcloud iam service-accounts add-iam-policy-binding "$MIGRATE_GSA" \
  --project="$GCP_PROJECT" \
  --role=roles/iam.workloadIdentityUser \
  --member="serviceAccount:${GCP_PROJECT}.svc.id.goog[${NAMESPACE}/coyote-migrate]"
```

Grant the server only the runtime access it uses:

```sh
gcloud projects add-iam-policy-binding "$GCP_PROJECT" \
  --member="serviceAccount:${SERVER_GSA}" \
  --role=roles/cloudsql.client
gcloud storage buckets add-iam-policy-binding "gs://${ARTIFACT_BUCKET}" \
  --member="serviceAccount:${SERVER_GSA}" --role=roles/storage.objectAdmin
gcloud storage buckets add-iam-policy-binding "gs://${CACHE_BUCKET}" \
  --member="serviceAccount:${SERVER_GSA}" --role=roles/storage.objectAdmin
gcloud storage buckets add-iam-policy-binding "gs://${WORKSPACE_REVISION_BUCKET}" \
  --member="serviceAccount:${SERVER_GSA}" --role=roles/storage.objectAdmin
```

Grant Secret Manager access at the secret resource, not project-wide:

```sh
for secret in coyote-staging-database-url coyote-workspace-helper-capability-secret \
  coyote-staging-oidc-client-secret coyote-staging-session-secret; do
  gcloud secrets add-iam-policy-binding "$secret" --project="$GCP_PROJECT" \
    --member="serviceAccount:${SERVER_GSA}" --role=roles/secretmanager.secretAccessor
done
gcloud secrets add-iam-policy-binding coyote-staging-database-url --project="$GCP_PROJECT" \
  --member="serviceAccount:${MIGRATE_GSA}" --role=roles/secretmanager.secretAccessor
gcloud projects add-iam-policy-binding "$GCP_PROJECT" \
  --member="serviceAccount:${MIGRATE_GSA}" --role=roles/cloudsql.client
```

The server intentionally has no Cloud Build or Artifact Registry IAM role.
Image pulls use the cluster/node image-pull path; server runtime code does not
invoke Cloud Build.

## Build, deploy, and private validation

Build and publish the three images. The command prints digest-pinned values:

```sh
make gke-control-plane-image-build
```

Export the printed `COYOTE_SERVER_IMAGE`, `COYOTE_MIGRATE_IMAGE`, and
`COYOTE_FRONTEND_IMAGE` values, then configure deployment-specific values:

```sh
export COYOTE_SERVER_GSA_EMAIL="$SERVER_GSA"
export COYOTE_MIGRATE_GSA_EMAIL="$MIGRATE_GSA"
export COYOTE_STAGING_DATABASE_URL_SECRET=coyote-staging-database-url
export ARTIFACT_GCS_BUCKET="$ARTIFACT_BUCKET"
export WORKER_CACHE_GCS_BUCKET="$CACHE_BUCKET"
export WORKSPACE_REVISION_GCS_BUCKET="$WORKSPACE_REVISION_BUCKET"
export CONTROL_PLANE_AUTH_MODE=oidc
export CONTROL_PLANE_OIDC_ISSUER_URL=https://issuer.example.com
export CONTROL_PLANE_OIDC_CLIENT_ID=coyote-ci-staging
export CONTROL_PLANE_OIDC_REDIRECT_URL=https://<temporary-staging-hostname>/auth/callback
export CONTROL_PLANE_BOOTSTRAP_ADMIN_EMAILS=admin@example.com
make gke-control-plane-deploy
make gke-control-plane-smoke
```

The deploy script validates rendered resources server-side, applies identity,
configuration, Services, and zero-replica Deployments, replaces any old
completed migration Job, waits for the new migration Job, then scales and waits
for the server before the frontend. The completed migration Job is retained for
staging diagnostics until the next deployment explicitly replaces it. A
migration failure stops the rollout.

The OIDC provider must register the configured temporary staging callback URL,
and `coyote-staging-oidc-client-secret` must contain its client secret before
deployment. `coyote-staging-session-secret` must contain an independent,
high-entropy random value. OIDC is required even for private, port-forward-only
validation so in-namespace untrusted workloads cannot impersonate an
administrator through the ClusterIP Service.

`gke-control-plane-smoke` uses temporary `kubectl port-forward` processes only.
It checks the migration Job, rollout state, digest-pinned images, in-cluster
server verifier configuration, Cloud SQL proxy readiness, GCS provider config,
server `/healthz`, server `/readyz`, frontend SPA serving, and frontend
same-origin `/api/readyz` routing.

## Sizing assumptions

The server starts with 1 CPU / 2 GiB maximum and 4 GiB ephemeral storage to
accommodate transient source checkouts and archive streaming. Its `/tmp`
`emptyDir` is explicitly capped at 4 GiB, matching that limit. The Cloud SQL
Auth Proxy starts at 250m CPU / 256 MiB and is capped at 500m / 512 MiB.
Frontend starts at 250m CPU / 256 MiB. These are conservative Autopilot
requests/limits to tune from observed memory and ephemeral-storage metrics;
they are not durable volumes.

## Slice D prerequisites

Before adding temporary public ingress/TLS, confirm the private smoke succeeds,
the staging database is isolated from production, and cluster support for the
native Job sidecar API used by the migration Cloud SQL Auth Proxy is available.
No DNS, TLS, or public ingress resource is created in this slice.
