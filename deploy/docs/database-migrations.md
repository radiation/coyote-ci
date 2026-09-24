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
contains Goose `v3.24.1` and the tracked `backend/db/migrations` directory. It
accepts `DATABASE_URL` or `DATABASE_URL_FILE`; the file value takes precedence,
matching the application database configuration.

Build it directly when preparing a release image:

```bash
docker build --target migrate-runtime -t coyote-migrate:local ./backend
```

The image runs `goose up` by default. Running it against an already-current
database exits successfully.

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

The future Kubernetes migration Job must use the immutable `migrate-runtime`
image, mount the database DSN as `DATABASE_URL_FILE`, and run alongside a Cloud
SQL Auth Proxy sidecar that listens on the DSN's loopback host and port. A
successful Job completion is required before the server Deployment rolls out.
