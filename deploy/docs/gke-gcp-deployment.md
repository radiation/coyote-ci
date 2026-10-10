# GKE on GCP Deployment (Pre-Helm)

This is the currently proven deployment path for Coyote CI on GKE and GCP. It
documents an isolated staging control plane and staging worker/execution path,
not a production DNS or VM cutover runbook.

This path is intentionally pre-Helm. Helm packaging, production topology,
high availability, multi-region operation, and production DNS/VM cutover are
separate follow-up work.

## Architecture and boundaries

```text
Internet
  -> GKE Gateway API / HTTPS
     -> coyote-frontend Service -> coyote-frontend Deployment
     -> coyote-server Service   -> coyote-server Deployment
                                      + Cloud SQL Auth Proxy sidecar

coyote-migrate Job + Cloud SQL Auth Proxy sidecar

coyote-kubernetes-worker-staging Deployment
  -> coyote-ci-staging execution Jobs
     -> workspace, cache, and artifact helpers
     -> coyote-server.coyote-ci.svc.cluster.local:8080

Cloud SQL: durable PostgreSQL state
GCS: artifacts, cache archives, workspace revisions, Cloud Build source archives
```

The control plane runs in `coyote-ci`:

- `coyote-server` is the API and control-plane Deployment.
- `coyote-frontend` is the frontend Deployment.
- `coyote-migrate` is an explicit blocking migration Job.

The isolated staging worker runs in `coyote-ci-staging`:

- `coyote-kubernetes-worker-staging` creates ephemeral execution Jobs only in
  `coyote-ci-staging`.
- It uses `coyote-staging-database-url`, never production database
  credentials.
- `coyote-workspace-helper` is the execution helper ServiceAccount in that
  namespace.

This namespace separation prevents staging and production-facing controllers
from observing or managing each other's Kubernetes Jobs. The old
production-facing worker path remains separate.

Ordinary build Pods do not receive general GCP credentials. Trusted
control-plane, worker, and helper flows own cloud integration; helper traffic
uses the internal ClusterIP URL
`http://coyote-server.coyote-ci.svc.cluster.local:8080` with existing
capability and workload-identity protections rather than traversing the public
Gateway. `image_build` steps are sent to Google Cloud Build; they do not run
Docker inside GKE.

## Prerequisites

Operators provision the GCP resources and IAM outside these scripts:

- GKE Autopilot with Workload Identity and Secret Manager CSI available.
- Cloud SQL for PostgreSQL.
- Artifact Registry for deployment and build images.
- GCS buckets for artifacts, cache archives, workspace revisions, and Cloud
  Build source staging.
- Secret Manager.
- Certificate Manager and Cloud DNS.
- A reserved global external IPv4 address.
- An OIDC/OAuth web client.

Enable the APIs required by those services before deployment, including
Kubernetes Engine, Compute Engine, Cloud SQL Admin, Artifact Registry, Cloud
Storage, Secret Manager, Certificate Manager, Cloud DNS, and Cloud Build.
The deployment scripts render and apply Kubernetes resources; they do not
create all of these external resources.

For supporting details, see:

- [Cloud SQL PostgreSQL](gcp-cloud-sql-postgres.md)
- [GCS artifacts](gcp-gcs-artifacts.md)
- [workspace-helper capabilities](../../docs/workspace-helper-capabilities.md)

## Identities, Workload Identity, and IAM

Bind each Kubernetes ServiceAccount (KSA) to its dedicated Google Service
Account (GSA) with Workload Identity. IAM binding creation remains an operator
prerequisite.

