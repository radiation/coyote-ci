# Coyote CI - Copilot Instructions

## Project intent

Coyote CI is a CI control plane and artifact repository system. The project should stay understandable, start with clear local defaults, and grow incrementally toward durable multi-node operation.

Prioritize:
- correctness
- readability
- explicit control flow
- small interfaces
- incremental delivery

Do not over-engineer for speculative future needs. Build the simplest version that works today while keeping clean seams for future expansion.

## Context and token discipline

This repository may be used with AI coding agents that have usage-based token costs.

Prefer targeted context over broad repository scans:
- inspect existing instructions and recent diffs first
- use symbol/file search before opening large files
- avoid reading generated, vendored, build-output, coverage, `dist`, and dependency directories unless necessary
- when asked to plan, identify the smallest relevant file set before proposing edits
- when available, use generated repo-index, graph, or AI-context artifacts as navigation aids, but do not treat them as more authoritative than source code and tests

Do not make broad exploratory changes just because related files exist.

## AI context files

Before scanning large parts of the repository, check the curated context files when they are relevant:

- `docs/ai-context/product-context.md` explains the product vision, differentiators, current product areas, and future layers.
- `docs/ai-context/current-priorities.md` explains the current roadmap, development posture, and near-term focus.
- `docs/ai-context/backend-map.md` maps the backend packages and common change areas so you can identify the smallest relevant file set first.
- `docs/ai-context/frontend-map.md` maps the frontend routes, pages, shared components, client modules, and test surfaces, including where to check OpenAPI docs and existing frontend API types before reading backend handlers for frontend/API work.
- `docs/ai-context/domain-model.md` summarizes the main domain relationships across builds, artifacts, queueing/job priority, provenance, and auth.
- `docs/ai-context/prompt-recipes.md` contains low-token prompt templates for planning, review, debugging, and scoped architecture questions.

Use these files as navigation and intent aids. Do not treat them as more authoritative than source code, migrations, or tests.

### Context maintenance

- Update `docs/ai-context/current-priorities.md` when roadmap or current focus changes.
- Update `docs/ai-context/backend-map.md` or `docs/ai-context/frontend-map.md` after large structural refactors.
- Update `docs/ai-context/domain-model.md` when core domain concepts or relationships change.
- After meaningful feature changes, update the relevant `docs/ai-context/` files if navigation or project-state guidance would otherwise become stale.
- Do not update AI context files for small localized bug fixes unless the docs would otherwise become misleading.

## Architectural direction

Assume the near-term architecture is:
- a Go backend as the primary control plane
- containerized local development with Docker Compose
- Postgres as the durable system of record for metadata and workflow state
- artifact blob storage via a pluggable store (filesystem for local/simple installs; object storage for production)
- worker execution via containers or external workers
- a UI/API layer added incrementally, not all at once

Prefer standard library or lightweight libraries unless there is a clear productivity or reliability benefit.

Avoid introducing complex frameworks or distributed infrastructure unless explicitly requested. Keep deployment assumptions compatible with both single-instance setups and future Helm/Kubernetes packaging.

## Layering rules

Keep layers clean and responsibilities separate.

Use this mental model:

- **domain/models** define core business concepts and state
- **repositories** own database access and persistence concerns
- **services** own business logic, orchestration, validation, and state transitions
- **handlers/routes** are thin and handle HTTP request/response concerns only
- **composition root** wires dependencies together explicitly

## Database Migrations (Durable Mode)

This project now assumes durable Postgres deployments.

Guidelines:
- Applied migrations are immutable; do not edit old migration files after they may have been applied in any environment.
- Add new numbered migration files for schema changes.
- Prefer additive, forward-safe migrations (new tables/columns/indexes, backfill, compatibility windows).
- Avoid destructive or irreversible migrations unless explicitly required.
- Keep migration steps explicit and operationally safe for persistent environments.

Migration tracking expectations:
- Use a real migration tool and migration history table.
- New schema work should be authored as tracked migrations, not by mutating existing applied files.

## Lifecycle and state machine guidance

For persisted workflow transitions:

- keep lifecycle/state-machine decision logic in shared pure helper functions
- do not duplicate transition rules across worker, service, repository, and handler layers
- repositories may apply lifecycle outcomes atomically inside a transaction when persisted state must be checked and mutated together
- services should orchestrate use cases and map outcomes, but should not re-implement repository-backed transition rules
- workers should execute work and report results, not decide build advancement or terminal build state
- when a step result affects both step state and build state, prefer one repository method that applies both atomically

### Domain
Domain types should represent core concepts like:
- Build
- BuildStep
- Worker
- Artifact
- QueueItem
- Pipeline
- Project

Domain types should not contain HTTP-specific or database-driver-specific behavior unless there is a very strong reason.

