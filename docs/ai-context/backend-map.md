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
- `backend/internal/platform/config`: env loading for email/slack notification support, SMTP settings, and optional public build-detail base URL via `COYOTE_PUBLIC_URL` or `APP_BASE_URL`.
- `backend/internal/platform/email`: SMTP sender plumbing for local Mailpit development and future email notification delivery.
- `backend/internal/platform/slack/client.go`: Slack API client for `auth.test` workspace verification, live `users.lookupByEmail` member resolution, and personal `chat.postMessage` direct-message delivery.
- `backend/internal/service/build/notifications.go`, `backend/internal/service/build/notification_recovery.go`, `backend/internal/service/build/notification_recovery_drain.go`: terminal build email, shared Slack webhook, and personal Slack DM formatting/delivery for failed and successful builds, notification target/subscription resolution, commit-author personal preference evaluation, env email fallback for local/default setups, transport-aware logical delivery planning keyed by stable internal destination ids, claim-owner-based delivery attempts with bounded retries and stale-claim-safe state updates, deterministic recovery rehydration from persisted delivery references plus authoritative build data, and a server-owned periodic recovery drain for due retries and expired claims.
- `backend/internal/service/build/slack_sender.go`: small incoming-webhook sender abstraction used by build notifications for Slack-compatible webhook delivery.
- `backend/internal/service/notification_service.go`: admin-style notification target/subscription validation and CRUD orchestration for backend API management of email and Slack webhook targets plus scoped subscriptions, plus self-scoped commit-author email/Slack channel preference reads and writes.
- `backend/internal/service/user_slack_identity_service.go`: self-scoped personal Slack identity resolution, confirmation revalidation, enable/disable, unlink, and workspace-readiness semantics.
- `backend/internal/repository/notification_delivery_repository.go`, `backend/internal/repository/memory/notification_delivery_repository.go`, `backend/internal/repository/postgres/notification_delivery_repository.go`: notification delivery persistence for transport-aware logical deliveries keyed by `(build_id, event_type, transport, destination_key)`, with atomic claim acquisition, bounded recoverable-candidate scans, retry eligibility windows, stale-claim reclamation, bounded retry scheduling metadata, and lost-claim-safe sent/failure transitions. The scan only identifies candidates; the existing claim contract remains authoritative for cross-instance recovery.
- `backend/internal/repository/notification_subscription_repository.go`, `backend/internal/repository/memory/notification_subscription_repository.go`, `backend/internal/repository/postgres/notification_subscription_repository.go`: notification target/subscription persistence for both terminal build event lookup and admin CRUD/list operations; targets can be email addresses or Slack webhooks, and subscriptions scope them to projects or jobs.
- `backend/internal/repository/user_slack_identity_repository.go`, `backend/internal/repository/memory/user_slack_identity_repository.go`, `backend/internal/repository/postgres/user_slack_identity_repository.go`: one-user-to-one-Slack-member persistence and conflict enforcement for personal Slack identities.
- `backend/internal/repository/memory/slack_workspace_integration_repository.go`, `backend/internal/repository/postgres/slack_workspace_integration_repository.go`: instance-level Slack workspace persistence plus linked-identity dependency checks for disconnect/switch safety.
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
- Build notifications: start with `backend/internal/platform/config/config.go`, `backend/internal/platform/email/sender.go`, `backend/internal/platform/slack/client.go`, `backend/internal/service/build/notifications.go`, `backend/internal/service/build/slack_sender.go`, `backend/internal/service/notification_service.go`, `backend/internal/service/build/lifecycle.go`, `backend/internal/service/build/completion.go`, `backend/internal/service/build/source.go`, `backend/internal/repository/notification_delivery_repository.go`, `backend/internal/repository/notification_subscription_repository.go`, `backend/internal/repository/user_notification_preference_repository.go`, `backend/internal/repository/user_slack_identity_repository.go`, `backend/internal/repository/slack_workspace_integration_repository.go`, `backend/db/migrations`, `backend/cmd/server/main.go`, `backend/cmd/worker/main.go`, `backend/internal/http/handler/notification_handler.go`, and `backend/internal/http/router.go` for admin notification management endpoints, personal commit-author preference endpoints, and the dev-only sample route.
- Personal Slack identity linking and DM delivery: start with `backend/internal/service/user_slack_identity_service.go`, `backend/internal/service/notification_service.go`, `backend/internal/service/build/notifications.go`, `backend/internal/http/handler/notification_personal_slack_identity_handler.go`, `backend/internal/http/handler/notification_handler.go`, `backend/internal/api/notification_dto.go`, `backend/internal/platform/slack/client.go`, `backend/internal/repository/user_notification_preference_repository.go`, `backend/internal/repository/user_slack_identity_repository.go`, `backend/internal/repository/memory/slack_workspace_integration_repository.go`, `backend/internal/repository/postgres/slack_workspace_integration_repository.go`, `backend/internal/repository/memory/user_slack_identity_repository.go`, `backend/internal/repository/postgres/user_slack_identity_repository.go`, `backend/db/migrations/00030_add_user_slack_identities.sql`, `backend/db/migrations/00031_add_personal_slack_notification_preferences.sql`, and the matching `*_test.go` files.
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