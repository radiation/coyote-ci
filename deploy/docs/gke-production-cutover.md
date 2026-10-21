# GKE Production Cutover Runbook

This runbook moves the Coyote CI control plane from the legacy Compute Engine
VM to GKE without changing live infrastructure as part of repository
preparation. It uses Cloud SQL instance
`bryanchoate:us-central1:bryanchoate-postgres`, a new `coyote_ci` database,
and the existing GCS resources. It does not migrate historical Coyote data.

Do not run this procedure until the preflight checks below are complete. Never
print Secret Manager values, Kubernetes Secret contents, or database passwords.

## Repository configuration

The non-secret starting points are:

- `deploy/helm/examples/gke-production-control-plane-values.yaml`
- `deploy/helm/examples/gke-production-worker-values.yaml`

Copy them outside Git before replacing every illustrative GSA, GCS, Cloud
Build, OIDC, global-address, and certificate-map reference:

```sh
mkdir -p .local/helm
cp deploy/helm/examples/gke-production-control-plane-values.yaml \
  .local/helm/gke-production-control-plane-values.yaml
cp deploy/helm/examples/gke-production-worker-values.yaml \
  .local/helm/gke-production-worker-values.yaml
chmod 600 .local/helm/gke-production-*-values.yaml
```

Both profiles use the `coyote-ci-database-url` Secret Manager secret and the
same Cloud SQL instance. The chart mounts that secret only as
`DATABASE_URL_FILE` for the server, migration Job, and production worker.
The migration Job cannot receive a DSN for another database from these values.

The production profile uses the existing `coyote-ci` namespace for the control
plane and production worker. `allowSharedExecutionNamespace: true` and
`allowSharedControlPlaneNamespace: true` are explicit exceptions for that
existing topology; staging retains its separate `coyote-ci-staging` namespace
and default isolation.

`coyote-mcp` is deliberately disabled in the copied production profile until
its live ownership and database contract are verified. The rollout does not
inspect an unmanaged MCP Deployment's database contract. If an unowned
`coyote-mcp` Deployment exists, it stops before any scale or migration
operation until an operator has independently verified that it cannot access
the `coyote_ci` database and explicitly sets
`GKE_HELM_UNMANAGED_MCP_DATABASE_ISOLATION_CONFIRMED=true`. A Deployment owned
by another or partial Helm release always stops the rollout. Do not point MCP
at `coyote_ci` based on this runbook alone.

## 1. Read-only preflight

Set the fixed project and instance names:

```sh
export GCP_PROJECT=bryanchoate
export CLOUD_SQL_INSTANCE=bryanchoate-postgres
export CLOUD_SQL_CONNECTION_NAME=bryanchoate:us-central1:bryanchoate-postgres
export CONTROL_NAMESPACE=coyote-ci
```

Confirm the target is empty and unrelated databases remain untouched. These
commands print metadata only:

```sh
gcloud sql instances describe "$CLOUD_SQL_INSTANCE" \
  --project="$GCP_PROJECT" \
  --format='yaml(name,region,databaseVersion,state,settings.backupConfiguration)'
gcloud sql databases list --instance="$CLOUD_SQL_INSTANCE" \
  --project="$GCP_PROJECT" --format='table(name)'
kubectl -n "$CONTROL_NAMESPACE" get deployment coyote-server coyote-frontend \
  coyote-kubernetes-worker coyote-mcp --ignore-not-found
kubectl -n "$CONTROL_NAMESPACE" get deployment coyote-mcp --ignore-not-found \
  -o jsonpath='{.metadata.labels.app\.kubernetes\.io/managed-by}{"\n"}'
kubectl -n "$CONTROL_NAMESPACE" get deployment coyote-mcp --ignore-not-found \
  -o jsonpath='{.spec.template.spec.containers[*].name}{"\n"}'
```

Record the VM name, zone, IP, legacy DNS record, GKE global IP, Certificate
Manager map, OIDC client, GCS buckets/prefixes, and Artifact Registry
repository. Inspect the VM's storage and worker ownership before cutover; do
not copy its `.env.prod` file or secret values into the repository.

## 2. Provision the isolated database and application role

