# Database Migrations Runbook

This repository uses Goose for PostgreSQL schema migrations.

## Location and rules

- Migrations live in `backend/db/migrations`.
- Applied migrations are immutable.
- Add new numbered migration files for schema changes.
- Do not edit old applied migration files.
- Prefer deterministic DDL in tracked migrations. Avoid `IF NOT EXISTS` / `IF EXISTS` on schema objects when the migration is asserting expected state; let durable deployments fail loudly on drift or partial manual changes instead of masking them.

## Core commands

From repository root:

```bash
make db-migrate-create name=add_example_column
make db-migrate-status
make db-migrate-up
make db-migrate-down-one
```

Use a custom database DSN when needed:

```bash
make db-migrate-up MIGRATE_DSN='postgres://user:pass@localhost:5432/coyote_ci?sslmode=disable'
```

Compose one-shot migration runner:

```bash
docker compose run --rm migrate
```

## Immutable migration image

The `migrate-runtime` target in [backend/Dockerfile](../../backend/Dockerfile)
contains the Coyote runner using Goose `v3.24.1` and the tracked migration directory. It
accepts `DATABASE_URL` or `DATABASE_URL_FILE`; the file value takes precedence,
matching the application database configuration.

Build it directly when preparing a release image:

```bash
docker build --target migrate-runtime -t coyote-migrate:local ./backend
```

The image runs `migrate up` by default. Running it against an already-current
database exits successfully. `up`, `down`, and `status` use the same PostgreSQL
session advisory lock, including version-table initialization. Make and Compose
use this runner too. Raw Goose CLI/database writers are not supported concurrent
writers. Migration SQL and `goose_db_version` use the `public` schema regardless
of a DSN search-path override.

Read the bundled target without database access:

```bash
docker run --rm "$COYOTE_MIGRATE_IMAGE" target
```

Set `MIGRATION_SCHEMA_TARGET` (or `-target`) to verify an expected image target.
The Helm hook always requires it. A mismatch fails before database writes.
Applied versions absent from the image, including a database ahead of the image,
are rejected. This is version/history verification, not SQL checksum verification;
publishers must verify the exact digest's target and never change applied SQL.
The V3 manifest format and publication immutability contract are unchanged.

## Canonical rollout rule (persistent environments)

- Run migrations before app rollout.
- Do not depend on app startup auto-migration.
- Avoid concurrent first-start migration races.

## Recommended sequences

### Local development

1. Start database.
2. Run `make db-migrate-up` (or `docker compose run --rm migrate`).
3. Start backend and worker.

### CI

1. Provision ephemeral Postgres.
2. Run `make db-migrate-up` against CI DSN.
3. Run tests after migration step succeeds.

### Production / persistent deployment

1. Run migrations once in a controlled deploy step.
2. Verify migration status.
3. Roll out backend and worker instances.

### Kubernetes deployment contract

The Job uses the digest-pinned `migrate-runtime` image and mounts only the database
DSN as `DATABASE_URL_FILE`. Native Secrets must already exist. Optional GKE CSI
requires the installed CSI driver, secret versions, Workload Identity and IAM.
Optional Cloud SQL uses a native init sidecar with a startup probe; Kubernetes
1.29+ with native sidecars enabled is required. Its identity needs Cloud SQL Client
and database-secret access. Database/network/image access are external prerequisites.

### Automatic Helm lifecycle

