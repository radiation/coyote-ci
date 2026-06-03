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

- Artifacts are produced outputs linked back to the build that created them.
- Artifact metadata is stored separately from blob bytes so artifacts can support browsing, lineage, and later storage backends.
- Version tags connect artifacts or managed image outputs to release-like concepts such as versions and channels.
- Releases/lineage behavior ties together artifact outputs, version tags, and the source/build context that produced them.

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
- If the change is about browsing outputs, inspect artifacts, version tags, provenance/source types, and the pages that expose them.
- If the change is about access control, inspect user, membership, and token types together with auth helpers and the relevant handler/service slice.