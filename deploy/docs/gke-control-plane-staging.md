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
export CONTROL_PLANE_PUBLIC_URL=https://<temporary-staging-hostname>
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

## Slice D temporary public hostname

Slice D exposes only the staging GKE control plane through a temporary HTTPS
hostname. It does not modify `coyote-ci.bryanchoate.com`, production DNS, the
VM/Caddy deployment, the production database, or the active
`coyote-kubernetes-worker`.

Set the hostname explicitly; the example below uses the temporary hostname for
this validation:

```sh
export GKE_TEMPORARY_HOSTNAME=k8s.coyote-ci.bryanchoate.com
export CONTROL_PLANE_PUBLIC_URL="https://${GKE_TEMPORARY_HOSTNAME}"
export CONTROL_PLANE_OIDC_REDIRECT_URL="https://${GKE_TEMPORARY_HOSTNAME}/auth/callback"
```

Before deploying Gateway resources, complete these external GCP and DNS steps:

1. Enable Certificate Manager:

   ```sh
   gcloud services enable certificatemanager.googleapis.com --project="$GCP_PROJECT"
   ```

2. Reserve a new global external IPv4 address. Do not reuse the VM or any
   regional address:

   ```sh
   export GKE_GATEWAY_ADDRESS_NAME=coyote-staging-gateway-ip
   gcloud compute addresses create "$GKE_GATEWAY_ADDRESS_NAME" \
     --project="$GCP_PROJECT" --global --ip-version=IPV4
   export GKE_GATEWAY_IP="$(gcloud compute addresses describe "$GKE_GATEWAY_ADDRESS_NAME" \
     --project="$GCP_PROJECT" --global --format='value(address)')"
   ```

3. Create a Certificate Manager DNS authorization, add the emitted CNAME to
   the existing `bryanchoate.com.` Cloud DNS zone, then create the
   Google-managed certificate and certificate map:

   ```sh
   export GKE_CERTIFICATE_NAME=coyote-staging-gateway-cert
   export GKE_CERTIFICATE_MAP=coyote-staging-gateway-map
   gcloud certificate-manager dns-authorizations create coyote-staging-gateway \
     --project="$GCP_PROJECT" --domain="$GKE_TEMPORARY_HOSTNAME"
   gcloud certificate-manager dns-authorizations describe coyote-staging-gateway \
     --project="$GCP_PROJECT"
   # Add the returned dnsResourceRecord.name/type/data to the bryanchoate-com zone.
   gcloud certificate-manager certificates create "$GKE_CERTIFICATE_NAME" \
     --project="$GCP_PROJECT" --domains="$GKE_TEMPORARY_HOSTNAME" \
     --dns-authorizations=coyote-staging-gateway
   gcloud certificate-manager maps create "$GKE_CERTIFICATE_MAP" --project="$GCP_PROJECT"
   gcloud certificate-manager maps entries create coyote-staging-gateway-entry \
     --project="$GCP_PROJECT" --map="$GKE_CERTIFICATE_MAP" \
     --hostname="$GKE_TEMPORARY_HOSTNAME" --certificates="$GKE_CERTIFICATE_NAME"
   ```

4. Create an A record in the `bryanchoate-com` managed zone pointing
   `$GKE_TEMPORARY_HOSTNAME` to `$GKE_GATEWAY_IP`. Do not alter the production
   hostname record.
5. Register the exact OIDC callback
   `https://$GKE_TEMPORARY_HOSTNAME/auth/callback` with the staging OIDC
   client before signing in.

Deploy the staging control plane with its public URL only after the callback
is registered:

```sh
make gke-control-plane-deploy
```

Deploy the Gateway after the Certificate Map is active:

```sh
make gke-gateway-deploy
```

The Gateway routes `/api`, `/auth`, `/healthz`, `/readyz`, and `/swagger` to
`coyote-server`; all other paths go to `coyote-frontend`. HTTP redirects to
HTTPS. TLS terminates at the Gateway; the ClusterIP Services remain unchanged.

## Slice D isolated staging worker

The Slice D worker runs in `coyote-ci-staging`, uses
`coyote-staging-database-url`, and creates execution Jobs only in that
namespace. It must never use the production worker database secret or share
the `coyote-ci` execution namespace.

Create a dedicated Google service account and bind it to the staging Kubernetes
service account. Grant only the permissions needed by the existing worker:

```sh
export STAGING_WORKER_GSA="coyote-gke-staging-worker@${GCP_PROJECT}.iam.gserviceaccount.com"
gcloud iam service-accounts create coyote-gke-staging-worker --project="$GCP_PROJECT"
gcloud iam service-accounts add-iam-policy-binding "$STAGING_WORKER_GSA" \
  --project="$GCP_PROJECT" \
  --role=roles/iam.workloadIdentityUser \
  --member="serviceAccount:${GCP_PROJECT}.svc.id.goog[coyote-ci-staging/coyote-kubernetes-worker-staging]"
gcloud projects add-iam-policy-binding "$GCP_PROJECT" \
  --member="serviceAccount:${STAGING_WORKER_GSA}" --role=roles/cloudsql.client
gcloud secrets add-iam-policy-binding coyote-staging-database-url --project="$GCP_PROJECT" \
  --member="serviceAccount:${STAGING_WORKER_GSA}" --role=roles/secretmanager.secretAccessor
gcloud storage buckets add-iam-policy-binding "gs://${ARTIFACT_BUCKET}" \
  --member="serviceAccount:${STAGING_WORKER_GSA}" --role=roles/storage.objectAdmin
```

The manifest adds only Kubernetes-side configuration and RBAC; it does not
create IAM bindings. Supply a digest-pinned worker image and explicit GSA:

```sh
export COYOTE_STAGING_WORKER_GSA_EMAIL="$STAGING_WORKER_GSA"
export COYOTE_STAGING_WORKER_IMAGE=us-central1-docker.pkg.dev/.../coyote-worker@sha256:...
export ARTIFACT_GCS_BUCKET="$ARTIFACT_BUCKET"
make gke-staging-worker-deploy
```

The staging worker injects
`http://coyote-server.coyote-ci.svc.cluster.local:8080` into workspace, cache,
and artifact helpers. This is authenticated by projected workload identity and
capability tokens; it intentionally does not traverse the public Gateway.

For public smoke automation, sign in through the temporary hostname and create
a staging-only token with `build:read`, `build:logs`, and `build:run`:

```sh
export COYOTE_STAGING_SMOKE_API_TOKEN=<staging-token>
make gke-public-e2e-smoke
```

Manual browser validation remains required: complete OIDC sign-in at the
temporary hostname, verify the secure session cookie, and verify authenticated
frontend API requests. The smoke intentionally does not weaken OIDC.

Production certificate/DNS cutover remains out of scope for Slice D.
