# Coyote CI - Copilot Instructions

## Project intent

Coyote CI is a greenfield CI/orchestration system. The project should start small, stay understandable, and grow incrementally.

Prioritize:
- correctness
- readability
- explicit control flow
- small interfaces
- incremental delivery

Do not over-engineer for speculative future needs. Build the simplest version that works today while keeping clean seams for future expansion.

## Architectural direction

Assume the near-term architecture is:
- a Go backend as the primary control plane
- containerized local development with Docker Compose
- Postgres for persistence once database-backed coordination is added
- worker execution via containers or external workers
- a UI/API layer added incrementally, not all at once

Prefer standard library or lightweight libraries unless there is a clear productivity or reliability benefit.

Avoid introducing complex frameworks, distributed systems infrastructure, or Kubernetes-specific behavior unless explicitly requested.

## Layering rules

Keep layers clean and responsibilities separate.

Use this mental model:

- **domain/models** define core business concepts and state
- **repositories** own database access and persistence concerns
- **services** own business logic, orchestration, validation, and state transitions
- **handlers/routes** are thin and handle HTTP request/response concerns only
- **composition root** wires dependencies together explicitly

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

Repositories should not contain HTTP logic.
Repositories should not contain orchestration-heavy business rules.

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

Prefer implementing these first:
- builds
- build steps
- job states
- worker assignment
- logs
- artifacts
- retries
- queueing
- API endpoints for core workflows

Do not proactively implement these unless asked:
- Kubernetes-native controllers/operators
- multi-region coordination
- predictive capacity forecasting
- AI-generated PR or diff summaries
- CVE scanning pipelines
- DORA analytics
- org-wide RBAC hierarchies
- advanced monorepo dependency graphs

These are valid future features, but they should be layered on top of a stable core.

## Persistence guidance

Assume persistent state matters. Design models so the system can eventually support multiple control-plane instances backed by Postgres.

However:
- do not assume full distributed coordination unless asked
- do not introduce premature leader-election logic
- do not add event sourcing unless explicitly requested
- prefer straightforward relational schemas and explicit transactions

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

## Monorepo and dependency-awareness guidance

Future support for monorepos, selective rebuilds, and parallel module execution is desirable.

For now:
- keep build definitions extensible
- keep file-change detection and dependency evaluation pluggable
- do not build a complicated graph engine unless explicitly asked

## Product guidance

This project may eventually include:
- artifact storage
- notifications
- AI notes on diffs
- queue prioritization
- resource usage reporting
- dashboards
- authorization at multiple scopes
- DORA metrics

These are future layers, not baseline assumptions.

When generating code, always ask:
- is this needed for the current milestone?
- is there a simpler version?
- can this be added later behind a clear interface?

## Output style for Copilot

When generating code for this repo:
- prefer complete, minimal implementations
- avoid placeholder architecture with no behavior
- avoid speculative abstractions
- keep comments useful and brief
- explain tradeoffs when making architectural choices
- when multiple options exist, prefer the simpler one unless requirements clearly justify complexity