| Workload | KSA | Example GSA | Required access |
| --- | --- | --- | --- |
| Server | `coyote-ci/coyote-server` | `coyote-gke-server@<project>.iam.gserviceaccount.com` | Cloud SQL Client, required Secret Manager secrets, and GCS object access for artifacts, cache archives, and workspace revisions. |
| Migration | `coyote-ci/coyote-migrate` | `coyote-gke-migrate@<project>.iam.gserviceaccount.com` | Cloud SQL Client and only the database URL secret. |
| Staging worker | `coyote-ci-staging/coyote-kubernetes-worker-staging` | `coyote-gke-staging-worker@<project>.iam.gserviceaccount.com` | Cloud SQL Client, staging database secret, GCS access, Cloud Build submission, Artifact Registry read access, and authority to use the Cloud Build runtime GSA. |
| Execution helpers | `coyote-ci-staging/coyote-workspace-helper` | none | Projected Kubernetes identity and server-issued capabilities only. |

The server GSA needs GCS object write permission, including
`storage.objects.create`. Workspace revision publication otherwise fails with a
GCS 403 even if other storage reads succeed.

The staging worker GSA needs:

- `roles/cloudsql.client`;
- Secret Manager access to `coyote-staging-database-url`;
- appropriate GCS object access;
- `roles/cloudbuild.builds.editor`;
- `roles/artifactregistry.reader` on the configured Artifact Registry repository;
- `roles/iam.serviceAccountUser` on the configured Cloud Build runtime GSA,
  such as `coyote-cloud-build@<project>.iam.gserviceaccount.com`.

Kubernetes RBAC stays narrow: the staging worker can manage Jobs and inspect
Pods/logs only in `coyote-ci-staging`; the server gets Pod `get` in that
namespace to verify helpers.

## Secrets and control-plane configuration

Create these Secret Manager secrets outside source control:

- `coyote-staging-database-url`: staging DSN using `127.0.0.1:5432` through
  the Cloud SQL Auth Proxy sidecar.
- `coyote-workspace-helper-capability-secret`: server and helper capability
  authorization.
- `coyote-staging-oidc-client-secret`: OIDC client secret.
- `coyote-staging-session-secret`: independent high-entropy session secret.

The server CSI mount receives all four. The migration Job receives only the
database URL. The staging worker receives only the staging database URL. No
secret value is placed in a manifest or ConfigMap.

The control-plane deploy requires OIDC mode. Set:

```sh
export CONTROL_PLANE_AUTH_MODE=oidc
export CONTROL_PLANE_OIDC_ISSUER_URL=https://issuer.example.com
export CONTROL_PLANE_OIDC_CLIENT_ID=<client-id>
export CONTROL_PLANE_OIDC_REDIRECT_URL=https://<hostname>/auth/callback
export CONTROL_PLANE_PUBLIC_URL=https://<hostname>
export CONTROL_PLANE_BOOTSTRAP_ADMIN_EMAILS=admin@example.com
```

The exact `https://<hostname>/auth/callback` URI must be in the provider's
authorized redirect URIs. `CONTROL_PLANE_OIDC_REDIRECT_URL` must match it
exactly, and `CONTROL_PLANE_PUBLIC_URL` must use the same hostname. Secure,
`SameSite=lax` session cookies remain enabled.

## Build and deploy the control plane

Set common storage and identity inputs:

```sh
export GCP_PROJECT=<project>
export COYOTE_SERVER_GSA_EMAIL=coyote-gke-server@${GCP_PROJECT}.iam.gserviceaccount.com
export COYOTE_MIGRATE_GSA_EMAIL=coyote-gke-migrate@${GCP_PROJECT}.iam.gserviceaccount.com
export COYOTE_STAGING_DATABASE_URL_SECRET=coyote-staging-database-url
export ARTIFACT_GCS_BUCKET=<artifact-bucket>
export WORKER_CACHE_GCS_BUCKET=<cache-bucket>
export WORKSPACE_REVISION_GCS_BUCKET=<workspace-revision-bucket>
```

Before building, remove stale digest-valued deployment inputs. Docker build
tags cannot contain `@sha256:...`.

```sh
unset COYOTE_SERVER_IMAGE
unset COYOTE_MIGRATE_IMAGE
unset COYOTE_FRONTEND_IMAGE
make gke-control-plane-image-build
```

The command prints immutable values for `COYOTE_SERVER_IMAGE`,
`COYOTE_MIGRATE_IMAGE`, and `COYOTE_FRONTEND_IMAGE`. Export the printed values
before deployment.

