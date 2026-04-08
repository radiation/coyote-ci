# Database Migrations Runbook

This repository uses Goose for PostgreSQL schema migrations.

## Location and rules

- Migrations live in `backend/db/migrations`.
- Applied migrations are immutable.
- Add new numbered migration files for schema changes.
- Do not edit old applied migration files.

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
