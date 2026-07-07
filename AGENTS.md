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
2. For non-trivial PR slices, use the discovery and implementation recipes in `docs/ai-context/prompt-recipes.md`.
3. Read the smallest relevant `docs/ai-context/` files first.
4. For API-contract questions, check `backend/docs/swagger.yaml`, `backend/docs/swagger.json`, and existing frontend API client/types before opening handlers.
5. Query CodeGraph before broad grep/find/read scans when `.codegraph/` exists.
6. Identify concrete files with full repo-relative paths, not just basenames.
7. Inspect only the concrete files needed for that slice.
8. Before editing, summarize the docs read, CodeGraph queries used, files selected, validation planned, and any related files intentionally left out of scope.
9. Preserve existing package/layer boundaries.
10. Update or add tests for changed behavior.
11. Avoid widening scope without calling it out.

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