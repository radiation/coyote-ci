# Coyote CI State Machine

This document is the source of truth for build and step lifecycle rules in Coyote CI.

## Build States

- pending: Build record exists but is not yet queued for execution.
- queued: Build is eligible for preparation.
- preparing: Build workspace/source preparation is in progress.
- running: At least one step has been claimed or is executing.
- success: Build completed successfully.
- failed: Build completed with at least one failed step.
- canceled: Build was canceled by an operator before reaching a terminal result.

## Step States

- pending: Step is defined but not yet claimed.
- running: Step is actively claimed/executing under a lease.
- success: Step completed successfully.
- failed: Step completed unsuccessfully.
- canceled: Step was canceled as part of build cancellation.

## Allowed Transitions

### Build

- pending -> queued
- queued -> preparing
- preparing -> running
- preparing -> failed
- running -> success
- running -> failed
- queued -> canceled
- preparing -> canceled
- running -> canceled

### Step

- pending -> running
- running -> success
- running -> failed

Cancellation is an explicit bulk operation for pending or running steps rather
than a normal step-completion transition.

## Triggering Events

- Build pending -> queued:
  - Build is explicitly queued (for example via API or worker queue bootstrap for default steps).
- Build queued -> running:
- Build queued -> preparing:
  - Worker/service begins build-level preparation.
- Build preparing -> running:
  - Build workspace and source checkout complete.
- Build preparing -> failed:
  - Build-level preparation or checkout fails.
- Build running -> failed:
  - Any required step completes with failed.
- Build running -> success:
  - Last required step completes with success and all steps are successful.

- Step pending -> running:
  - Worker claim succeeds (including reclaim for expired leases where the step remains running under a new claim).
- Step running -> success:
  - Worker reports successful completion for the active claim token.
- Step running -> failed:
  - Worker reports failed completion for the active claim token.
- Build/step pending or running -> canceled:
  - An operator cancels the build; Postgres terminalizes the build, its
    cancelable steps, and its queued/running execution jobs atomically.

## Guard Conditions

- Build and step transitions must satisfy the allowed transition table.
- Terminal states cannot be mutated.
- Step completion is valid only for the active claim token / lease owner.
- Claim-less step completion is not supported.
- Completion from stale claim tokens must be rejected and must not change persisted state.
- Repository updates use guarded compare-and-swap style conditions (status and claim token checks) so stale workers cannot overwrite newer state.

## Terminal State Behavior

- Build terminal states: success, failed, canceled.
- Step terminal states: success, failed, canceled.
- Terminal records are immutable with respect to lifecycle status transitions.
- Duplicate completions against terminal steps are treated as no-op outcomes and do not mutate the step/build lifecycle.

## Key Invariants

- A build cannot transition directly from pending -> running.
- A build cannot transition directly from queued -> running; it must pass through preparing.
- A step cannot transition directly from pending -> success or pending -> failed.
- Any step failure forces the build to failed.
- Build success is only valid when all required steps are successful.
- Worker result handling must reject stale completions and stale lease renewals.
- Source checkout/prep happens once per build before step execution; step runners do not perform source preparation.
- Execution jobs use terminal `status = failed` for both ordinary execution
  failures and timeouts. `failure_kind = execution` identifies ordinary
  execution failures; `failure_kind = timeout` identifies timeouts. Successful
  and canceled execution jobs have no failure kind.

## Pipeline Group Semantics

- Top-level pipeline `steps` are ordered.
- `group.steps` are parallel siblings.
- A group is a barrier: downstream top-level items do not become runnable until all steps in the current group succeed.
- If any step in a group fails, downstream groups remain blocked and the build fails.

## Workspace Lineage Semantics

- A root execution job starts from the build's immutable source snapshot.
- A job with one predecessor may continue that predecessor's logical workspace lineage after the predecessor succeeds.
- Parallel siblings are isolated writable descendants of the same predecessor baseline. They must not share one writable filesystem.
- A fan-in job never merges mutable predecessor filesystems. It starts from the nearest common ancestor baseline when one exists, otherwise the immutable source snapshot; future explicit upstream outputs provide branch-specific data.
- Workspace lineage is a logical execution input, not a filesystem implementation. A fresh execution container or pod does not require reconstructing every workspace from source, and a logical workspace revision does not require a physical snapshot after every step.
- Materialization provides the physical writable filesystem for one execution. A successful workspace commit advances logical lineage; the current local implementation commits without copying and may reuse one build directory through a linear segment.
- A WorkspaceRevision is distinct durable-publication metadata for a successful logical workspace state. It is not a MaterializedWorkspace and does not itself store durable workspace bytes; a future WorkspaceRevisionStore will own that representation.
- A revision is authoritative only in the `published` state. `publishing` and `deleted` revisions are not eligible workspace inputs, and publication must be guarded by the producing execution job's active claim.
- The current host-backed implementation also retains the legacy shared build-directory behavior for planned fan-out and fan-in inputs so existing Docker/local pipelines continue to run. This is compatibility behavior only: it does not provide writable sibling isolation or portable fan-in restoration. Future strict or portable materializers must reject unsupported plans using the workspace capability errors until those semantics are implemented.
- Retries and reruns never reuse failed mutable state. Each build attempt has independent logical workspace lineage, though a future runtime may reuse a valid successful predecessor baseline when immutable inputs match.
- Caches remain performance-only and are not part of workspace correctness semantics.

## Step/Build Relationship

- Workers operate on steps, not directly on final build outcomes.
- Build lifecycle progression is derived/reconciled from step outcomes:
  - First claimed running step may advance build queued -> running.
  - Failed step completion advances build running -> failed.
  - Final successful step completion advances build running -> success when all required steps succeeded.
- This keeps orchestration policy explicit while repository code stays focused on atomic persistence and guards.
