# GKE Bootstrap And Clean Redeployment

Replace the two historical Helm releases with one unified chart, preserving the
existing data and public endpoint. Downtime is acceptable. No temporary Coyote
installation, new database, ownership bridge, or product release manifest is
needed. Run from the repository root at the approved, clean source revision.

**Temporary runbook:** retire this document after the successful GKE cutover and
recovery window. Keep the permanent GKE values and generic Helm/release guidance;
archive operational evidence outside Git. This is not an ongoing deployment mechanism.

**Approval boundary:** this document is preparation, not permission to execute
the cutover. Image pushes, production shutdown/deletion, backups that write cloud
state, production migrations, and reopening execution require explicit approval.
Do not change IAM, DNS, Secret Manager values, or registry policy under this runbook.

## Fixed Environment

The permanent [GKE values](../helm/gke-values.yaml) use live configuration read on
2026-11-14, not the production examples or failed release values. Image references
are deliberately omitted; a verified digest overlay is mandatory. Normal desired
replicas are chart-managed. The initial install overrides all replicas to zero.

| Preserve | Existing identity |
| --- | --- |
| Cluster | `gke_bryanchoate_us-central1_coyote-ci-autopilot` |
| Namespaces | Control `coyote-ci`; execution `coyote-ci-staging` |
| Cloud SQL | `bryanchoate:us-central1:bryanchoate-postgres`, PostgreSQL 17, database **`postgres`**, target 57 |
| Database credential | `coyote-staging-database-url` for server, worker, MCP and migrator |
| Other credentials | `coyote-staging-oidc-client-secret`, `coyote-staging-session-secret`, `coyote-workspace-helper-capability-secret`; preserve all other native/SCM credentials too |
| Artifacts / workspaces / sources | `gs://bryanchoate-coyote-ci-artifacts/`: `builds`, `workspace-revisions`, `coyote-sources` |
| Cache | `gs://coyote-ci-cache-bryanchoate/coyote-ci/cache` |
| Product releases | `gs://bryanchoate-coyote-ci-releases`, unchanged by bootstrap |
| Public endpoint | `https://coyote-ci.bryanchoate.com`; address `coyote-staging-gateway-ip`; certificate map `coyote-staging-gateway-map` |
| Identity boundary | Keep the exact KSA names/namespaces and GSA annotations in the values; helper has no GSA |

The supplied preflight establishes database `postgres`, target 57, and no
application PVCs. Reconfirm these before deletion. On 2026-11-14, Cloud SQL had
eight retained backups, seven-day PITR, and successful backup `1794639600000`.
Successful backup status is not proof of a tested restore. No restore was performed
during preparation. Secret Manager references use `latest`: record enabled version
numbers and confirm they match the credentials currently mounted before restart.

```sh
export CTX=gke_bryanchoate_us-central1_coyote-ci-autopilot
export PROJECT=bryanchoate SQL_INSTANCE=bryanchoate-postgres
export CONTROL=coyote-ci EXECUTION=coyote-ci-staging
export REGISTRY=us-central1-docker.pkg.dev/bryanchoate/coyote-ci
export VALUES=deploy/helm/gke-values.yaml
export IMAGES="$PWD/.local/helm/bootstrap-images.json"
export SHA="$(git rev-parse HEAD)"
export BUILT_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
export BOOTSTRAP_TAG="bootstrap-${SHA}-$(date -u +%Y%m%d%H%M%S)"
```

## 1. Build And Verify Bootstrap Images

Use existing Dockerfile targets, with the same source for every component. Require
a clean checkout, a working Docker host/Buildx with both platforms, registry write
authorization, and Docker authentication for `us-central1-docker.pkg.dev`.
`docker buildx inspect` must advertise `linux/amd64` and `linux/arm64`.
Do not overwrite a bootstrap tag or use a `0.0.N` product-release tag. No chart,
CLI package, V3 manifest or channel is published by these commands.

**Image-publication approval required for `--push`:**

```sh
test -z "$(git status --porcelain)"
mkdir -p .local/helm
for component in server worker mcp migrate frontend; do
  context=backend
  case "$component" in
    server|worker) target=source-runtime ;;
    mcp) target=source-runtime-mcp ;;
    migrate) target=migrate-runtime ;;
    frontend) context=frontend; target='' ;;
  esac
  target_args=()
  if test -n "$target"; then target_args=(--target "$target"); fi
  docker buildx build --platform linux/amd64,linux/arm64 \
    --file "$context/Dockerfile" "${target_args[@]}" \
    --build-arg "COYOTE_APPLICATION_VERSION=$BOOTSTRAP_TAG" \
    --build-arg "COYOTE_COMMIT=$SHA" --build-arg "COYOTE_BUILD_DATE=$BUILT_AT" \
    --provenance=mode=min --tag "$REGISTRY/coyote-$component:$BOOTSTRAP_TAG" \
    --metadata-file ".local/helm/bootstrap-$component.json" --push "$context" || exit 1
done
```

