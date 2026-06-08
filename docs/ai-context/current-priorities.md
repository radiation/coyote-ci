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
- notifications
- behavior-preserving refactors that improve maintainability
- frontend polish that improves clarity without broad redesigns

Recent completed slice:

- artifact lineage plus automatic generated artifact version/channel labels V1 is complete
- generated artifact versions and channels are configured per artifact declaration, not a top-level `release` block
- notifications are an active product feature area after the artifact/provenance slice
- local Mailpit-backed email notification plumbing is the first notifications slice; SMTP config lives in `backend/internal/platform/config`, transport plumbing lives in `backend/internal/platform/email`, and local inspection is via `http://localhost:8025`
- the current notification slice is terminal build email for failed and successful builds with durable notification targets and project/job subscriptions, env-recipient fallback when no subscriptions match, durable per-recipient delivery records/dedupe, and small admin-style backend endpoints for managing email targets and project/job subscriptions without direct SQL: config lives in `backend/internal/platform/config`, SMTP transport lives in `backend/internal/platform/email`, admin CRUD orchestration lives in `backend/internal/service/notification_service.go`, target/subscription persistence lives in `backend/internal/repository/notification_subscription_repository.go` plus `backend/db/migrations`, delivery persistence lives in `backend/internal/repository/notification_delivery_repository.go`, worker/server wiring lives in `backend/cmd/server` and `backend/cmd/worker`, terminal build hooks live in `backend/internal/service/build/lifecycle.go` and `backend/internal/service/build/completion.go`, admin API wiring lives in `backend/internal/http/handler/notification_handler.go` and `backend/internal/http/router.go`, and local/manual verification uses the notification target/subscription API plus `POST /api/dev/notifications/sample-build` and Mailpit at `http://localhost:8025`

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