Run these once with an administrator authorized for this Cloud SQL instance.
The password remains in the shell process and is sent to Google Cloud through
standard input; it is neither printed nor stored in Git. Refuse to continue if
the dedicated secret already exists; this procedure must not overwrite another
credential.

```sh
export GCP_PROJECT=bryanchoate
export CLOUD_SQL_INSTANCE=bryanchoate-postgres
read -rsp 'New coyote_ci_app password: ' COYOTE_CI_DB_PASSWORD
printf '\n'
if gcloud secrets describe coyote-ci-database-url --project="$GCP_PROJECT" >/dev/null 2>&1; then
  echo "coyote-ci-database-url already exists; inspect its owner and stop" >&2
  exit 1
fi
gcloud sql databases create coyote_ci --instance="$CLOUD_SQL_INSTANCE" \
  --project="$GCP_PROJECT"
gcloud sql users create coyote_ci_app --instance="$CLOUD_SQL_INSTANCE" \
  --project="$GCP_PROJECT" --password="$COYOTE_CI_DB_PASSWORD"
```

Connect through a temporary Cloud SQL Auth Proxy or approved administrator
path, then grant only the permissions needed to create and own Coyote's schema.
Provide the administrator DSN interactively; it must connect to `coyote_ci`.

```sh
read -rsp 'Administrator coyote_ci DSN: ' CLOUD_SQL_ADMIN_DSN
printf '\n'
psql "$CLOUD_SQL_ADMIN_DSN" -v ON_ERROR_STOP=1 <<'SQL'
GRANT CONNECT, TEMPORARY ON DATABASE coyote_ci TO coyote_ci_app;
GRANT USAGE, CREATE ON SCHEMA public TO coyote_ci_app;
SQL
unset CLOUD_SQL_ADMIN_DSN
```

Create the dedicated secret with a loopback DSN for the in-Pod Cloud SQL Auth
Proxy. Percent-encode the password so URI-reserved characters work correctly.
Validate the generated connection with a temporary `PGPASSFILE`, which avoids
putting credentials in command output or process arguments. This must be the
only DSN used by the production values. Run the validation while an authorized
Cloud SQL Auth Proxy is listening on `127.0.0.1:5432`; it must not connect to a
different database or a public Cloud SQL endpoint:

```sh
ENCODED_DB_PASSWORD="$(printf '%s' "$COYOTE_CI_DB_PASSWORD" | ruby -e '
  password = STDIN.read
  puts password.bytes.map { |byte|
    (byte >= 0x41 && byte <= 0x5a) ||
      (byte >= 0x61 && byte <= 0x7a) ||
      (byte >= 0x30 && byte <= 0x39) ||
      [0x2d, 0x2e, 0x5f, 0x7e].include?(byte) ? byte.chr : "%%%02X" % byte
  }.join
')"
DATABASE_URL="postgres://coyote_ci_app:${ENCODED_DB_PASSWORD}@127.0.0.1:5432/coyote_ci?sslmode=disable"
PGPASS_PASSWORD="$(printf '%s' "$COYOTE_CI_DB_PASSWORD" | ruby -e '
  password = STDIN.read
  puts password.gsub(/[:\\]/) { |character| "\\#{character}" }
')"
PGPASS_FILE="$(mktemp)"
chmod 600 "$PGPASS_FILE"
printf '127.0.0.1:5432:coyote_ci:coyote_ci_app:%s\n' "$PGPASS_PASSWORD" >"$PGPASS_FILE"
PGPASSFILE="$PGPASS_FILE" psql \
  'postgres://coyote_ci_app@127.0.0.1:5432/coyote_ci?sslmode=disable' \
  -Atqc 'SELECT current_database()' | grep -Fx coyote_ci
rm -f "$PGPASS_FILE"
printf '%s' "$DATABASE_URL" | gcloud secrets create coyote-ci-database-url \
  --project="$GCP_PROJECT" --replication-policy=automatic --data-file=-
unset DATABASE_URL ENCODED_DB_PASSWORD COYOTE_CI_DB_PASSWORD
```

If `coyote_ci` already exists, stop and inspect its owner and Goose history.
Do not reuse `coyote`, `postgres`, or any other shared database.

