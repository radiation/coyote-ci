# Coyote CI — Backend

Go backend for Coyote CI providing the control-plane API server and build-step worker.

## Architecture

```
cmd/
  server/       HTTP API server (port 8080)
  worker/       Build-step execution worker (polls for work)

internal/
  api/          HTTP request/response DTOs
  artifact/     Artifact storage (filesystem, GCS)
  domain/       Core business types (Build, BuildStep, Job, …)
  http/         Router and handler layer (chi)
  logs/         Log sinks (postgres, memory, noop)
  observability/Metrics collection
  pipeline/     Pipeline YAML parsing and validation
  platform/     Configuration and database setup
  repository/   Persistence (memory + postgres implementations)
  runner/       Step execution backends (docker, inprocess)
  service/      Business logic and orchestration
  source/       Git source integration
  webhook/      Webhook ingestion (GitHub)
  workspace/    Build workspace management
```

See [docs/state-machine.md](docs/state-machine.md) for build/step lifecycle details.

## Prerequisites

- Go 1.26+
- Docker & Docker Compose
- PostgreSQL 17 (provided via Compose)

## Quick start (Docker Compose)

From the repository root:

```bash
# Copy env template and fill in secrets
cp .env.example .env

# Start everything (dev mode with hot-reload)
docker compose --profile dev up --build
```

Services:

| Service       | Address             |
|---------------|---------------------|
| API server    | http://localhost:8080 |
| Frontend      | http://localhost:3000 |
| PostgreSQL    | localhost:5432       |

## Running locally (without Compose)

```bash
# Start Postgres and run migrations
# (or use `docker compose up db migrate`)

cd backend
cp ../.env.example ../.env   # adjust DB_HOST=localhost etc.
source ../.env

go run ./cmd/server   # API server
go run ./cmd/worker   # worker (separate terminal)
```

## Make targets

```
make swagger           # regenerate Swagger docs
make check-go-version  # verify Go version consistency
make db-migrate-status # goose migration status
make db-migrate-up     # apply pending migrations
make db-migrate-down-one # rollback one migration
make db-migrate-create name=<migration_name> # create new numbered SQL migration
```

Migrations live in `backend/db/migrations` and are managed with Goose.
Applied migrations are immutable; add new numbered migrations for schema changes.
See [../deploy/docs/database-migrations.md](../deploy/docs/database-migrations.md) for rollout and operator workflow.

## Testing

```bash
cd backend
go test ./...
```

Tests use in-memory repository implementations by default. Integration tests
that require Postgres are gated behind `DB_HOST` being set.

## Persistence support

- Durable runtime persistence is PostgreSQL only.
- In-memory repositories are for tests/non-durable scenarios.
- Managed Postgres provider selection is deployment-specific and stays outside core runtime logic.

## Artifact persistence support

- Artifact metadata is stored in PostgreSQL.
- Artifact blob bytes are stored in the configured artifact store.
- `filesystem` is the default artifact store for local/simple installs.
- `gcs` is supported for production object storage deployments.

## API documentation

With the server running, visit http://localhost:8080/swagger/ for the Swagger UI.

## Configuration

All configuration is via environment variables. See `../.env.example` for the
full list with defaults and descriptions.

Database configuration supports:

- `DATABASE_URL` (preferred for external/managed Postgres)
- or split fields: `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`, `DB_SSLMODE`

Optional connection pool settings:

- `DB_MAX_OPEN_CONNS`
- `DB_MAX_IDLE_CONNS`
- `DB_CONN_MAX_LIFETIME` (Go duration, e.g. `30m`)
- `DB_CONN_MAX_IDLE_TIME` (Go duration, e.g. `5m`)

Artifact storage configuration:

- `ARTIFACT_STORAGE_PROVIDER` (`filesystem` or `gcs`)
- `ARTIFACT_STORAGE_ROOT` (required for filesystem)
- `ARTIFACT_STORAGE_STRICT` (fail startup on gcs config/init errors when true)
- `ARTIFACT_GCS_BUCKET` (required when provider is `gcs`)
- `ARTIFACT_GCS_PREFIX` (optional)

