# Coyote CI on GCP Cloud SQL (PostgreSQL)

This guide shows one deployment profile for running Coyote CI against externally managed PostgreSQL on GCP Cloud SQL.

Coyote runtime requirements stay the same across providers:

- Backend and worker require PostgreSQL for durable state.
- In-memory repositories are not durable and are intended for tests/non-production scenarios.
- Managed database provider choice is a deployment concern.

## Required runtime environment

Set these environment variables for both backend and worker:

- `DATABASE_URL` (recommended)
- `GITHUB_WEBHOOK_SECRET`

Example:

```bash
DATABASE_URL='postgres://coyote_app:${DB_PASSWORD}@127.0.0.1:5432/coyote_ci?sslmode=disable'
DB_MAX_OPEN_CONNS=20
DB_MAX_IDLE_CONNS=10
DB_CONN_MAX_LIFETIME=30m
DB_CONN_MAX_IDLE_TIME=5m
GITHUB_WEBHOOK_SECRET='replace-me'
```

Notes:

- Use `DATABASE_URL` for external Postgres. Split `DB_*` fields are still supported, but `DATABASE_URL` is clearer for managed deployments.
- Tune pool values to your Cloud SQL instance size and expected concurrency.

## Recommended connection approach: Cloud SQL Auth Proxy

Use the Cloud SQL Auth Proxy sidecar/process and connect locally from Coyote containers/processes.

1. Start Cloud SQL Auth Proxy with IAM credentials for your Cloud SQL instance.
2. Bind proxy to `127.0.0.1:5432` (or another local port).
3. Point `DATABASE_URL` host/port to that local proxy endpoint.

Why this is preferred:

- Keeps app runtime config simple (`DATABASE_URL`).
- Avoids embedding provider-specific socket logic in application code.
- Works well with local Docker, VM, or service-based deployments.

## Alternative: private IP direct connection

If your deployment runs in the same VPC and network policy allows it, you can connect to Cloud SQL private IP directly.

- Set `DATABASE_URL` host to the private IP / DNS endpoint.
- Use `sslmode=require` (or stricter) according to your network and cert posture.
- Keep credentials in secret storage, not committed files.

## Secrets handling expectations

- Do not commit DB credentials or webhook secrets.
- Inject secrets via your deploy platform (Secret Manager, CI/CD secret variables, or runtime secret mounts).
- Rotate database credentials and webhook secret on a regular schedule.

## Migrations and startup

- Run Goose migrations from `backend/db/migrations` before starting backend/worker.
- Migration history is tracked by Goose in the target database.

Recommended operator sequence (current repo model):

1. Bring up Cloud SQL and confirm connectivity from your runtime network.
2. Run migrations once in a controlled job/process.
3. Start Coyote backend.
4. Start Coyote worker(s).

Avoid multi-replica first-start races:

- Do not have multiple backend/worker replicas all attempt first-time schema setup concurrently.
- Use a single migration step in CI/CD or release automation, then roll out app replicas.
- If rollout and migration are coupled, gate app startup on migration success.

## Operations notes

- Backups, point-in-time recovery, and HA are managed at the Postgres provider layer (Cloud SQL in this profile).
- Coyote expects durable PostgreSQL semantics and does not provide its own database durability layer.
- Monitor connection counts and saturation (`max_connections`) and adjust pool sizing/env vars as needed.