## 3. Secret Manager, IAM, OIDC, and certificate changes

Create a new session secret; add the existing OIDC client secret to its own
new secret name without putting its value in a command or values file:

```sh
SESSION_SECRET="$(openssl rand -hex 32)"
printf '%s' "$SESSION_SECRET" | gcloud secrets create coyote-ci-session-secret \
  --project="$GCP_PROJECT" --replication-policy=automatic --data-file=-
unset SESSION_SECRET
gcloud secrets create coyote-ci-oidc-client-secret \
  --project="$GCP_PROJECT" --replication-policy=automatic
# Add the OIDC secret value interactively in Cloud Console or with:
# gcloud secrets versions add coyote-ci-oidc-client-secret --data-file=-
```

Grant Secret Manager access only to the workload identities selected in the
copied production values. The server needs all four listed secrets; migration
and worker identities need only the database secret.

```sh
export SERVER_GSA='REPLACE_WITH_SERVER_GSA_EMAIL'
export MIGRATE_GSA='REPLACE_WITH_MIGRATE_GSA_EMAIL'
export WORKER_GSA='REPLACE_WITH_WORKER_GSA_EMAIL'
for secret in coyote-ci-database-url coyote-workspace-helper-capability-secret \
  coyote-ci-oidc-client-secret coyote-ci-session-secret; do
  gcloud secrets add-iam-policy-binding "$secret" --project="$GCP_PROJECT" \
    --member="serviceAccount:${SERVER_GSA}" \
    --role='roles/secretmanager.secretAccessor'
done
for principal in "$MIGRATE_GSA" "$WORKER_GSA"; do
  gcloud secrets add-iam-policy-binding coyote-ci-database-url \
    --project="$GCP_PROJECT" --member="serviceAccount:${principal}" \
    --role='roles/secretmanager.secretAccessor'
done
```

The selected server, migration, and worker GSAs also require
`roles/cloudsql.client`. Keep the existing, least-privilege GCS and Cloud
Build grants after confirming their bucket and repository names. Register this
exact callback URL at the OIDC provider:

```text
https://coyote-ci.bryanchoate.com/auth/callback
```

Use the existing global address and Certificate Manager map when present. If
new production certificate resources are required, create them before any DNS
change:

```sh
gcloud certificate-manager dns-authorizations create coyote-ci-production \
  --project="$GCP_PROJECT" --domain=coyote-ci.bryanchoate.com
gcloud certificate-manager dns-authorizations describe coyote-ci-production \
  --project="$GCP_PROJECT" --format='yaml(dnsResourceRecord)'
# Publish the displayed DNS authorization record, wait for it to resolve, then:
gcloud certificate-manager certificates create coyote-ci-production \
  --project="$GCP_PROJECT" --domains=coyote-ci.bryanchoate.com \
  --dns-authorizations=coyote-ci-production
gcloud certificate-manager maps create coyote-ci-production-map \
  --project="$GCP_PROJECT"
gcloud certificate-manager maps entries create coyote-ci-production-entry \
  --project="$GCP_PROJECT" --map=coyote-ci-production-map \
  --hostname=coyote-ci.bryanchoate.com --certificates=coyote-ci-production
```

Set the copied control-plane values to the actual Certificate Manager map and
the existing or newly reserved global address name. Do not modify production
DNS in this phase.

## 4. Render, migrate, and deploy before DNS

Resolve one immutable product release manifest before rendering. The generated
overlay supplies all four required digest references: server, frontend, worker,
and migrate. Do not assemble image references independently or use mutable
tags. `release resolve` accepts a filesystem path or `file://` URL whose
directory contains `releases/<version>.json` and `channels.json`; it does not
resolve a `gs://` path.

```sh
export COYOTE_RELEASE_VERSION='REPLACE_WITH_APPROVED_RELEASE'
export COYOTE_RELEASE_SOURCE='REPLACE_WITH_EXISTING_RELEASE_SOURCE'
make cli-build
backend/tmp/coyote release resolve \
  --release "$COYOTE_RELEASE_VERSION" \
  --release-source "$COYOTE_RELEASE_SOURCE" \
  --output .local/helm/coyote-release-images.yaml
```

