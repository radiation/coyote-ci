# Parallel Execution v1 Semantics

This document describes the first version of group-based parallel execution in Coyote CI.

## DSL

`steps:` entries now support two forms:

1. A normal step.
2. A parallel group entry:

```yaml
steps:
  - name: setup
    run: ./setup.sh
  - group:
      name: test-matrix
      steps:
        - name: unit-tests
          run: pytest tests/unit
        - name: integration-tests
          run: pytest tests/integration
  - name: package
    run: ./package.sh
```

Validation rules:

- `group.name` is required and non-empty.
- `group.steps` must contain at least one step.
- Nested groups are rejected.
- Step names must remain unique across the full expanded pipeline.

## Normalization

Pipelines are normalized into a flat execution plan of real executable nodes.

Each executable step has:

- `node_id` (stable within a build)
- optional `group_name`
- `depends_on_node_ids`
- existing execution fields (`run`, `image`, `env`, `timeout_seconds`, `working_dir`, artifacts, cache)

Group behavior in v1:

- Steps before a group become dependencies of every step in that group.
- Steps after a group depend on all steps in that group.

## Scheduling and completion

A step/job is runnable only when all its dependencies have succeeded.

Scheduler behavior:

- Multiple unblocked steps from the same build can be claimed concurrently.
- Claim token/lease/stale-claim protections are unchanged.

Completion behavior:

- Step terminal state is persisted atomically.
- Build succeeds only when all steps succeed.
- Build fails when no running or runnable work remains and success is no longer reachable.

Failure semantics (v1):

- A failed step blocks downstream dependents.
- Unrelated already-runnable or already-running steps may continue and finish.
- No continue-on-error.

## Persistence additions

Migration `00004_add_parallel_execution_graph_metadata.sql` adds additive metadata columns:

- `build_steps.node_id`
- `build_steps.group_name`
- `build_steps.depends_on_node_ids`
- `build_jobs.node_id`
- `build_jobs.group_name`
- `build_jobs.depends_on_node_ids`

## API additions

Minimal response fields were added for future UI graph rendering:

- step response: `node_id`, `group_name`, `depends_on_node_ids`
- execution job response: `node_id`, `group_name`, `depends_on_node_ids`

## Workspace concurrency caveat

There are currently two claim paths:

1. Durable job path (default): dependency-aware and supports true per-build parallel claims.
2. Transitional step-only fallback path: still used for builds without durable jobs.

The fallback path now honors dependency metadata when selecting steps, but durable job scheduling remains the canonical path for parallel execution semantics.