These snippets use Bash or Zsh. Source builds of server/worker share Docker cache;
both targets intentionally contain `/app/server` and `/app/worker`. The chart
overrides the worker entrypoint and uses **that same digest** for helpers.

Resolve registry-visible indexes and compare them with build metadata. Helm accepts
the resulting JSON as a values overlay; it is not a release manifest.

```sh
for component in server worker mcp migrate frontend; do
  reference="$REGISTRY/coyote-$component:$BOOTSTRAP_TAG"
  digest="$(docker buildx imagetools inspect "$reference" --format '{{json .Manifest}}' | jq -er '.digest')" || exit 1
  test "$digest" = "$(jq -er '."containerimage.digest"' ".local/helm/bootstrap-$component.json")" || exit 1
  docker buildx imagetools inspect "$REGISTRY/coyote-$component@$digest" --raw |
    jq -e '(.mediaType == "application/vnd.oci.image.index.v1+json" or .mediaType == "application/vnd.docker.distribution.manifest.list.v2+json") and
      any(.manifests[]; .platform.os == "linux" and .platform.architecture == "amd64") and
      any(.manifests[]; .platform.os == "linux" and .platform.architecture == "arm64")' || exit 1
done
jq -n --arg registry "$REGISTRY" \
  --slurpfile server .local/helm/bootstrap-server.json \
  --slurpfile worker .local/helm/bootstrap-worker.json \
  --slurpfile mcp .local/helm/bootstrap-mcp.json \
  --slurpfile migrate .local/helm/bootstrap-migrate.json \
  --slurpfile frontend .local/helm/bootstrap-frontend.json \
  '{images: ({server:$server[0], worker:$worker[0], mcp:$mcp[0], migrate:$migrate[0], frontend:$frontend[0]} |
    with_entries(.value = ($registry + "/coyote-" + .key + "@" + .value["containerimage.digest"])))}' > "$IMAGES"
for platform in linux/amd64 linux/arm64; do
  test "$(docker run --rm --network none --read-only --cap-drop ALL \
    --platform "$platform" "$(jq -r .images.migrate "$IMAGES")" target)" = 57 || exit 1
  probe_output="$(docker run --rm --network none --read-only --cap-drop ALL --platform "$platform" \
    --entrypoint /usr/bin/timeout "$(jq -r .images.worker "$IMAGES")" \
    5 /app/worker workspace prepare 2>&1)" && probe_exit=0 || probe_exit=$?
  test "$probe_exit" -eq 1 || exit 1
  printf '%s\n' "$probe_output" | grep -F \
    'workspace prepare requires internal API URL, token path, execution job ID, pod UID, and destination' || exit 1
done
go test ./backend/cmd/worker ./backend/cmd/migrate ./backend/internal/pipeline ./backend/internal/platform/cloudbuild
helm lint --strict deploy/helm/coyote-ci -n "$CONTROL" -f "$VALUES" -f "$IMAGES"
helm template coyote-ci deploy/helm/coyote-ci -n "$CONTROL" -f "$VALUES" -f "$IMAGES" \
  --set server.replicas=0,frontend.replicas=0,mcp.replicas=0,worker.replicas=0 \
  > .local/helm/bootstrap-rendered.yaml
```

The worker probe invokes the timeout utility directly and requires the supported
`workspace prepare` command to reject missing configuration with exit 1. Exit 124
(timeout), execution failures, or an unrelated startup error fail the probe. No
credentials or helper API are supplied. This checks executable/architecture/helper
dispatch, not an actual upload; the post-install controlled build checks uploads.
Review rendered identities, digest pins, target 57, namespace split, migration hook
order (-30/-20/-10), and zero consumer replicas. Expect no Namespace, Secret, PV or
PVC resources. Do not put fabricated product version/digest metadata in `release`.

## 2. Recovery Gate Before Destructive Operations

