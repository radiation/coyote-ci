# Coyote CI State Machine

This document is the source of truth for build and step lifecycle rules in Coyote CI.

## Build States

- pending: Build record exists but is not yet queued for execution.
- queued: Build is eligible for worker pickup and step claiming.
- running: At least one step has been claimed or is executing.
- success: Build completed successfully.
- failed: Build completed with at least one failed step.

## Step States

- pending: Step is defined but not yet claimed.
- running: Step is actively claimed/executing under a lease.
- success: Step completed successfully.
- failed: Step completed unsuccessfully.

## Allowed Transitions

### Build

- pending -> queued
- queued -> running
- running -> success
- running -> failed

### Step

- pending -> running
- running -> success
- running -> failed

## Triggering Events

- Build pending -> queued:
  - Build is explicitly queued (for example via API or worker queue bootstrap for default steps).
- Build queued -> running:
  - First runnable step is successfully claimed by a worker.
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

## Guard Conditions

- Build and step transitions must satisfy the allowed transition table.
- Terminal states cannot be mutated.
- Step completion is valid only for the active claim token / lease owner.
- Claim-less step completion is not supported.
- Completion from stale claim tokens must be rejected and must not change persisted state.
- Repository updates use guarded compare-and-swap style conditions (status and claim token checks) so stale workers cannot overwrite newer state.

## Terminal State Behavior

- Build terminal states: success, failed.
- Step terminal states: success, failed.
- Terminal records are immutable with respect to lifecycle status transitions.
- Duplicate completions against terminal steps are treated as no-op outcomes and do not mutate the step/build lifecycle.

## Key Invariants

- A build cannot transition directly from pending -> running.
- A step cannot transition directly from pending -> success or pending -> failed.
- Any step failure forces the build to failed.
- Build success is only valid when all required steps are successful.
- Worker result handling must reject stale completions and stale lease renewals.

## Step/Build Relationship

- Workers operate on steps, not directly on final build outcomes.
- Build lifecycle progression is derived/reconciled from step outcomes:
  - First claimed running step may advance build queued -> running.
  - Failed step completion advances build running -> failed.
  - Final successful step completion advances build running -> success when all required steps succeeded.
- This keeps orchestration policy explicit while repository code stays focused on atomic persistence and guards.