```sh
make gke-control-plane-deploy
```

The deploy script renders and server-side validates resources, applies
zero-replica server/frontend Deployments, replaces and waits for
`coyote-migrate`, then scales and waits for the server and frontend. It also
verifies digest-pinned deployed images. A migration failure stops rollout.

## Gateway, TLS, and DNS

Set the temporary hostname and matching public URL:

```sh
export GKE_TEMPORARY_HOSTNAME=<hostname>
export CONTROL_PLANE_PUBLIC_URL="https://${GKE_TEMPORARY_HOSTNAME}"
export CONTROL_PLANE_OIDC_REDIRECT_URL="https://${GKE_TEMPORARY_HOSTNAME}/auth/callback"
export GKE_GATEWAY_ADDRESS_NAME=<reserved-global-address>
export GKE_GATEWAY_CERTIFICATE_MAP=<certificate-map>
```

Before Gateway deployment:

1. Reserve a global external IPv4 address.
2. Create a Certificate Manager DNS authorization and publish its DNS record.
3. Create the Google-managed certificate.
4. Create the Certificate Map and map entry.
5. Point the hostname A record to the reserved global IP.
6. Register the exact OIDC callback.

Then deploy:

```sh
make gke-gateway-deploy
```

The `gke-l7-global-external-managed` Gateway routes `/api`, `/auth`,
`/healthz`, `/readyz`, and `/swagger` to `coyote-server`; all other paths
route to `coyote-frontend`. HTTP port 80 redirects to HTTPS. The server health
check uses `/readyz`, the frontend health check uses `/`, and both Services
remain ClusterIP.

## Deploy the isolated staging worker

The worker image must be digest pinned and include `/app/worker`. Configure:

```sh
export COYOTE_STAGING_WORKER_GSA_EMAIL=coyote-gke-staging-worker@${GCP_PROJECT}.iam.gserviceaccount.com
export COYOTE_STAGING_WORKER_IMAGE=<worker-image@sha256:...>
export WORKER_KUBERNETES_MAX_IN_FLIGHT_JOBS=4

export GOOGLE_CLOUD_PROJECT="$GCP_PROJECT"
export CLOUD_BUILD_LOCATION=<region>
export CLOUD_BUILD_RUNTIME_SERVICE_ACCOUNT=coyote-cloud-build@${GCP_PROJECT}.iam.gserviceaccount.com
export CLOUD_BUILD_ARTIFACT_REGISTRY_REPOSITORY=<region>-docker.pkg.dev/${GCP_PROJECT}/<repository>
export CLOUD_BUILD_SOURCE_BUCKET=<source-bucket>
# Optional; defaults to coyote-sources.
export CLOUD_BUILD_SOURCE_PREFIX=coyote-sources

make gke-staging-worker-deploy
```

`CLOUD_BUILD_PROJECT` falls back to `GOOGLE_CLOUD_PROJECT`. The worker creates
the Cloud Build image-build controller only when that project configuration is
present. Without it, ordinary Kubernetes steps can work while `image_build`
nodes fail with `image build execution controller is not configured`.

`WORKER_KUBERNETES_MAX_IN_FLIGHT_JOBS` is a per-worker/controller concurrency
governor. The application default is `1`; the validated staging deployment
uses `4`. It bounds how many durable execution Jobs a worker actively
supervises and provides queue backpressure. It is not a cluster-wide limit.

## Validation

### Private control plane

```sh
make gke-control-plane-smoke
```

This private port-forward smoke verifies migration completion, Deployment
readiness, digest pinning, Cloud SQL proxy readiness, CSI mounts, GCS
configuration, server health/readiness, frontend serving, and same-origin API
routing.

### Gateway

```sh
kubectl -n coyote-ci get gateway coyote-temporary-public -o wide
kubectl -n coyote-ci get httproute coyote-temporary-public \
  coyote-temporary-http-redirect -o wide
```

