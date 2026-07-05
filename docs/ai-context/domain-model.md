# Coyote CI Domain Model

This file is a navigational summary of the main domain relationships. Use it to choose the right backend and frontend files before editing. Source code, migrations, and tests remain authoritative.

## Core workflow

- Projects are the top-level grouping for jobs, memberships, and project-scoped visibility.
- Jobs define repeatable pipeline/workflow configuration within a project.
- Builds are executions of a job. They carry trigger/source metadata, lifecycle state, and links to steps, logs, artifacts, and queue history.
- Build steps are the individual execution units within a build. Step state rolls up into build state.
- Logs belong to build steps and explain execution outcomes.

## Queue and execution

- Queue items represent scheduled or claimable execution work for builds/jobs.
- Job priority influences queue ordering and should be checked alongside queue-item and worker behavior.
- Workers claim queue work and report execution progress/results back into persisted build and step state.
- Execution support code prepares workspaces, source materialization, caches, and runner context around a build.

## Artifacts, versions, and releases

- Artifact instances are the concrete outputs produced by one build.
- Logical artifacts or packages are the stable browse/release identity for those outputs, usually grouped by job plus logical artifact path.
- Artifact versions are immutable version assignments for one logical artifact and point at one concrete artifact instance.
- Channels or labels are mutable aliases such as `latest`, `stable`, or `prod`; artifact-backed version/channel data is surfaced through the compatibility `VersionTag` read model.
- Artifact provenance or lineage ties artifact/package views back to the producing build, project/job context, and source metadata.
- Source links on artifact lineage expose the producing git ref and commit SHA when that build source metadata exists.
- Automatic version generation comes from artifact declaration config and applies generated version and optional channel labels after artifact collection.

## Provenance and source relationships

- Source refs describe repository URL, branch/ref, commit SHA, and related trigger context for a build.
- Provenance/source linking lets the system answer which source and which build produced an artifact or release view.
- Source credentials and repo writeback settings support fetching sources and writing CI results back to repositories when configured.

## Identity and authorization

- Users have a global role and may also hold project memberships.
- Project memberships apply project-scoped roles such as owner, maintainer, or viewer.
- API tokens are user-scoped credentials used to call the API without an interactive session.
- API tokens now persist explicit scopes such as `build:read`, `build:logs`, `build:run`, and `artifact:read`; a token never grants more access than the owning user's normal authorization.
- CLI contexts are local client-side records that bind a human-chosen name to one server URL plus an optional credential reference and default output mode.
- Authorization checks sit at handler/service boundaries but are grounded in user role and project membership state.

## Notification and Slack concepts

- `slack_webhook` notification targets are shared delivery destinations used by build notifications today.
- Slack workspace integration is instance-level infrastructure that stores the connected Slack workspace and bot credentials.
- User Slack identity is a self-scoped mapping between one Coyote user and one stable Slack member ID in the connected workspace.
- A personal Slack identity is not a notification target; personal Slack DM delivery uses the stored workspace integration plus stable Slack member ID without creating a shared target or subscription.
- User notification preferences now store independent commit-author email and Slack enablement for failed and successful builds. Saved preference and current delivery availability remain separate concepts.
- Notification deliveries are now logical transport-aware records keyed by build, event type, transport, and stable opaque destination key; source attribution is intentionally outside the dedupe identity.
- Shared email and Slack webhook deliveries key off stable target ids, personal email deliveries key off owned personal email target ids, and personal Slack DM deliveries key off workspace integration id plus stable Slack member id.
- Notification deliveries now behave as a claimable bounded-retry ledger: non-terminal rows can move through `pending`, `sending`, and `retry_waiting`, claim ownership is used to prevent stale writers from overwriting newer attempts, retry scheduling metadata is persisted with the row, and terminal outcomes distinguish permanent from exhausted failure.
- Periodic recovery draining of due retries/stale claims and recovery of terminal builds that never reached notification planning both remain deferred follow-up slices.
- Future shared Slack channel destinations, if added, should remain separate from personal user identities.

## Practical edit routing

- If the change is about lifecycle or state transitions, inspect build, build step, queue, and worker types together with their service and repository helpers.
- If the change is about browsing outputs, inspect artifacts, artifact packages/versions/channels, provenance/source types, and the pages that expose them.
- If the change is about access control, inspect user, membership, and token types together with auth helpers and the relevant handler/service slice.