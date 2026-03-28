# Coyote CI

[![CI](https://github.com/radiation/coyote-ci/actions/workflows/ci.yml/badge.svg)](https://github.com/radiation/coyote-ci/actions/workflows/ci.yml)
[![Frontend CI](https://github.com/radiation/coyote-ci/actions/workflows/frontend-ci.yml/badge.svg)](https://github.com/radiation/coyote-ci/actions/workflows/frontend-ci.yml)
[![CodeQL](https://github.com/radiation/coyote-ci/actions/workflows/codeql.yml/badge.svg)](https://github.com/radiation/coyote-ci/actions/workflows/codeql.yml)
[![Dependency Scan](https://github.com/radiation/coyote-ci/actions/workflows/dependency-scan.yml/badge.svg)](https://github.com/radiation/coyote-ci/actions/workflows/dependency-scan.yml)
[![Lint](https://github.com/radiation/coyote-ci/actions/workflows/lint.yml/badge.svg)](https://github.com/radiation/coyote-ci/actions/workflows/lint.yml)
[![Actionlint](https://github.com/radiation/coyote-ci/actions/workflows/actionlint.yml/badge.svg)](https://github.com/radiation/coyote-ci/actions/workflows/actionlint.yml)
[![codecov](https://codecov.io/gh/radiation/coyote-ci/branch/main/graph/badge.svg)](https://codecov.io/gh/radiation/coyote-ci)
[![Go Version](https://img.shields.io/badge/go-1.26%2B-00ADD8.svg)](https://go.dev/dl/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Coyote CI is a greenfield CI/orchestration system focused on a small, correct, and understandable core.

## Lifecycle model

- Build lifecycle: pending -> queued -> running -> success|failed
- Step lifecycle: pending -> running -> success|failed
- Workers claim and complete steps; build status is derived/reconciled from step outcomes.
- Terminal states are immutable and stale worker completions are rejected by guarded repository updates.

See [backend/docs/state-machine.md](docs/state-machine.md) for the full state machine, transition guards, and invariants.

## What's in this repo

- Go backend control plane
- Worker process
- Postgres-backed persistence
- Layered architecture (domain, repository, service, handlers)
- Docker Compose local development

## Prerequisites

- Docker + Docker Compose
- Go 1.26+ (for local non-container workflows)

## Quick start

Start Postgres + backend + worker:

```bash
docker compose up --build
```

Backend API is exposed on http://localhost:8080.

Migrations are applied automatically during `docker compose up` by a one-shot `migrate` service before backend and worker start. The Postgres container itself does not run schema SQL directly.

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

## Local dev with live reload (Go)

Go binaries are compiled, so source file changes are not automatically picked up by a running binary. For development, this repo includes a hot-reload profile using Air.

Start DB + hot-reload backend:

```bash
docker compose --profile dev up --build db backend-dev
```

This mounts [backend](backend) into the container and rebuilds/restarts on file changes.

If you need the worker in parallel, run it in a second command:

```bash
docker compose up --build worker
```

## Git hooks setup

This repository stores Git hooks in source control under `.githooks` so contributors get consistent local checks.

Enable the hooks path for this clone:

```bash
git config core.hooksPath .githooks
```

If needed, mark the hook as executable:

```bash
chmod +x .githooks/pre-commit
```

The pre-commit hook runs backend format/vet/lint checks, regenerates Swagger docs, and stages `backend/docs`. CI remains the enforcement layer.

## Quality gates

CI includes:

- backend workflow ([.github/workflows/ci.yml](.github/workflows/ci.yml)): `gofmt`, `go vet`, tests with coverage, `golangci-lint`
- frontend workflow ([.github/workflows/frontend-ci.yml](.github/workflows/frontend-ci.yml)): `npm test -- --run`, `npm run lint`, `npm run build`
- actions workflow linting (`actionlint`)
- CodeQL analysis
- dependency vulnerability scan (`govulncheck`)

Coverage artifacts are uploaded from CI, and coverage is published to Codecov.

## Notes

- Badge URLs currently reference `radiation/coyote-ci`. If this repository is under a different owner/name, update those links.