Choose an existing approved, access-controlled backup location **outside Git**;
set `EVIDENCE` to it and use `umask 077`. Archive both old releases' values,
manifests, hooks and history with explicit context/namespace. Capture live workload,
RBAC, CSI and routing definitions, native Secrets securely (never terminal output),
old image digests and exact old chart packages. Inspect uninstall hooks and both
stored manifests: uninstall must not delete durable storage or credential objects.
Do not use captured failed values as the new deployment baseline.

```sh
gcloud sql instances describe "$SQL_INSTANCE" --project="$PROJECT" \
  --format='yaml(connectionName,databaseVersion,settings.backupConfiguration)'
gcloud sql backups list --instance="$SQL_INSTANCE" --project="$PROJECT" \
  --format='table(id,status,startTime,endTime)'
kubectl --context "$CTX" get pvc -A
kubectl --context "$CTX" get pv
helm --kube-context "$CTX" get manifest coyote-ci -n "$CONTROL" > "$EVIDENCE/control-manifest.yaml"
helm --kube-context "$CTX" get hooks coyote-ci -n "$CONTROL" > "$EVIDENCE/control-hooks.yaml"
helm --kube-context "$CTX" get manifest coyote-ci-worker -n "$EXECUTION" > "$EVIDENCE/worker-manifest.yaml"
helm --kube-context "$CTX" get hooks coyote-ci-worker -n "$EXECUTION" > "$EVIDENCE/worker-hooks.yaml"
```

Require documented successful restore evidence for an existing backup, an identified
restore operator/path, preserved database ownership/roles, and a final quiescent
backup below. If recovery has never been tested, **stop**; neither a backup listing
nor `pg_restore --list` establishes recoverability. Do not provision a temporary
database/installation as part of this procedure. Do not restore a shared Cloud SQL
instance in place: recovery must protect its other databases.

Inventory and protect all three GCS buckets above, including object versions,
generation-pinned source references, checksums, release manifests and channels.
Record existing recovery/copy evidence and Secret Manager versions/access. A
current-object `rsync` or soft-delete policy alone is not a complete backup; copies
have new generations. Do not delete originals or run registry/object cleanup.

## 3. Freeze And Dispose Of No History Automatically

Freeze build submissions, repository webhooks/schedules, deployment automation,
artifact-trigger producers and release publishers. Disable verified autoscaling or
other controllers that would restore replicas. Confirm external/VM consumers of
database `postgres` are stopped too. Do not assume a worker SIGTERM drains work.

Before enabling any new worker, inventory builds, execution jobs, leases, external
Cloud Builds, artifact deliveries and incomplete workspace publications by ID.
Through an existing protected PostgreSQL connection (`PGSERVICE=coyote-maintenance`,
configured outside Git; no password in argv), capture a read-only baseline:

```sh
PGSERVICE=coyote-maintenance psql -X -v ON_ERROR_STOP=1 <<'SQL'
BEGIN READ ONLY;
SELECT current_database();
SELECT version_id, is_applied FROM public.goose_db_version ORDER BY id;
SELECT status, count(*) FROM builds GROUP BY status;
SELECT status, count(*) FROM build_jobs GROUP BY status;
SELECT id, build_id, status, claimed_by, claim_expires_at
FROM build_jobs WHERE status IN ('queued', 'running') ORDER BY created_at;
SELECT status, count(*) FROM workspace_revisions GROUP BY status;
SELECT id, build_id, status FROM workspace_revisions WHERE status <> 'published';
COMMIT;
SQL
gcloud builds list --project="$PROJECT" --region=us-central1 \
  --filter='status=(QUEUED WORKING)' --format='table(id,status,createTime)'
kubectl --context "$CTX" -n "$EXECUTION" get jobs,pods
```

For each historical nonterminal item, explicitly choose finish, supported cancel,
or keep held for investigation. A held item may remain in the database, but **workers
must stay at zero** until it cannot be claimed or its execution is separately
approved. There is no per-cutover queue quarantine switch. Never truncate tables,
reset leases, mark missing workspaces published, fabricate outputs, or bulk replay.
Use existing cancellation only after reviewing its semantics; unresolved records
are a blocker to resuming execution, not permission for ad hoc SQL repairs.

Active remote builds and helper uploads must reach a reconciled terminal state or
receive an explicit cancellation/recovery decision before shutdown. Keep evidence
of incomplete workspace records and their object keys; do not delete their bytes.
Server startup also runs release/notification/SCM recovery; zero workers alone is
not a database read-only mode. Review those pending records before server activation.

## 4. Approved Shutdown, Final Backup, And Uninstall

