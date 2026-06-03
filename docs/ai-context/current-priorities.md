# Coyote CI Current Priorities

This file captures current product and engineering priorities for AI assistants working in this repository.

It should be updated after meaningful PRs or roadmap changes.

## Current focus

Current work is focused on making Coyote CI easier to inspect, operate, and reason about.

Near-term priorities include:

- build detail UX polish
- artifact browser and artifact lineage
- generated artifact version/channel labels on artifact declarations
- source and provenance linking
- queue operational visibility
- auth, RBAC, project membership, and API token foundations
- behavior-preserving refactors that improve maintainability
- frontend polish that improves clarity without broad redesigns

Recent completed slice:

- artifact lineage plus generated artifact version/channel labels V1 is complete
- generated artifact versions and channels are configured on artifact declarations, not a top-level `release` block

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
