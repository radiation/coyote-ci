# Coyote CI

[![CI](https://github.com/radiation/coyote-ci/actions/workflows/ci.yml/badge.svg)](https://github.com/radiation/coyote-ci/actions/workflows/ci.yml)
[![CodeQL](https://github.com/radiation/coyote-ci/actions/workflows/codeql.yml/badge.svg)](https://github.com/radiation/coyote-ci/actions/workflows/codeql.yml)
[![Dependency Scan](https://github.com/radiation/coyote-ci/actions/workflows/dependency-scan.yml/badge.svg)](https://github.com/radiation/coyote-ci/actions/workflows/dependency-scan.yml)
[![Lint](https://github.com/radiation/coyote-ci/actions/workflows/lint.yml/badge.svg)](https://github.com/radiation/coyote-ci/actions/workflows/lint.yml)
[![Actionlint](https://github.com/radiation/coyote-ci/actions/workflows/actionlint.yml/badge.svg)](https://github.com/radiation/coyote-ci/actions/workflows/actionlint.yml)
[![codecov](https://codecov.io/gh/radiation/coyote-ci/branch/main/graph/badge.svg)](https://codecov.io/gh/radiation/coyote-ci)
[![Go Version](https://img.shields.io/badge/go-1.26%2B-00ADD8.svg)](https://go.dev/dl/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Coyote CI is a greenfield CI/orchestration system focused on a small, correct, and understandable core.

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

## Quality gates

CI includes:

- formatting check (`gofmt`)
- `go vet`
- tests with coverage profile
- `golangci-lint`
- `actionlint`
- CodeQL analysis
- dependency vulnerability scan (`govulncheck`)

Coverage artifacts are uploaded from CI, and coverage is published to Codecov.

## Notes

- Badge URLs currently reference `radiation/coyote-ci`. If this repository is under a different owner/name, update those links.