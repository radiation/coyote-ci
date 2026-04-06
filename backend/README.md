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
```

## Testing

```bash
cd backend
go test ./...
```

Tests use in-memory repository implementations by default. Integration tests
that require Postgres are gated behind `DB_HOST` being set.

## API documentation

With the server running, visit http://localhost:8080/swagger/ for the Swagger UI.

## Configuration

All configuration is via environment variables. See `../.env.example` for the
full list with defaults and descriptions.
