# AGENTS.md

## Purpose

Coyote CI is a greenfield CI control plane and artifact repository. Agents should preserve the project’s bias toward small, understandable, durable increments.

For detailed coding conventions, use `.github/copilot-instructions.md` as the source of truth.

## Non-negotiables

- Keep handlers thin.
- Keep business logic in services.
- Keep persistence in repositories.
- Keep domain types free of HTTP/database-driver concerns.
- Use explicit constructor-based dependency wiring.
- Prefer concrete types first; introduce small interfaces only at consumer boundaries.
- Add new migrations; do not edit applied migrations.
- Avoid speculative distributed/platform features unless explicitly requested.
- Treat build workloads as untrusted.
- Prefer deterministic tests over timing-sensitive tests.

## Agent workflow

Before making changes:

1. Identify the smallest PR-sized slice.
2. Inspect only the files needed for that slice.
3. Preserve existing package/layer boundaries.
4. Update or add tests for changed behavior.
5. Avoid widening scope without calling it out.

## Current development posture

Prioritize:
- build/job correctness
- artifact metadata and lineage
- source/provenance linking
- queue/build observability
- auth/RBAC seams
- frontend polish around build, artifact, and queue workflows

Defer unless explicitly requested:
- Kubernetes controllers/operators
- multi-region coordination
- full dependency graph engines
- AI/MCP features
- DORA dashboards
- CVE/secret scanning
- complex enterprise RBAC