# Executable Architecture Guardrails

This repo now has executable architecture checks for the current backend and frontend structure, plus local and CI enforcement wired to the same commands. The rules are intentionally descriptive, not aspirational.

## Backend guardrails

Current backend direction:

- `backend/cmd/server` and `backend/cmd/worker` are composition roots.
- `backend/internal/http/handler` is the HTTP edge.
- `backend/internal/service` and focused service subpackages own business logic.
- `backend/internal/repository` contains repository interfaces.
- `backend/internal/repository/postgres` and `backend/internal/repository/memory` are concrete persistence adapters.
- `backend/internal/domain` contains core business types and helpers.
- `backend/internal/platform`, `backend/internal/observability`, and `backend/internal/logs` are treated as low-level support packages instead of controller layers.

Enforced backend rules:

- Domain must not import HTTP, services, concrete repositories, or command packages.
- HTTP handlers must not import concrete Postgres or memory repositories, including nested adapter packages.
- Services and nested service packages must not import HTTP, command packages, or concrete repositories.
- Repository interface packages must not import handlers, services, command packages, or concrete repository adapters.
- Concrete Postgres and memory repositories must not import HTTP, services, or command packages.
- Low-level `platform`, `observability`, and `logs` packages must not import upward into HTTP or command layers.
- Concrete Postgres and memory repositories may only be imported by the composition roots in `cmd/server` and `cmd/worker`.

These checks run in [the backend architecture tests](../backend/architecture/guardrails_test.go) and are available directly through `make backend-architecture`.

## Frontend guardrails

Current frontend direction:

- `frontend/src/App.tsx` and `frontend/src/main.tsx` are app-composition entrypoints.
- `frontend/src/auth*` and `frontend/src/theme*` are root provider and context modules.
- `frontend/src/routes` composes pages into the router.
- `frontend/src/pages` owns page-level orchestration, with explicit `page-support` modules for helpers, sections, and page-local forms.
- `frontend/src/components` provides reusable UI building blocks.
- `frontend/src/api` is the public API layer.
- `frontend/src/queries` contains reusable query helpers.
- `frontend/src/types`, `frontend/src/utils`, and `frontend/src/styles.css` are shared support.
- `frontend/src/test` and `*.test.*` files are test-only modules.

Enforced frontend rules:

- Every production source file must match a declared element type.
- App entrypoints may depend only on providers, routes, the public API layer, styles, and shared support.
- Routes may compose pages and shared UI support, but should not become a data-access layer.
- Pages may orchestrate components, page-support modules, queries, public API exports, auth, theme, and shared helpers.
- Components may depend on shared UI support, API exports, verified query helpers, auth, theme, and shared support, but not on pages or routes.
- API and query modules may depend only on API-local modules and shared support.
- Types and utilities must stay below UI and composition layers.
- Production modules must not import test modules or test support.
- Production imports into `src/api` must go through the public barrel instead of deep-importing private files such as `api/client`.

These rules run through [the frontend ESLint boundaries](../frontend/eslint.config.js), so `make frontend-lint` is the main frontend architecture check.

## Local enforcement

- `make backend-architecture`
- `make backend-test`
- `make frontend-lint`
- `make pre-push-check`
- `.githooks/pre-push`

The pre-push hook now reuses the same root commands for backend architecture checks, backend tests, frontend lint, frontend tests, frontend build, and Swagger drift detection.

## CI enforcement

- Backend CI uses [the backend CI workflow](../.github/workflows/ci.yml) and keeps `CI Summary` as the required summary check.
- Frontend CI uses [the frontend CI workflow](../.github/workflows/frontend-ci.yml) and now emits `Frontend Summary` even when frontend work is unchanged.
- Local Make targets are reused in CI for Swagger drift, backend formatting, backend architecture checks, backend vet, frontend lint, and frontend build.

Manual branch-protection guidance:

- Require `CI Summary`.
- Require `Frontend Summary`.
- Require `Backend lint`.
- Require `Lint GitHub Actions workflows`.
- Require `Go vulnerability scan`.
- Require `Analyze (go)`.

That set keeps GitHub Actions authoritative while still allowing path-aware job skipping behind summary checks.

## Narrow exceptions

If a new exception is justified:

- prefer changing one exact package or element rule rather than adding a broad wildcard
- add a short comment beside the exception explaining why it is intentional
- keep the rule aligned to architecture the repo already uses today

New guardrails should describe agreed structure that already exists. They should not be used to force speculative refactors.