### Repository layer
Repositories are responsible for:
- reading and writing persistent state
- executing SQL or database operations
- mapping database rows into domain-friendly structures
- handling transactions when persistence behavior requires them

Repositories should not contain HTTP logic or high-level orchestration.
Repositories may enforce persistence-level invariants and apply precomputed lifecycle outcomes atomically when correctness requires a transaction.

### Service layer
Services are responsible for:
- workflow/business rules
- state transitions
- validation beyond basic request parsing
- coordination across repositories or infrastructure components
- queue behavior
- retry decisions
- worker assignment logic

Services should be the main home for application behavior.

### Handler / route layer
Handlers should stay thin.

Handlers are responsible for:
- parsing requests
- calling services
- returning HTTP responses
- mapping errors to appropriate status codes
- serializing JSON

Handlers should not contain business logic or direct SQL/database access.

## Dependency injection style

Use explicit constructor-based dependency injection.

Do not use framework-style dependency injection containers unless explicitly requested.

Preferred pattern:

- repositories receive database handles
- services receive repositories and other required collaborators
- handlers receive services
- `main.go` or a small composition package wires everything together

Example mindset:

- `repo := NewBuildRepository(db)`
- `svc := NewBuildService(repo, workerRepo, artifactStore)`
- `handler := NewBuildHandler(svc)`

Keep dependency wiring obvious and easy to trace.

## Interfaces

Do not create interfaces for everything by default.

Prefer:
- concrete types first
- small interfaces at the consumer boundary when they improve testing or substitution
- focused interfaces with only the methods actually needed

Avoid Java-style or enterprise-style abstraction layers with interfaces for every struct.

## Package and boundary guidance

Prefer a structure similar to:

- `cmd/server` for application entrypoint
- `internal/domain` for core domain types
- `internal/repository` for persistence logic
- `internal/service` for business logic
- `internal/http` for handlers, request/response models, and routing
- `internal/platform` or `internal/infra` for external integrations such as Postgres, containers, notifications, and artifact storage

Keep package responsibilities narrow and obvious.

## Data model guidance

Separate concerns where helpful:
- domain structs for core concepts
- request/response structs for HTTP payloads
- persistence row/mapping structs if needed

Do not collapse HTTP, persistence, and domain concerns into one giant struct unless the simplicity benefit is clear and the boundary remains understandable.

## Frontend guidance

For frontend changes:
- prefer small, focused components over large page files
- keep API access in shared client modules rather than inline fetch calls
- keep page containers responsible for data loading/mutations and delegate rendering to sections/components
- preserve existing routing and query/mutation patterns unless explicitly changing them
- add focused tests for user-visible behavior, empty states, links, and error handling
- avoid broad visual redesigns when the request is for polish or behavior

## Implementation priorities

When proposing or generating code, optimize for this order:

1. local single-node correctness
2. clean domain and persistence boundaries
3. safe job execution model
4. observability
5. horizontal scalability
6. advanced enterprise features

Do not skip ahead to advanced distributed features unless explicitly requested.

## What "start small" means in this repo

The core workflow already exists or is being built incrementally around:
- projects
- jobs
- builds
- build steps
- worker assignment
- logs
- artifacts
- retries
- queueing
- API endpoints for core workflows

When extending these areas:
- prefer small, behavior-preserving changes
- keep lifecycle behavior explicit and tested
- avoid introducing broad new platform layers unless requested

## Persistence guidance

Assume persistent state matters. Design models so the system can eventually support multiple control-plane instances backed by Postgres.

However:
- do not assume full distributed coordination unless asked
- do not introduce premature leader-election logic
- do not add event sourcing unless explicitly requested
- prefer straightforward relational schemas and explicit transactions

For artifacts:
- keep metadata in Postgres
- keep blob bytes in pluggable artifact stores
- keep provider-specific storage behavior in adapter/infra layers, not domain/service/handler logic

## Queue and execution guidance

Queueing should be modeled explicitly.
Execution should be observable and restart-safe.
Job state transitions should be easy to inspect and reason about.

Prefer:
- explicit state machines
- persisted status transitions
- idempotent worker operations where possible
- bounded retries with clear failure states

Avoid:
- implicit retry loops
- hidden side effects
- in-memory-only coordination for durable workflows

## API guidance

If building APIs:
- prefer REST/JSON unless told otherwise
- keep payloads simple and stable
- use explicit request/response types
- make status fields and timestamps easy for a UI to consume

## Internal service communication guidance

For external client-facing APIs:
- prefer REST/JSON unless told otherwise

For internal service-to-service communication:
- prefer gRPC with Protocol Buffers once multiple services exist
- define service contracts in `.proto` files
- keep generated protobuf code separate from handwritten business logic
- do not manually edit generated files
- keep transport adapters thin and push behavior into services