If no approved manifest exists, publish one only from a clean, trusted
checkout. Either publish a complete approved Coyote build:

```sh
backend/tmp/coyote release publish-coyote \
  --build 'REPLACE_WITH_COMPLETE_COYOTE_BUILD_ID' \
  --release-source "$COYOTE_RELEASE_SOURCE" \
  --channel latest
```

or build and publish all four product images together:

```sh
VERSION='REPLACE_WITH_SEMVER' \
RELEASE_REGISTRY='us-central1-docker.pkg.dev/bryanchoate/coyote-ci' \
RELEASE_SOURCE="$COYOTE_RELEASE_SOURCE" \
RELEASE_CHANNEL=latest \
make release-build
```

Do not publish a partial manifest or substitute live image digests for an
approved product release.

Validate the local profiles and release overlay first:

```sh
helm lint deploy/helm/coyote-ci \
  --namespace coyote-ci --values .local/helm/gke-production-control-plane-values.yaml
helm lint deploy/helm/coyote-ci-worker \
  --namespace coyote-ci --values .local/helm/gke-production-worker-values.yaml
helm template coyote-ci deploy/helm/coyote-ci --namespace coyote-ci \
  --values .local/helm/gke-production-control-plane-values.yaml \
  --values .local/helm/coyote-release-images.yaml >/dev/null
helm template coyote-ci-worker deploy/helm/coyote-ci-worker --namespace coyote-ci \
  --values .local/helm/gke-production-worker-values.yaml \
  --values .local/helm/coyote-release-images.yaml >/dev/null
```

After verifying that the live `coyote-mcp` is a Coyote CI component and that
its database transition is approved, explicitly transfer its verified
non-secret configuration into
`.local/helm/gke-production-control-plane-values.yaml` before running the
commands above. The copied profile does not provide an MCP image or Google
service-account binding and has `mcp.enabled: false`; enabling it alone is not
valid.

Set `images.mcp`, `serviceAccounts.mcp.name`, and
`serviceAccounts.mcp.gsaEmail` to the verified live values. Set
`mcp.enabled: true`, preserve its replicas, external-replica-management
setting, resources, service port, termination grace period, and allowed
origins, and retain the existing `coyote-mcp` ServiceAccount and
SecretProviderClass identities. Use the intended production Secret Manager
reference only after the MCP database transition is approved. Re-run the lint
and template commands above after these settings are present.

### Adopt ownership before applying production configuration

Do not run ownership adoption with the production values: their database
secret references, hostname, OIDC callback, Gateway configuration, worker API
URL, helper image, and release images intentionally differ from the running
objects. Create two operator-local, uncommitted adoption values files that
render the current live configuration exactly. They must preserve current
resource names and selectors and, in particular, set:

- the current hostname, OIDC callback, Gateway host/certificate map, and
  current database secret references;
- `worker.internalAPIURL` to the current public worker endpoint;
- `worker.helperImage` to the current helper image;
- current image references for server, frontend, worker, migrate, and MCP.

Run the strict preflight against those live-equivalent files. It must complete
with no mismatches before metadata adoption is considered:

```sh
GKE_HELM_CONTROL_PLANE_VALUES=.local/helm/gke-adoption-control-values.yaml \
GKE_HELM_WORKER_VALUES=.local/helm/gke-adoption-worker-values.yaml \
GKE_HELM_CONTROL_PLANE_NAMESPACE=coyote-ci \
GKE_HELM_WORKER_NAMESPACE=coyote-ci \
GKE_HELM_ADOPT_DRY_RUN=true \
GKE_HELM_ADOPT_APPLY=true \
bash scripts/gke-helm-adopt.sh
```

After an approved review of a zero-drift preflight, run the same command with
only `GKE_HELM_ADOPT_DRY_RUN` changed to `false`:

```sh
GKE_HELM_CONTROL_PLANE_VALUES=.local/helm/gke-adoption-control-values.yaml \
GKE_HELM_WORKER_VALUES=.local/helm/gke-adoption-worker-values.yaml \
GKE_HELM_CONTROL_PLANE_NAMESPACE=coyote-ci \
GKE_HELM_WORKER_NAMESPACE=coyote-ci \
GKE_HELM_ADOPT_DRY_RUN=false \
GKE_HELM_ADOPT_APPLY=true \
bash scripts/gke-helm-adopt.sh
```

