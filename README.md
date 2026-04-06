# Coyote CI

[![CI](https://github.com/radiation/coyote-ci/actions/workflows/ci.yml/badge.svg)](https://github.com/radiation/coyote-ci/actions/workflows/ci.yml)
[![Frontend CI](https://github.com/radiation/coyote-ci/actions/workflows/frontend-ci.yml/badge.svg)](https://github.com/radiation/coyote-ci/actions/workflows/frontend-ci.yml)
[![CodeQL](https://github.com/radiation/coyote-ci/actions/workflows/codeql.yml/badge.svg)](https://github.com/radiation/coyote-ci/actions/workflows/codeql.yml)
[![Dependency Scan](https://github.com/radiation/coyote-ci/actions/workflows/dependency-scan.yml/badge.svg)](https://github.com/radiation/coyote-ci/actions/workflows/dependency-scan.yml)
[![Lint](https://github.com/radiation/coyote-ci/actions/workflows/lint.yml/badge.svg)](https://github.com/radiation/coyote-ci/actions/workflows/lint.yml)
[![Actionlint](https://github.com/radiation/coyote-ci/actions/workflows/actionlint.yml/badge.svg)](https://github.com/radiation/coyote-ci/actions/workflows/actionlint.yml)
[![codecov](https://codecov.io/gh/radiation/coyote-ci/branch/main/graph/badge.svg)](https://codecov.io/gh/radiation/coyote-ci)
[![Go 1.26](https://img.shields.io/badge/go-1.26-00ADD8.svg)](backend/go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Coyote CI is a greenfield CI/orchestration system focused on a small, correct, and understandable core.

## Lifecycle model

- Build lifecycle: pending -> queued -> running -> success|failed
- Step lifecycle: pending -> running -> success|failed
- Workers claim and complete steps; build status is derived/reconciled from step outcomes.
- Terminal states are immutable and stale worker completions are rejected by guarded repository updates.

See [backend/docs/state-machine.md](backend/docs/state-machine.md) for the full state machine, transition guards, and invariants.

## What's in this repo

- Go backend control plane
- Worker process
- Postgres-backed persistence
- Layered architecture (domain, repository, service, handlers)
- Docker Compose local development

## Prerequisites

- Docker + Docker Compose
- Go 1.26+ (see `backend/go.mod` for exact toolchain version)
- Node.js 22+ (frontend)

## Go version policy

**Source of truth:** `backend/go.mod` (`go` and `toolchain` directives).

CI reads `go.mod` directly (`go-version-file: backend/go.mod`). The Dockerfile has a matching default so standalone builds work without extra args.

For Docker Compose, `.env` contains a `GO_VERSION` override that is passed as a build arg. This must stay in sync with `go.mod`.

To update Go:

1. Update `backend/go.mod` (`go` + `toolchain` lines)
2. Update `GO_VERSION` in `.env`
3. Update `.coyote/pipeline.yml` image tag
4. Update the `ARG GO_VERSION` default in `backend/Dockerfile`
5. Run `make check-go-version` to verify consistency

## Quick start

```bash
cp .env.example .env   # set GITHUB_WEBHOOK_SECRET and review defaults
docker compose up --build
```

The default `.env` sets `COMPOSE_PROFILES=dev`, which starts:

| Service        | Description                          | Address               |
|----------------|--------------------------------------|-----------------------|
| db             | PostgreSQL 17                        | localhost:5432        |
| migrate        | Applies schema migrations on startup | —                     |
| backend-dev    | Go backend with hot reload (Air)     | http://localhost:8080 |
| worker         | Build step executor                  | —                     |
| frontend-dev   | Vite dev server with HMR             | http://localhost:3000 |

For production-like images instead:

```bash
COMPOSE_PROFILES=prod docker compose up --build
```

This swaps `backend-dev`/`frontend-dev` for pre-built `backend`/`frontend` containers.

## Queue Fixture Scenarios (Repo Pipeline Path)

Use the repo-backed build endpoint with `pipeline_path` to queue different scenarios from the same repository.

Set common values once:

```bash
API_URL="http://localhost:8080"
FIXTURE_REPO_URL="https://github.com/radiation/coyote-ci-fixtures.git"
FIXTURE_REF="main"
PROJECT_ID="fixtures"
```

Queue each scenario:

```bash
curl -sS -X POST "$API_URL/api/builds/repo" \
	-H "Content-Type: application/json" \
	-d '{
		"project_id": "'"$PROJECT_ID"'",
		"repo_url": "'"$FIXTURE_REPO_URL"'",
		"ref": "'"$FIXTURE_REF"'",
		"pipeline_path": "scenarios/success-basic/coyote.yml"
	}'
```

```bash
curl -sS -X POST "$API_URL/api/builds/repo" \
	-H "Content-Type: application/json" \
	-d '{
		"project_id": "'"$PROJECT_ID"'",
		"repo_url": "'"$FIXTURE_REPO_URL"'",
		"ref": "'"$FIXTURE_REF"'",
		"pipeline_path": "scenarios/failure-exit-1/coyote.yml"
	}'
```

```bash
curl -sS -X POST "$API_URL/api/builds/repo" \
	-H "Content-Type: application/json" \
	-d '{
		"project_id": "'"$PROJECT_ID"'",
		"repo_url": "'"$FIXTURE_REPO_URL"'",
		"ref": "'"$FIXTURE_REF"'",
		"pipeline_path": "scenarios/logs-long-running/coyote.yml"
	}'
```

```bash
curl -sS -X POST "$API_URL/api/builds/repo" \
	-H "Content-Type: application/json" \
	-d '{
		"project_id": "'"$PROJECT_ID"'",
		"repo_url": "'"$FIXTURE_REPO_URL"'",
		"ref": "'"$FIXTURE_REF"'",
		"pipeline_path": "scenarios/artifacts-basic/coyote.yml"
	}'
```

```bash
curl -sS -X POST "$API_URL/api/builds/repo" \
	-H "Content-Type: application/json" \
	-d '{
		"project_id": "'"$PROJECT_ID"'",
		"repo_url": "'"$FIXTURE_REPO_URL"'",
		"ref": "'"$FIXTURE_REF"'",
		"pipeline_path": "scenarios/multi-step-failure/coyote.yml"
	}'
```

Expected response fields for repo-backed fixture builds:

- `data.pipeline_source` is `"repo"`
- `data.pipeline_path` matches the requested scenario path
- `data.status` is usually `"queued"` at creation time

For a faster workflow, use `scripts/run-fixtures.sh` to queue all scenarios or a single scenario.

Migrations are applied automatically during `docker compose up` by a one-shot `migrate` service before backend and worker start. The Postgres container itself does not run schema SQL directly.

Security note: the worker mounts `/var/run/docker.sock` for local Docker-based step execution. This effectively grants root-equivalent host access to processes in the worker container. Treat this compose setup as trusted local development only, and avoid using it unchanged in less-trusted or shared environments.

To run migrations manually:

```bash
docker compose run --rm migrate
```

## Worker Internal Status Endpoint

The worker can expose a small internal status server with recovery counters.

Set `WORKER_STATUS_ADDR` to enable it (empty by default, disabled):

```bash
WORKER_STATUS_ADDR=127.0.0.1:9091
```

When enabled, the worker serves:

- `GET /healthz` returns `ok`
- `GET /internal/status/worker` returns JSON with worker recovery counters and `timestamp_utc`

Current counters include:

- `claims_won`
- `reclaims_won`
- `renewals_won`
- `renewals_stale`
- `stale_completion_rejected`
- `reclaim_misses`

This endpoint is intended for internal observability only and is not exposed by the backend API router.

## Docker Compose profiles

The compose file uses two profiles to avoid port conflicts:

| Profile | Services started                                 | Use case                    |
|---------|--------------------------------------------------|-----------------------------|
| `dev`   | db, migrate, **backend-dev**, worker, **frontend-dev** | Active local development    |
| `prod`  | db, migrate, **backend**, worker, **frontend**         | Production-like validation  |

Shared infrastructure (`db`, `migrate`, `worker`) has no profile and starts with either.

The default profile is set via `COMPOSE_PROFILES` in `.env`. Change it to `prod` when you want to test built images.

## Local development

The dev profile mounts source directories into the containers so changes are reflected immediately:

- **backend-dev** uses [Air](https://github.com/air-verse/air) to rebuild and restart on Go file changes.
- **frontend-dev** runs the Vite dev server with HMR.

If you only need the backend:

```bash
docker compose up --build db backend-dev worker
```

### Running tests locally

Backend:

```bash
cd backend && go test ./...
```

Frontend:

```bash
cd frontend && npm test -- --run
```

## Git hooks

Hooks are stored in `.githooks/` and checked into source control.

### Install

```bash
make install-hooks
```

This sets `core.hooksPath` for this clone. Hooks are `#!/usr/bin/env sh` and work on macOS and Linux.

### What runs when

| Hook         | When             | What                                                                  | Speed   |
|--------------|------------------|-----------------------------------------------------------------------|---------|
| `pre-commit` | `git commit`     | `gofmt`, `go vet`, `golangci-lint`, ESLint, swagger doc regeneration  | Seconds |
| `pre-push`   | `git push`       | `go test ./...`, `vitest run`                                         | Minutes |

Both hooks gracefully skip checks when the required tool is not installed.

### Bypass

```bash
git commit --no-verify   # skip pre-commit
git push --no-verify     # skip pre-push
```

CI remains the enforcement layer.

## Quality gates

CI includes:

- backend workflow ([.github/workflows/ci.yml](.github/workflows/ci.yml)): `gofmt`, `go vet`, tests with coverage, `golangci-lint`
- frontend workflow ([.github/workflows/frontend-ci.yml](.github/workflows/frontend-ci.yml)): `vitest` with coverage, `eslint`, `vite build`
- actions workflow linting (`actionlint`)
- CodeQL analysis
- dependency vulnerability scan (`govulncheck`)

### Coverage

Both backend and frontend upload coverage to [Codecov](https://codecov.io/gh/radiation/coyote-ci) with separate flags (`backend`, `frontend`). Configuration lives in [codecov.yml](codecov.yml).

PRs that only touch one side carry forward the other side's coverage automatically (`carryforward: true`), so coverage status checks remain meaningful on partial changes.

## Notes

- Badge URLs currently reference `radiation/coyote-ci`. If this repository is under a different owner/name, update those links.