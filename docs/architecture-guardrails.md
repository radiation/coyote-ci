# Executable Architecture Guardrails V1

This repo now has a small set of executable architecture checks for the current backend and frontend structure. The rules are intentionally descriptive, not aspirational.

## Current backend dependency direction

- `backend/cmd/server` and `backend/cmd/worker` are composition roots.
- `backend/internal/http/handler` is the HTTP edge.
- `backend/internal/service` and focused service subpackages own business logic.
- `backend/internal/repository` contains repository interfaces.
- `backend/internal/repository/postgres` and `backend/internal/repository/memory` are concrete persistence adapters.
- `backend/internal/domain` contains core business types and helpers.

### Enforced backend rules

- Domain must not import HTTP, services, concrete repositories, or command packages.
- HTTP handlers must not import concrete Postgres or memory repositories.
- Services must not import HTTP, command packages, or concrete Postgres or memory repositories.
- Concrete Postgres and memory repositories must not import HTTP, services, or command packages.

These checks run in [backend/architecture/guardrails_test.go](backend/architecture/guardrails_test.go) via `go test`, so they are already included in `cd backend && go test ./...`.

### Backend exclusions in V1

- Packages such as `internal/auth`, `internal/platform`, `internal/observability`, `internal/source`, `internal/pipeline`, and other support packages are not forced into a layer yet.
- Deprecated Arch-Go cycle rules are not enabled in V1.

Those areas are intentionally left out because they currently span infrastructure or cross-cutting concerns, and broad rules there would create noisy exceptions faster than useful protection.

## Current frontend dependency direction

- `frontend/src/routes` composes pages into the router.
- `frontend/src/pages` owns page-level data loading and interaction flow.
- `frontend/src/components` provides reusable UI building blocks.
- `frontend/src/api` owns network calls and request helpers.
- `frontend/src/queries` contains reusable React Query helpers.
- `frontend/src/utils`, `frontend/src/types`, and a few root contracts are shared support code.
- `frontend/src/test` and `*.test.*` files are test-only support.

### Enforced frontend rules

- API modules must not import pages, components, or routes.
- Query helpers must not import pages, components, or routes.
- Components must not import pages or routes.
- Shared utilities and types must not import pages, components, or routes.
- Production modules must not import test files or test helpers.

These rules run through the existing ESLint config, so `cd frontend && npm run lint` is the main frontend architecture check.

### Frontend exclusions in V1

- The guardrails do not require every source file to belong to a declared element type.
- Public-entrypoint enforcement for flat folders such as `src/api` is deferred.

That keeps the baseline small enough to match the current tree without broad cleanup.

## Local commands

- `cd backend && go test ./architecture`
- `cd backend && go test ./...`
- `cd frontend && npm run lint`

## Narrow exceptions

If a new exception is justified:

- prefer changing one exact package or element rule rather than adding a broad wildcard
- add a short comment beside the exception explaining why it is intentional
- keep the rule aligned to architecture the repo already uses today

New guardrails should describe agreed structure that already exists. They should not be used to force speculative refactors.