This adopts only Helm ownership metadata. Then run the normal production
rollout below to apply the intentional changes. Do not relax the preflight
comparison or use `helm uninstall`.

After ownership adoption has completed, run the controlled rollout. It scales
database consumers to zero, updates only the production migrator ServiceAccount
and `coyote-migrate-secrets` SecretProviderClass, applies one uniquely named
migration Job, waits for success, then restores the server, frontend, worker,
and MCP. This prerequisite update ensures the Job mounts the intended
production database reference before it starts:

```sh
GKE_HELM_CONTROL_PLANE_VALUES=.local/helm/gke-production-control-plane-values.yaml \
GKE_HELM_WORKER_VALUES=.local/helm/gke-production-worker-values.yaml \
GKE_HELM_CONTROL_PLANE_NAMESPACE=coyote-ci \
GKE_HELM_WORKER_NAMESPACE=coyote-ci \
GKE_HELM_CONTROL_PLANE_RELEASE=coyote-ci \
GKE_HELM_WORKER_RELEASE=coyote-ci-worker \
GKE_HELM_RELEASE_VALUES=.local/helm/coyote-release-images.yaml \
make gke-helm-rollout
```

Inspect the completed migration without exposing its DSN:

```sh
kubectl -n coyote-ci get jobs -l app.kubernetes.io/name=coyote-migrate
kubectl -n coyote-ci get pods -l job-name \
  --sort-by=.metadata.creationTimestamp
```

Validate the new Gateway before changing DNS. Substitute the reserved global
IP, after its certificate is active:

```sh
export GKE_GLOBAL_IP='REPLACE_WITH_GKE_GLOBAL_IP'
curl --resolve "coyote-ci.bryanchoate.com:443:${GKE_GLOBAL_IP}" \
  --fail --show-error --silent https://coyote-ci.bryanchoate.com/readyz
curl --resolve "coyote-ci.bryanchoate.com:443:${GKE_GLOBAL_IP}" \
  --fail --show-error --silent https://coyote-ci.bryanchoate.com/api/readyz
kubectl -n coyote-ci get gateway,httproute
```

## 5. Ordered DNS cutover and validation

1. Lower the existing production DNS TTL to 60--300 seconds at least one old
   TTL in advance. Announce a short build-submission window.
2. Confirm the migration Job succeeded once, both Deployment images are
   digest-pinned, Gateway and routes are accepted/programmed, and the
   `curl --resolve` checks pass.
3. Drain active VM worker work and verify no legacy worker heartbeat can claim
   `coyote_ci` work. Do not run the VM worker and GKE production worker against
   the new database concurrently.
4. Change only the `coyote-ci.bryanchoate.com` A record to the GKE global IP.
5. Verify DNS, TLS, `/readyz`, `/api/readyz`, OIDC login, `/api/me`, webhook
   delivery, one complete build, log streaming, artifact upload/download and
   checksum, and a second build that exercises cache/workspace handoff.
6. Keep the VM running but idle for the observation period. Then stop its
   Coyote services, rerun health, authenticated UI, build, artifact, and
   webhook checks through GKE, and only then stop (not delete) the VM.

The eventual deletion candidates are VM-only: the instance, boot disk after a
snapshot/retention period, VM static IP, VM firewall rules, reverse-proxy
configuration, and legacy verifier credential. Never delete the shared Cloud
SQL instance, GCS buckets, Artifact Registry, GKE address, certificate map,
DNS zone, OIDC client, or Secret Manager secrets merely because the VM is
retired.

## Rollback

For routing, TLS, OIDC, API, worker, or artifact failure:

```sh
kubectl -n coyote-ci scale deployment/coyote-kubernetes-worker --replicas=0
# Restore coyote-ci.bryanchoate.com A record to the recorded VM IP.
# Wait for the reduced TTL, then verify VM health and enable only its worker.
```

Do not run Goose down migrations or restore Cloud SQL just because DNS rolls
back. Preserve Gateway/HTTPRoute conditions, migration Job logs, failed build
identifiers, webhook results, and DNS answers for diagnosis.