Worker cache configuration:

- `WORKER_CACHE_STORAGE_ROOT` (local cache snapshot root; defaults to `$TMPDIR/coyote-cache`)
- `CACHE_MAX_SIZE_MB` (local cache retention limit; oldest entries are evicted when exceeded)
- `COYOTE_CACHE_PATH_BYTES_MODE` (optional cache path byte measurement mode: `off`/unset, `sample`, `always`; default is `off`)
- `COYOTE_CACHE_PATH_BYTES_SAMPLE_PERCENT` (optional sampling percentage for `sample` mode, `0-100`; default `10`)
- `COYOTE_CACHE_STORE_SIZE_MODE` (optional cache store total-size measurement mode: `off`/unset, `sample`, `always`; default is `off`)
- `COYOTE_CACHE_STORE_SIZE_SAMPLE_PERCENT` (optional sampling percentage for `sample` mode, `0-100`; default `10`)

Recommended production defaults:

- Keep `COYOTE_CACHE_PATH_BYTES_MODE` unset (or set to `off`) to avoid full cache-directory walks on every step restore/save.
- Use `COYOTE_CACHE_PATH_BYTES_MODE=sample` with a low `COYOTE_CACHE_PATH_BYTES_SAMPLE_PERCENT` (for example `1` to `5`) when you need periodic byte-size observability with lower overhead.
- Use `COYOTE_CACHE_PATH_BYTES_MODE=always` only for short debugging windows.
- Keep `COYOTE_CACHE_STORE_SIZE_MODE` unset (or set to `off`) to avoid expensive total-size scans on every cache save.
- Use `COYOTE_CACHE_STORE_SIZE_MODE=sample` with a low `COYOTE_CACHE_STORE_SIZE_SAMPLE_PERCENT` (for example `1` to `5`) for occasional capacity signals with limited overhead.
- Use `COYOTE_CACHE_STORE_SIZE_MODE=always` only for short debugging windows.

## Pipeline Step Cache DSL

Pipeline YAML supports first-class per-step cache configuration with pipeline-level defaults and step-level overrides.

Preset shorthand:

```yaml
version: 1
pipeline:
  image: golang:1.26.1
  cache:
    preset: golang
    scope: job
steps:
  - name: test
    run: go test ./...
```

Explicit form:

```yaml
version: 1
steps:
  - name: lint
    run: golangci-lint run
    cache:
      paths:
        - /root/.cache/golangci-lint
      scope: job
      key:
        files:
          - .golangci.yml
          - go.mod
          - go.sum
```

Semantics:

- `pipeline.cache` applies to all steps by default.
- `steps[].cache` fully overrides the pipeline default for that step.
- V1 supports scopes `build` and `job`.
- `build` scope is isolated to a single build.
- `job` scope is reusable across builds for the same job identity.
- Cache storage backend details are intentionally not part of YAML.

Validation rules:

- `scope` is required whenever `cache` is present.
- `paths` must be absolute container paths.
- `key.files` must be workspace-relative and must not escape the workspace.
- Unknown presets are rejected.
- Unknown fields are rejected by strict YAML decoding.

## Pipeline Parallel Groups

Execution semantics:

- Top-level `steps` execute in declaration order.
- `group.steps` execute in parallel.
- A group is a barrier: the next top-level step or group does not start until every step in the current group succeeds.
- Build/source preparation (workspace + checkout) runs once before any step can run.
- Step runners do not perform checkout/source-prep work.

Canonical grouped example:

```yaml
version: 1
pipeline:
  name: backend-and-frontend
steps:
  - name: prepare
    run: echo "build prep happens before this step"

  - group:
      name: verify
      steps:
        - name: backend test
          run: go test ./...
          working_dir: backend
        - name: frontend test
          run: npm test
          working_dir: frontend
          image: node:22-alpine

  - group:
      name: package
      steps:
        - name: backend image
          run: docker build -t coyote-ci/backend:latest -f backend/Dockerfile backend
          image: docker:27
        - name: frontend image
          run: docker build -t coyote-ci/frontend:latest -f frontend/Dockerfile frontend
          image: docker:27
```
