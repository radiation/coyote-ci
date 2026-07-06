# Coyote CI Current Priorities

This file captures current product and engineering priorities for AI assistants working in this repository.

It should be updated after meaningful PRs or roadmap changes.

## Current focus

Current work is focused on making Coyote CI easier to inspect, operate, and reason about.

Near-term priorities include:

- build detail UX polish
- artifact browser and release browsing polish
- source and provenance linking polish
- queue operational visibility
- auth, RBAC, project membership, and API token foundations
- CLI-first remote workflow foundations
- scope-aware API-token authorization for build, log, artifact, and rerun APIs
- notifications
- behavior-preserving refactors that improve maintainability
- frontend polish that improves clarity without broad redesigns

Recent completed slice:

- artifact lineage plus automatic generated artifact version/channel labels V1 is complete
- generated artifact versions and channels are configured per artifact declaration, not a top-level `release` block
- notifications are an active product feature area after the artifact/provenance slice
- local Mailpit-backed email notification plumbing is the first notifications slice; SMTP config lives in `backend/internal/platform/config`, transport plumbing lives in `backend/internal/platform/email`, and local inspection is via `http://localhost:8025`
- the current notification slice is terminal build email for failed and successful builds with durable notification targets and project/job subscriptions, env-recipient fallback when no subscriptions match, durable per-recipient delivery records/dedupe, and small admin-style backend endpoints for managing email targets and project/job subscriptions without direct SQL: config lives in `backend/internal/platform/config`, SMTP transport lives in `backend/internal/platform/email`, admin CRUD orchestration lives in `backend/internal/service/notification_service.go`, target/subscription persistence lives in `backend/internal/repository/notification_subscription_repository.go` plus `backend/db/migrations`, delivery persistence lives in `backend/internal/repository/notification_delivery_repository.go`, worker/server wiring lives in `backend/cmd/server` and `backend/cmd/worker`, terminal build hooks live in `backend/internal/service/build/lifecycle.go` and `backend/internal/service/build/completion.go`, admin API wiring lives in `backend/internal/http/handler/notification_handler.go` and `backend/internal/http/router.go`, and local/manual verification uses the notification target/subscription API plus `POST /api/dev/notifications/sample-build` and Mailpit at `http://localhost:8025`
- the current frontend notification slice is a small admin/settings UI at `frontend/src/pages/NotificationsPage.tsx` mounted under `/settings/notifications`; it manages email targets plus project/job subscriptions using the existing notification API and local Mailpit verification flow
- the current Slack notification slice adds instance-level Slack workspace connection metadata for admins, self-scoped personal Slack identity linking, and commit-author personal Slack DM delivery with independent per-event email/Slack preference controls; linking persists stable Slack member IDs only, delivery uses `chat.postMessage`, and shared Slack webhook targets remain separate
- Slack terminal build notifications now include real copyable CLI follow-up commands based on implemented `coyote build status`, `coyote build logs`, and `coyote build retry` flows; success stays minimal, failed-build log hints prefer a specific failed-step index when known, and notifications must not suggest nonexistent commands
- the current notification delivery refactor replaces recipient-string dedupe with a claimable transport-aware delivery ledger backed by stable internal destination ids; the current slice now includes atomic claim acquisition, bounded retry eligibility and scheduling metadata, stale-claim reclamation, permanent-vs-retryable failure handling, lost-claim-safe sent/failure transitions, and a server-owned recovery drain that actively processes due retries and expired sending claims across memory and Postgres implementations while keeping the ledger claim authoritative
- the current CLI slice adds a thin `coyote` binary under `backend/cmd/coyote` plus `backend/internal/cli` and `backend/internal/apiclient`; it now covers version reporting, named server contexts, token storage, auth status, server info, build inspection, artifact download, and read-only project/job discovery via `project list/show` and `job list/show`, and JSON output remains the stable automation interface for those inspection commands
- API tokens now persist sorted scopes and backend authorization requires both the owning user's permission and the token's required scope for build metadata, log reads, artifact reads, and build-run style actions; valid tokens may still call `/api/me` without a resource scope
- project and job discovery for API-token callers now also requires `build:read`; project selectors accept id or slug, and job name selectors require a project-scoped lookup to resolve ambiguity server-side

## Next likely notification slice

- Shared project/job Slack channel routing is the next likely Slack-delivery follow-on after personal DM delivery.
- Additional notification observability and operator inspection around per-transport delivery records is a likely follow-up now that personal DM delivery exists.
- A dedicated notification-runner process remains deferred; the current recovery loop runs inside server processes and multiple servers may run it safely because the delivery ledger claim remains authoritative.
- Recovery from terminal builds that were persisted but never reached notification planning is still deferred.
- Shared project/job Slack channel routing remains deferred until owner-or-maintainer authorization for shared destinations is designed.

## Current development style

Prefer:

- small PR-sized changes
- behavior-preserving refactors
- explicit state transitions
- focused tests
- clear source/build/artifact relationships
- incremental UX improvements
- backend/frontend changes that preserve existing contracts unless intentionally changing them

Avoid:

- broad rewrites
- speculative abstractions
- unrelated cleanup bundled into feature work
- new distributed systems machinery unless requested
- AI/MCP/product-intelligence work unless specifically in scope
- full dependency graph engines unless explicitly requested

## Current UI direction

The UI should favor operational clarity.

Recent direction includes:

- build detail pages that surface the most important information quickly
- artifact views that behave more like release/lineage browsers than flat file lists
- queue views that show running, queued, failed, and completed work in an operator-friendly way
- provenance/source information that is visible and easy to navigate
- cards/sections that avoid unnecessary density while still fitting important context

## Current backend direction

The backend should preserve clear layered boundaries:

- handlers parse requests and map responses
- services own workflow orchestration and business logic
- repositories own persistence and transaction-safe updates
- domain types stay free of HTTP and database-driver concerns
- lifecycle/state-machine behavior should be explicit and tested

## Current AI/token workflow

When using AI coding agents:

- start from this file, `docs/ai-context/product-context.md`, and `.github/copilot-instructions.md`
- inspect only the smallest relevant file set
- use generated repo maps or code graphs as navigation aids when available
- treat source code, migrations, and tests as authoritative
- do not scan broad directories unless necessary
