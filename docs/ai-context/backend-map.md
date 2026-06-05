# Coyote CI Backend Map

Use this file to narrow backend changes to the smallest relevant file set before scanning packages.

Start with this file, `docs/ai-context/current-priorities.md`, `docs/ai-context/product-context.md`, and `.github/copilot-instructions.md`. Source code, migrations, and tests remain authoritative.

## Entry points

- `backend/cmd/server/main.go`: composition root for the HTTP API, storage, auth, services, and repositories.
- `backend/cmd/worker/main.go`: worker process entry point for queue claiming and build execution.
- `backend/internal/http/router.go`: request routing surface before dropping into handlers.

## Main package map

- `backend/internal/http/handler`: thin HTTP handlers. Start here when changing request parsing, response shaping, authz checks at the edge, or endpoint wiring.
- `backend/internal/domain`: core business types and lifecycle helpers such as builds, steps, jobs, projects, queue items, workers, artifacts, source specs, users, memberships, and version tags.
- `backend/internal/service`: orchestration and business rules. This is usually the first stop for behavioral changes.
- `backend/internal/repository`: persistence logic, Postgres adapters, in-memory adapters, and transaction-safe state updates.
- `backend/db/migrations`: additive schema history. Add new numbered migrations here; do not edit applied migrations.
- `backend/internal/artifact`: artifact key resolution and blob-store adapters.
- `backend/internal/platform/config`: env loading for `EMAIL_NOTIFICATIONS_ENABLED`, `EMAIL_NOTIFICATION_RECIPIENTS`, and SMTP settings.
- `backend/internal/platform/email`: SMTP sender plumbing for local Mailpit development and future email notification delivery.
- `backend/internal/service/build/notifications.go`: terminal build email formatting/delivery for failed and successful builds, durable per-recipient delivery tracking/dedupe, and the dev-only sample send path.
- `backend/internal/repository/notification_delivery_repository.go`, `backend/internal/repository/memory/notification_delivery_repository.go`, `backend/internal/repository/postgres/notification_delivery_repository.go`: notification delivery persistence for build email attempts and future delivery-state inspection.
- `backend/internal/source`: source fetch/materialization, PR/source metadata, and repo writeback helpers.
- `backend/internal/versioning`: release/version-tag behavior and lineage helpers.
- `backend/internal/auth`: session/auth-mode logic and authorization helpers.
- `backend/internal/runner`, `backend/internal/workspace`, `backend/internal/pipeline`: execution support code used by build and worker flows.

## Common change areas

- API/handlers: start in `backend/internal/http/handler`, then follow the handler into the owning service.
- Domain/model concepts: start in `backend/internal/domain`, then inspect the repository and service that own the relevant state transition.
- Services/business logic: start in `backend/internal/service`. Useful subareas are `service/build`, `service/execution`, `service/worker`, and `service/artifact`.
- Repositories/persistence: start in `backend/internal/repository`, then inspect `repository/postgres` or `repository/memory` only if the behavior differs by storage backend.
- Migrations: inspect `backend/db/migrations` and only the repositories touched by the schema change.
- Queue/workers/build execution: start with `backend/internal/service/build`, `backend/internal/service/worker`, `backend/internal/service/execution`, then `backend/cmd/worker` if process wiring matters.
- Build email notifications: start with `backend/internal/platform/config/config.go`, `backend/internal/platform/email/sender.go`, `backend/internal/service/build/notifications.go`, `backend/internal/service/build/lifecycle.go`, `backend/internal/service/build/completion.go`, `backend/internal/service/build/source.go`, `backend/internal/repository/notification_delivery_repository.go`, `backend/db/migrations`, `backend/cmd/server/main.go`, `backend/cmd/worker/main.go`, `backend/internal/http/handler/notification_handler.go`, and `backend/internal/http/router.go` for the dev-only sample route.
- Artifacts/provenance/source linking: start with `backend/internal/domain/artifact.go`, `backend/internal/service/build/artifacts.go`, `backend/internal/repository/artifact_repository.go`, `backend/internal/repository/artifact_label_repository.go`, `backend/internal/api/artifact_dto.go`, `backend/internal/http/handler/artifact_handler.go`, `backend/internal/versioning/artifact_template.go`, and `backend/internal/source`.
- Auth/RBAC/API tokens: start with `backend/internal/auth`, `backend/internal/service/api_token_service.go`, `backend/internal/service/project_membership_service.go`, and matching handlers/repositories.

## Focused docs worth checking first

- `backend/docs/state-machine.md`: lifecycle/state-machine intent before changing build or step transitions.
- `backend/docs/artifact-model.md`: artifact metadata and lineage concepts before changing artifact behavior.
- `backend/docs/swagger.yaml`: generated API contract for artifact browse/detail/version endpoints before scanning handlers for frontend/API work.
- `backend/docs/api-tokens.md`: API token behavior and expectations.

## Tests

- Backend tests are mostly colocated as `*_test.go` beside handlers, services, repositories, domain helpers, and package-level behavior.
- For endpoint work, start with the matching handler test in `backend/internal/http/handler`.
- For workflow/state changes, prefer the closest service or repository test before scanning unrelated packages.