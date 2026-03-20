# AGENTS.md

## Purpose

This repository is a greenfield CI/orchestration system named Coyote CI.

The goal is to build a clean, understandable core first, then layer on more advanced features over time. Agents working in this repo should optimize for simplicity, correctness, and maintainability rather than speculative platform complexity.

---

## Core principles

* Start small
* Keep control flow obvious
* Prefer explicit wiring over framework magic
* Maintain strict layer separation
* Build only what is needed for the current milestone
* Leave clean seams for future extension

---

## Desired architecture

Use a layered Go application structure.

### Layers

#### 1. Domain

Core business concepts live here.

Examples:

* Build
* BuildStep
* Worker
* Artifact
* QueueItem
* Pipeline
* Project

Rules:

* No HTTP concerns
* No database driver logic
* Focus on representing state clearly

---

#### 2. Repository

The repository layer owns persistence.

Responsibilities:

* SQL queries and data access
* Inserts, updates, selects
* Transactions where needed
* Mapping database rows to domain types

Rules:

* No HTTP logic
* No orchestration-heavy business logic

---

#### 3. Service

The service layer owns business logic.

Responsibilities:

* Workflow orchestration
* Validation
* State transitions
* Retry logic
* Queue management
* Worker assignment
* Coordination across repositories and platform components

Guideline:
If it’s not purely transport or persistence, it belongs here.

---

#### 4. Handler / HTTP

Handlers must stay thin.

Responsibilities:

* Parse request
* Call service
* Return HTTP response
* Map errors to status codes

Rules:

* No business logic
* No direct SQL
* No orchestration

---

#### 5. Composition root

Dependency wiring happens explicitly in:

* cmd/server/main.go
* or a small composition package

No DI frameworks unless explicitly requested.

---

## Dependency injection expectations

Use constructor-based dependency injection.

Example wiring:

buildRepo := repository.NewBuildRepository(db)
workerRepo := repository.NewWorkerRepository(db)

buildService := service.NewBuildService(buildRepo, workerRepo, scheduler)
buildHandler := http.NewBuildHandler(buildService)

Guidelines:

* repositories receive DB handles
* services receive repositories
* handlers receive services
* wiring is explicit and traceable

---

## Interface guidance

Do not introduce interfaces everywhere.

Preferred:

* concrete types first
* interfaces at the consumer boundary
* small, focused interfaces

Avoid:

* interface-per-struct patterns
* unnecessary abstraction layers

---

## Package guidance

Preferred layout:

cmd/server
internal/domain
internal/repository
internal/service
internal/http
internal/platform

Keep packages cohesive and narrow.

---

## Data modeling guidance

It is acceptable to separate:

* domain models
* HTTP request/response models
* database row models

Do not force a single struct across all layers.

---

## Current implementation priorities

Focus on:

* build creation
* build step modeling
* queueing
* worker assignment
* persisted state transitions
* logs
* artifact metadata
* basic REST API
* local dev via Docker Compose

---

## Future features (do NOT overbuild early)

* combined CI + artifact repo
* multi-master control plane
* Kubernetes-native execution
* AI-generated diff summaries
* notifications
* dependency-based build sequencing
* monorepo selective builds
* parallel execution
* queue prioritization
* resource forecasting
* dashboards
* CVE / secret scanning
* complex RBAC
* DORA metrics

---

## Persistence expectations

Prefer:

* relational schema
* explicit status enums
* timestamps
* transaction-safe updates

Avoid:

* event sourcing by default
* in-memory-only workflow state

---

## Queue and workflow expectations

Prefer:

* explicit state machines
* retry limits
* idempotent operations
* persisted queue state

Avoid:

* hidden retry loops
* opaque background orchestration

---

## Concurrency expectations

Use concurrency deliberately.

Prefer:

* clear goroutine ownership
* context propagation
* graceful shutdown

Avoid:

* premature optimization
* complex patterns without need

---

## API expectations

Prefer REST/JSON.

APIs should be:

* explicit
* stable
* easy to debug

---

## Service communication expectations

Use different transport styles for external and internal boundaries.

Prefer:
* REST/JSON for external APIs exposed to users, CLIs, and UIs
* gRPC with Protocol Buffers for internal service-to-service communication once multiple services exist

Guidelines:
* keep transport concerns separate from business logic
* define internal service contracts in `.proto` files
* do not manually edit generated code
* HTTP handlers should remain thin adapters over service logic
* do not introduce gRPC transport before a real service boundary exists

For the current milestone:
* it is acceptable to structure service boundaries with future gRPC in mind
* do not add gRPC transport until a second service or remote worker boundary actually requires it

---

## Database expectations

Prefer:

* simple SQL
* migration-friendly schemas
* explicit repository methods

Avoid:

* heavy ORM abstractions
* magic query generation

---

## Testing expectations

Prefer:

* table-driven tests
* service-layer unit tests
* repository integration tests

Avoid:

* brittle mocks
* timing-based tests

---

## Security expectations

Treat builds as untrusted.

* no hardcoded secrets
* least privilege
* clear execution boundaries

---

## Code style expectations

Generated code should be:

* minimal but complete
* idiomatic Go
* explicit
* easy to trace

Avoid:

* speculative abstractions
* placeholder architecture

---

## Agent behavior

When modifying code:

1. Preserve layer boundaries
2. Keep handlers thin
3. Keep logic in services
4. Keep persistence in repositories
5. Wire dependencies explicitly
6. Choose the simplest working approach
7. Explain tradeoffs when adding complexity
