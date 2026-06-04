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
- Authorization checks sit at handler/service boundaries but are grounded in user role and project membership state.

## Practical edit routing

- If the change is about lifecycle or state transitions, inspect build, build step, queue, and worker types together with their service and repository helpers.
- If the change is about browsing outputs, inspect artifacts, artifact packages/versions/channels, provenance/source types, and the pages that expose them.
- If the change is about access control, inspect user, membership, and token types together with auth helpers and the relevant handler/service slice.