Until there is a real multi-service boundary:
- do not introduce gRPC transport prematurely
- instead, design service interfaces and request/response shapes so they can later be exposed cleanly via gRPC

## Database guidance

If adding database access:
- prefer simple SQL or lightweight data access patterns
- avoid ORM-heavy abstractions unless explicitly requested
- keep schema changes migration-friendly
- name tables and columns clearly
- model build, step, artifact, worker, queue, and audit concepts explicitly

## Observability guidance

Observability is a first-class concern.

Include:
- structured logging
- request or build correlation IDs
- metrics hooks where practical
- clear status enums
- timestamps for lifecycle events

Do not add heavyweight observability platforms in generated code unless asked.

## Go style and vet guidance

Avoid `go vet` shadow warnings aggressively. This repo treats repeated `err`
shadowing as a common commit blocker.

Rules:
- Once a function or test has an outer `err`, do not use `if _, err := ...; err != nil` or similar short declarations in an inner scope.
- Use operation-specific error names for scoped checks, such as `createErr`, `queueErr`, `cancelErr`, `completeErr`, `writeErr`, `closeErr`, or `markErr`.
- When assigning additional return values later in the same scope, prefer reassignment with the existing `err` only when it does not create a new inner scope shadow.
- Before finishing Go edits, scan touched code for `if .* err :=` patterns inside functions that already declared `err`.

## Testing guidance

Prefer:
- table-driven unit tests
- focused integration tests around repositories and service-layer behavior
- deterministic tests over timing-sensitive tests
- small mocks/fakes instead of elaborate harnesses

When possible, test:
- services independently from HTTP
- repositories with integration tests
- handlers as thin translation layers

For lifecycle/state-machine code specifically:
- prefer table-driven tests for pure decision helpers first
- then verify memory and Postgres repository parity against the same behavior
- treat repository tests as the source of truth for persisted transition semantics
- avoid timing-based tests when deterministic state assertions are possible

## Security guidance

Treat this as a CI system that may eventually run untrusted build workloads.

Therefore:
- do not assume builds are trusted
- avoid unnecessary privilege
- keep execution boundaries explicit
- never hardcode secrets
- prefer least-privilege defaults
- make room for future credential scoping and artifact scanning

Do not invent a full security subsystem unless explicitly requested.

For auth/authz seams:
- keep identity and authorization integration points explicit at service/handler boundaries
- design for external identity providers and group/project-scoped authorization over time
- avoid hardwiring provider-specific IAM semantics into core domain logic

## Deployment guidance

Keep runtime behavior provider-agnostic. Place provider-specific details in deploy/config/docs layers.

Implementation posture:
- support a simple single-instance deployment path
- keep seams clean for future multi-control-plane scaling
- keep artifacts and API serving concerns separable so artifact serving can scale independently later

## PR and review guidance

When responding to review feedback:
- make the smallest change that addresses the feedback
- preserve behavior unless the requested change is explicitly behavioral
- do not bundle unrelated cleanup into correctness fixes
- update tests when behavior changes or a bug is fixed
- explain when a suggested change is intentionally deferred

## Monorepo and dependency-awareness guidance

Future support for monorepos, selective rebuilds, and parallel module execution is desirable.

For now:
- keep build definitions extensible
- keep file-change detection and dependency evaluation pluggable
- do not build a complicated graph engine unless explicitly asked

## Product guidance

Coyote CI is a CI control plane and artifact repository. The near-term product direction is to make builds, jobs, queues, logs, artifacts, provenance, and source links easy to inspect and reason about.

Current and near-term product areas include:

- build and job execution workflows
- artifact storage, metadata, versioning, channels, and lineage
- build logs, step results, and provenance/source linking
- queue operational visibility
- auth, RBAC, project membership, and API token foundations
- lightweight dashboards and metrics for build and queue health
- behavior-preserving UI polish that improves clarity without changing core workflows

Future layers may include:

- notifications
- AI/MCP-powered build, diff, and artifact summaries
- resource usage recommendations
- advanced compliance scopes
- DORA metrics
- monorepo dependency graphs and selective rebuild planning
- predictive capacity or performance forecasting

When generating code, always ask:

- is this needed for the current milestone?
- is there a simpler version that preserves the current architecture?
- can this be added later behind a clear interface?
- does this change improve the build/artifact/operator experience without widening scope unnecessarily?


## Output style for Copilot

When generating code for this repo:
- prefer complete, minimal implementations
- avoid placeholder architecture with no behavior
- avoid speculative abstractions
- keep comments useful and brief
- explain tradeoffs when making architectural choices
- when multiple options exist, prefer the simpler one unless requirements clearly justify complexity
- avoid `go vet` shadow warnings by not re-declaring `err` in inner scopes when an outer `err` is already in scope; prefer explicit names like `writeErr`, `closeErr`, `markErr`
