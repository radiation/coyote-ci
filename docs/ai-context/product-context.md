# Coyote CI Product Context

Coyote CI is a CI control plane and artifact repository system.

The core product idea is that CI execution and artifact storage should be tightly connected. Builds, jobs, logs, steps, artifacts, versions, channels, provenance, source refs, and generated outputs should be easy to inspect as one connected workflow instead of separate systems.

## Product principles

- Make builds easy to understand.
- Treat artifacts as first-class citizens, not afterthoughts.
- Keep provenance and source links visible.
- Favor operational clarity over dense tables.
- Prefer small, understandable workflows before advanced platform features.
- Keep the system easy to run locally while leaving seams for durable, multi-node deployments.

## Core product areas

Current product areas include:

- projects and jobs
- build creation and execution
- build steps and logs
- persisted queueing and worker assignment
- job priority
- artifact metadata, storage, versions, channels, and lineage
- provenance and source linking
- auth, RBAC, project membership, and API tokens
- frontend views for builds, jobs, artifacts, and queue operations

## Differentiators

Coyote CI is not only a CI server. It is intended to combine CI orchestration with an artifact repository so the system can answer questions like:

- What source produced this artifact?
- What changed between these artifact versions?
- Which build created this release?
- What logs, steps, and provenance explain this output?
- Which artifacts are attached to a build, job, project, or source ref?

This source/build/artifact connection is a major product differentiator.

## Future product layers

Future layers may include:

- notifications
- AI/MCP-powered build, diff, and artifact summaries
- resource usage recommendations
- advanced scheduling policies beyond simple job priority
- advanced compliance scopes
- DORA metrics
- monorepo dependency graphs and selective rebuild planning
- predictive capacity or performance forecasting

These should not be implemented unless explicitly requested or clearly part of the current milestone.