The Gateway must report `Accepted=True` and `Programmed=True`. Each HTTPRoute
must have a parent reporting `Accepted=True` and `ResolvedRefs=True`.

### Public staging E2E

Complete browser OIDC login at the temporary hostname, verify the secure
session works with `/api/me`, and mint a staging token authorized for the
target project. The smoke token requires `build:read`, `build:logs`, and
`build:run`.

```sh
export COYOTE_STAGING_SMOKE_API_TOKEN=<staging-token>
make gke-public-e2e-smoke
```

The public smoke validates TLS/routing, worker heartbeat, isolated execution
Job placement, workspace prepare/publication, cache save/restore, logs,
artifact upload/download, and terminal build status. The validated dogfood
path also exercised parallel Kubernetes steps, backend/frontend builds,
external Cloud Build image builds, and non-default Git branch source
resolution.

## Operations notes and troubleshooting

Repository-defined jobs support ordinary non-default branch names such as
`feature/kubernetes-ingress-cutover`. Source resolution tries available local
refs before fetching the requested ref from `origin`.

`timeout_seconds` limits pipeline or step execution; it is not a GKE scheduling
limit. Cold Go dependency/cache work can need more than five minutes. Leave
sufficient headroom for cold conditions rather than disabling timeouts; exit
code `124` commonly indicates the configured execution timeout.

| Symptom | Likely cause | Operator action |
| --- | --- | --- |
| `Gaia id not found` | Missing GSA, incorrect KSA annotation, or missing Workload Identity binding. | Verify the GSA exists, the KSA annotation, and the exact `roles/iam.workloadIdentityUser` member. |
| `InvalidImageName` or invalid Docker tag | Digest-valued image environment variables were reused as build tags. | Unset `COYOTE_SERVER_IMAGE`, `COYOTE_MIGRATE_IMAGE`, and `COYOTE_FRONTEND_IMAGE` before the image build. |
| `redirect_uri_mismatch` | Provider and deployment callback URIs differ. | Register and deploy the exact `https://<hostname>/auth/callback` value. |
| Workspace publication GCS 403 | Server GSA cannot create/write workspace revision objects. | Grant required object access, including `storage.objects.create`, on the revision bucket. |
| `image build execution controller is not configured` | Staging worker lacks Cloud Build configuration. | Set the required `GOOGLE_CLOUD_PROJECT` and `CLOUD_BUILD_*` inputs, then redeploy the worker. |
| Cloud Build or Artifact Registry permission failure | Worker GSA lacks Cloud Build editor, Artifact Registry reader, or runtime-service-account user permission. | Verify `roles/cloudbuild.builds.editor`, `roles/artifactregistry.reader` on the configured repository, and `roles/iam.serviceAccountUser` on the configured runtime GSA. |
| `fatal: Needed a single revision` | Configured source ref is missing or cannot be fetched. | Verify the branch/ref exists remotely and check source-fetch credentials/access. |
| Step exits `124` | Execution timeout was too short. | Increase the relevant pipeline/step timeout with cold-start headroom. |
| Migration cannot reach Cloud SQL | Migration GSA, DB secret, DSN, or proxy sidecar is misconfigured. | Inspect migration Job and proxy logs; confirm Cloud SQL Client and `127.0.0.1:5432` DSN use. |
| Gateway does not program | Address, Certificate Map, DNS/certificate, or Gateway prerequisites are incomplete. | Inspect Gateway/HTTPRoute conditions and deploy-script diagnostics. |
| Execution Job appears in `coyote-ci` | Wrong worker/controller path is being used. | Stop and correct the staging worker namespace; do not repoint the production-facing worker. |

The execution timeline currently reports some helper phases as
elapsed-from-common-start measurements rather than fully independent phase
durations. Artifact collection, cache save, and workspace publication can
therefore each look nearly as long as the command even when they overlap or
wait on lifecycle work. Treat this as an observability/display issue, not
evidence that each helper independently consumed that duration.

Production cutover, broader cache/performance optimization, and documentation
reorganization belong to later work.