Use one `helm upgrade --install ... --wait --timeout 15m` at desired replicas,
as shown in the [Helm guide](../helm/README.md#install-in-one-operation).
Blocking `pre-install,pre-upgrade` hooks create the migrator ServiceAccount,
optional SecretProviderClass, then Job. Deployments remain ordinary resources.
There is no second worker release, manual migration phase, or application startup
migrator. Never bypass hooks with `--no-hooks`.

Goose state is authoritative. A missing application-version annotation does not
mean an empty database. Fresh databases and already-current retries can proceed;
pending changes on an existing database need either a verified source version in
`compatibility.upgrade_from` with `migration.onlineUpgradeApproved: true`, or
`migration.maintenanceAcknowledged: true` after quiescing consumers. If needed,
assert a verified legacy source via `migration.sourceVersion`; do not guess it.
Partially completed first installs also require explicit maintenance acknowledgment
when they leave applied migrations but no application history.

Online approval requires release-specific backward-compatibility testing, not just
a version range. Historical migrations are not all online-compatible. For breaking
changes, drain work and stop every database consumer before invoking Helm; zero
replicas in the same upgrade take effect only after its pre-upgrade hook. V3
`compatibility.maintenance` describes support lifecycle, not downtime permission.

### Failure and retry

1. Inspect the failed release and retained Job before changing anything:

   ```bash
   helm status coyote-ci -n "$COYOTE_NAMESPACE"
   kubectl -n "$COYOTE_NAMESPACE" get jobs -l app.kubernetes.io/instance=coyote-ci,app.kubernetes.io/name=coyote-migrate
   kubectl -n "$COYOTE_NAMESPACE" describe job "$MIGRATION_JOB"
   kubectl -n "$COYOTE_NAMESPACE" logs job/"$MIGRATION_JOB" -c migrate
   ```

2. Resolve missing Secret/IAM/network/image prerequisites, lock contention, or SQL
   failure. Do not expose the DSN in diagnostic output. Capture Job status/logs
   before deletion. Failed Jobs are retained and have no automatic retry.
3. Wait until the old Job has a terminal Complete/Failed condition and no migration
   process remains. A Helm timeout does not cancel its Job. The chart refuses to
   replace hook identities while an earlier release Job is nonterminal. If Helm
   metadata remains pending after an interrupted operation, resolve it through the
   documented Helm recovery process before retrying; do not delete release metadata
   or hook identities while work may still be running.
4. Repeat the same Helm command with the same immutable release/image. Goose skips
   successful migrations; an individual transactional failure rolls back that
   migration, not earlier successes. Nontransactional SQL requires separate recovery.
   A same-revision retry may replace its old terminal Job; new revisions retain old
   Jobs. Archive and prune evidence deliberately.

`migration.activeDeadlineSeconds` defaults to 600, minimum 60. The runner deadline
is 30 seconds shorter, including bounded lock acquisition (about 120 seconds).
Set Helm's timeout above the Job deadline plus workload startup time; increase
both deliberately for reviewed long migrations. Avoid `--atomic` and
`--cleanup-on-fail`; a failed Helm operation does not imply database rollback.

Helm uninstall does not clean up hook resources. Preserve audit evidence, then
remove obsolete Jobs and unused migration identities only when no attempt runs.
Serialize deploy operations even across separate Helm releases sharing a database;
the advisory lock protects SQL, not application rollout ordering.

### One-time ordinary-resource to hook transition

Do not directly upgrade a release whose stored ordinary manifest still contains
the migrator ServiceAccount or `coyote-migrate-secrets` SecretProviderClass.
On Helm 4.3.0, the pre-upgrade hooks successfully replace those resources and run
the Job, but the subsequent ordinary-resource cleanup deletes the replacements:
their identities are present in the old ordinary manifest and absent from the new
one. Helm still reports `deployed`. A second hook-enabled upgrade recreates them.
A first hook failure retains them, but its successful retry still deletes them.
Do not rely on that transient deletion/recreation as the supported transition.

Use this one-time maintenance bridge for a single existing release. Fresh installs
do not need it. This is not authorization to migrate ownership between two GKE
releases or to use legacy GKE rollout wrappers with the new chart.

1. Back up, stop new work, drain builds, and quiesce every database consumer.
   Using the exact previous chart and complete previous values, create a successful
   maintenance revision with `migration.render=false`, all consumer replicas zero,
   and external replica management/autoscalers disabled or separately quiesced.
   Wait for consumer Pods to stop and all migration attempts to be terminal.
   Do not combine this step with the hook-enabled upgrade.
2. Archive that quiesced revision's values, manifest and Job evidence. Construct a
   temporary static bridge chart from its ordinary manifest, omitting only the
   migrator ServiceAccount and optional migrator SecretProviderClass. Keep every
   other resource, namespace, name, image, replica count and PVC unchanged. For
   example, after setting `RELEASE`, `COYOTE_NAMESPACE`, `PREVIOUS_CHART`, and the
   verified prerequisite names (the defaults below may have been customized):

   ```sh
   umask 077
   BRIDGE="$(mktemp -d /tmp/coyote-hook-bridge.XXXXXX)"
   mkdir -p "$BRIDGE/templates"
   MIGRATOR_SA=coyote-migrate
   MIGRATOR_SPC=coyote-migrate-secrets
   helm show chart "$PREVIOUS_CHART" >"$BRIDGE/Chart.yaml"
   helm get manifest "$RELEASE" -n "$COYOTE_NAMESPACE" >"$BRIDGE/previous.yaml"
   ruby -ryaml - "$BRIDGE/previous.yaml" "$MIGRATOR_SA" "$MIGRATOR_SPC" >"$BRIDGE/resources.yaml" <<'RUBY'
   documents = YAML.load_stream(File.read(ARGV[0])).compact
   identities = [["ServiceAccount", ARGV[1]], ["SecretProviderClass", ARGV[2]]]
   removed, retained = documents.partition do |document|
     identities.include?([document["kind"], document.dig("metadata", "name")])
   end
   abort "expected exactly one migrator ServiceAccount" unless removed.count { |document| document["kind"] == "ServiceAccount" } == 1
   puts retained.map(&:to_yaml).join
   RUBY
   printf '{{ .Files.Get "resources.yaml" }}\n' >"$BRIDGE/templates/resources.yaml"
   helm template "$RELEASE" "$BRIDGE" -n "$COYOTE_NAMESPACE" >"$BRIDGE/review.yaml"
   ```

3. Review the rendered bridge against the archived manifest: only those prerequisite
   resource entries may disappear. Apply it as a separate successful revision of
   the same release: `helm upgrade "$RELEASE" "$BRIDGE" -n "$COYOTE_NAMESPACE"`.
   Confirm with `helm get manifest` that neither prerequisite remains an ordinary
   resource, and confirm their absence from the API. Verify consumers remain
   stopped and PVC/Secret identities are unchanged. Deleting live resources or
   changing their annotations alone does not remove the old manifest entries.
   Do not edit Helm's stored release Secrets.
4. Upgrade to the hook-enabled chart using complete desired values and the immutable
   release overlay, `migration.maintenanceAcknowledged=true`, and `--wait --timeout
   15m`. Do not reuse the bridge's empty values. The prerequisite hooks now have no
   old ordinary entries for Helm to prune. Confirm successful hook order, retained
   ServiceAccount/SecretProviderClass, and workload health before ending maintenance.
   If the hook fails, keep consumers stopped, inspect retained evidence and retry
   the same immutable target after resolving the cause. Do not roll back across
   this lifecycle boundary as a recovery shortcut.

The Kind suite verifies direct success/failure/retry, bridge success/failure/retry,
and a second hook-only upgrade in both native and CSI resource fixtures. Its
full native test also runs the actual previous chart through the bridge using
PostgreSQL and the real migrator. CSI coverage uses a local CRD stub and native
fixture credentials: it verifies rendered identity/Helm lifecycle only, not the
GKE driver, Secret Manager, Workload Identity, IAM or Cloud SQL runtime.

### Rollback and established GKE deployments

There is no automatic database rollback. Helm rollback restores application
resources only; use it only for binaries certified compatible with the current
schema. Otherwise prefer a forward fix or a separately planned consistent restore.
Manual `make db-migrate-down-one` is an explicit operator action, never a deployment
recovery default. Back up database and artifact state together.

Legacy GKE rollout/adoption scripts use manual `migration.render/runID` controls
and must remain on their historical chart revision. This chart rejects those
controls. Moving an established two-release deployment to automatic hooks/unified
ownership needs a separately approved maintenance plan; no ownership changes or
production operations are part of this implementation.