After the freeze/history gates, stop all four consumers:

```sh
kubectl --context "$CTX" -n "$EXECUTION" scale deployment coyote-kubernetes-worker-staging --replicas=0
kubectl --context "$CTX" -n "$CONTROL" scale deployment coyote-server coyote-mcp coyote-frontend --replicas=0
kubectl --context "$CTX" -n "$CONTROL" get pods
kubectl --context "$CTX" -n "$EXECUTION" get pods
```

Wait for consumer Pods to disappear, all migration Jobs to be terminal, and
read-only `pg_stat_activity` inspection to show no other application writers to
`postgres`. Pending historical queue records can remain held; active processes
cannot. Preserve namespaces, Secrets, GSAs/IAM and the external verifier token.

**Backup-write approval required.** Take and verify a final Cloud SQL backup and a
target-database dump through the protected connection. Record its backup ID and
`SUCCESSFUL` completion; record SHA-256 of the dump, GCS inventory/generations and
the quiescent application row/identity baseline together.

```sh
gcloud sql backups create --instance="$SQL_INSTANCE" --project="$PROJECT"
PGSERVICE=coyote-maintenance pg_dump --format=custom --file="$EVIDENCE/postgres.dump"
pg_restore --list "$EVIDENCE/postgres.dump" > "$EVIDENCE/postgres-dump-contents.txt"
shasum -a 256 "$EVIDENCE/postgres.dump"
```

Only after recovery sign-off, remove worker release first, then control release.
`--no-hooks` is intentional for **uninstall only**, after inspecting archived hooks;
it prevents historical delete hooks from executing. Never use it on install/upgrade.

```sh
helm --kube-context "$CTX" uninstall coyote-ci-worker -n "$EXECUTION" --no-hooks --wait --timeout 15m
helm --kube-context "$CTX" uninstall coyote-ci -n "$CONTROL" --no-hooks --wait --timeout 15m
```

Compare the rendered new resource list with survivors. Delete only individually
reviewed stateless collisions using `kubectl --context "$CTX" -n NAMESPACE delete
KIND NAME`: old terminal migration Jobs, hook ServiceAccount/CSI references, and
exclusive old worker/helper/verifier RBAC are possible survivors. Verify no process
uses them first. Do not delete native Secrets, PVCs/PVs, namespaces, the external
`coyote-workspace-verifier` credential, or shared RBAC. No namespace deletion,
directory-wide apply/delete, `delete all`, force adoption, or finalizer stripping.
Wait for old Gateway cleanup; preserve its reserved IP, certificate map and DNS.

## 5. Install Once, Then Activate Deliberately

**Production migration approval required**, even when target 57 appears current.
Use the same reviewed checkout/chart and image overlay. The hook, not app startup,
owns Goose. All consumers stay stopped after the hook while historical state is held.

```sh
helm --kube-context "$CTX" install coyote-ci deploy/helm/coyote-ci -n "$CONTROL" \
  -f "$VALUES" -f "$IMAGES" --set migration.maintenanceAcknowledged=true \
  --set server.replicas=0,frontend.replicas=0,mcp.replicas=0,worker.replicas=0 \
  --wait --timeout 15m
kubectl --context "$CTX" -n "$CONTROL" logs job/coyote-migrate-coyote-ci-1 -c migrate
helm --kube-context "$CTX" status coyote-ci -n "$CONTROL"
```

Require successful hook completion, target/history 57 and preserved baseline rows.
No migration bridge, manual Goose invocation, second Helm release or V3 manifest.
Do not use `--atomic` or `--cleanup-on-fail`. A timeout does not stop the hook Job:
inspect it, wait for terminal status and retry the same target only after diagnosis.
Do not delete release metadata or hook identities while a migration may be active.

Once startup recovery is approved, activate server/MCP/frontend with standard
`kubectl scale`; keep workers zero and external submissions frozen. These one-time
scales deliberately differ from the initial Helm zero-replica values until sign-off.

```sh
kubectl --context "$CTX" -n "$CONTROL" scale deployment coyote-server coyote-mcp coyote-frontend --replicas=1
kubectl --context "$CTX" -n "$CONTROL" rollout status deployment/coyote-server --timeout=10m
kubectl --context "$CTX" -n "$CONTROL" rollout status deployment/coyote-mcp --timeout=10m
kubectl --context "$CTX" -n "$CONTROL" rollout status deployment/coyote-frontend --timeout=10m
curl --fail https://coyote-ci.bryanchoate.com/readyz
kubectl --context "$CTX" -n "$CONTROL" get gateway,httproute
```

Check OIDC login/session and API-token access, historical builds/logs/users, a known
artifact download against its stored SHA-256, GCS workspace/cache reads, MCP health,
and routing TLS/hostname/address. Confirm Gateway `Accepted/Programmed`, HTTPRoutes
`Accepted/ResolvedRefs`, exact running image digests, and unchanged credential refs.
Do not use legacy smoke wrappers that assume the old migration Job name or submit
unreviewed work. No historical record should disappear during these checks.

After every old runnable record has an approved disposition, enable the worker and
run one explicitly approved non-publishing build. Verify heartbeat, execution in
`coyote-ci-staging`, prepare/publish/cache/artifact helpers using the worker digest,
artifact checksums and build-container credential isolation. If held work is still
claimable, do not start this test: a worker cannot select only the test job.

```sh
kubectl --context "$CTX" -n "$EXECUTION" scale deployment coyote-kubernetes-worker-staging --replicas=1
kubectl --context "$CTX" -n "$EXECUTION" rollout status deployment/coyote-kubernetes-worker-staging --timeout=10m
```

After validation, persist normal chart-managed replicas (all one) with the same
values/images; this runs the automatic migration guard again and needs the same
approved target. Do not claim online DDL compatibility; target 57 must be current.

```sh
helm --kube-context "$CTX" upgrade coyote-ci deploy/helm/coyote-ci -n "$CONTROL" \
  -f "$VALUES" -f "$IMAGES" --wait --timeout 15m
```

Leave both migration approval flags at their default `false`. With target 57
already applied, the guarded hook is a no-op even without release metadata or an
`upgrade_from` range. If it reports pending migrations, stop and investigate;
do not add an approval flag merely to make this replica-setting upgrade pass.

Reopen submissions only after sign-off. The updated installation can then execute
the normal pinned five-component V3 pipeline and publisher in [the release
guide](../../docs/releases.md), with separate image/chart/manifest publication
approval. Its artifact URLs use the preserved production endpoint.

## Blockers And Recovery

- Obtain the missing live preflight/restore evidence and final protected-state
  checkpoint; the checked-in discovery is not restore certification.
- Publish/verify current bootstrap digests after approval. The legacy migrator
  lacks the required runner contract; old server/worker cannot execute V3.
- Resolve or explicitly hold historical runnable/incomplete records before
  activation. No automatic cleanup/replay is provided or authorized.
- Confirm no external consumer/automation or protected resource blocks uninstall,
  and existing CSI/Workload Identity/native-sidecar permissions still work.

No current image/chart incompatibility was identified in source/render inspection.
Local validation on 2026-11-14 passed strict Helm lint, the 35-object zero-replica
render, shell-block syntax, and the four focused Go test packages above. A local
multi-platform `migrate-runtime` build returned `57` on both architectures with no
network or database credentials. On 2026-11-15, the worker probe above passed on
both architectures, and `bash scripts/kind-helm-smoke.sh` passed in disposable Kind
with PostgreSQL 17: zero-to-normal replicas, no release metadata/range, both
approvals false, a completed migration hook, and byte-identical Goose history.
The unapproved pending-migration case was rejected. This used native Secrets and
filesystem storage; a separate offline GKE render confirmed all four normal
replicas and the same migration flags. Published-digest execution, GKE CSI/Cloud SQL
access, MCP runtime, and a helper build remain acceptance gates.

Before any schema change, recovery can reinstall archived old charts/images
with their historical migration controls. After hook execution, prefer a forward
fix; any database restore needs a separately approved, database-scoped recovery
plan protecting other data. Helm rollback or Goose down is not a database backup.

## Retire After Successful Cutover

After the recovery window and caller checks, remove the standalone
`deploy/helm/coyote-ci-worker` chart; obsolete split staging/production worker
examples and local split values; `scripts/gke-helm-{adopt,normalize-ownership,rollout,staging-values}.sh`;
raw `gke-{control-plane-deploy,staging-worker-deploy,deploy,gateway-deploy}.sh`
paths and their superseded manifests under `deploy/kubernetes/gke`; associated
tests/Make targets and stale split-release instructions. Do not delete them during
cutover. Keep backup evidence and old chart packages outside Git. Keep external
workspace-verifier resources/tools until all external consumers are ruled out.
Retain useful image-building, migration-evidence and generic release tools; they
are not ownership machinery.