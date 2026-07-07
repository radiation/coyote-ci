# Coyote CI Prompt Recipes

These prompts are intentionally short. They are meant to help future assistants stay local, identify the smallest relevant file set, and avoid broad scans.

Treat source code, migrations, tests, and recent diffs as authoritative.
When a local `.codegraph/` index exists, query CodeGraph before broad grep/find/read sweeps. For frontend/backend contract questions, check `backend/docs/swagger.yaml`, `backend/docs/swagger.json`, and existing frontend API client/types before opening backend handlers.

## Discovery / PR slice planning

"Follow the repo AI workflow. This is discovery only; do not edit files.

Task: [feature, bug, or PR slice]

Read the smallest relevant docs under `docs/ai-context/` first, then query CodeGraph before broad scans. Include full repo-relative paths for any files identified. Open only enough source to confirm the slice.

Return:
1. Scope summary: what is included and explicitly not included.
2. Context used: AI-context docs read, CodeGraph queries used, and any Swagger/OpenAPI or frontend API client/type files checked.
3. Smallest relevant file set: backend, frontend, tests, migrations, docs, and config files with full repo-relative paths.
4. Proposed implementation approach: short steps, existing repo patterns to follow, and package/layer boundaries to preserve.
5. Trade-offs and options: conservative option, broader option, recommended option, and why.
6. Validation plan: narrow commands to run and validation intentionally deferred.
7. Risks and open questions: ambiguities, hidden dependencies, and places scope might need to expand."

## Implementing an approved slice

"Implement the approved slice below.

Use only the approved files unless you find a concrete reason to expand scope. If scope needs to expand, stop and explain why before editing additional files.

Approved plan:
[paste discovery result or approved file list]

Rules:
- Preserve existing package/layer boundaries.
- Keep the change PR-sized.
- Do not include deferred or broader options unless explicitly requested.
- Add or update focused tests for changed behavior.
- Run the approved narrow validation commands.
- Summarize changed files and validation results.

If a trade-off from the discovery plan becomes relevant during implementation, choose the conservative option unless the approved plan says otherwise."

## Planning a backend change

"Plan a backend change for [feature or bug]. Start with `.github/copilot-instructions.md`, `docs/ai-context/current-priorities.md`, `docs/ai-context/backend-map.md`, and `docs/ai-context/domain-model.md`. Query CodeGraph before broad scans, then identify the smallest relevant file set across handler, service, repository, domain, and migrations. Include full repo-relative paths. Explain where the behavior is controlled, what trade-offs exist, and what tests should be checked before editing. Do not scan unrelated packages."

## Planning a frontend change

"Plan a frontend change for [feature or bug]. Start with `.github/copilot-instructions.md`, `docs/ai-context/current-priorities.md`, `docs/ai-context/frontend-map.md`, and `docs/ai-context/domain-model.md`. Check Swagger/OpenAPI and existing frontend API client/types for contract questions, query CodeGraph before broad scans, then identify the smallest relevant route, page, component, API client, and test files. Include full repo-relative paths. Explain which file likely owns the behavior, what trade-offs exist, and avoid broad scans."

## Reviewing a PR diff

"Review this PR diff for correctness, regression risk, and missing tests. Use `.github/copilot-instructions.md` plus the AI context docs only as navigation aids, then inspect only the files changed in the diff and their nearest owning tests or helpers. Findings first, concise summary second."

## Fixing a failing test

"Fix the failing test [test name or file]. Start from the failing test and the nearest implementation file. Use `docs/ai-context/backend-map.md` or `docs/ai-context/frontend-map.md` only to narrow the search. State the likely owning file, make the smallest fix, and rerun the narrowest relevant test."

## Adding an API endpoint

"Add an API endpoint for [behavior]. Start with `.github/copilot-instructions.md`, `docs/ai-context/backend-map.md`, and `docs/ai-context/domain-model.md`. Identify the smallest handler, service, repository, domain, migration, and test file set needed. Include full repo-relative paths. Keep handlers thin and business logic in services."

## Adding or changing CLI behavior

"Add or change CLI behavior for [command or workflow]. Start with `.github/copilot-instructions.md`, `docs/ai-context/current-priorities.md`, `docs/ai-context/backend-map.md`, and `docs/ai-context/domain-model.md`. Stay local to `backend/cmd/coyote`, `backend/internal/cli`, `backend/internal/apiclient`, `backend/internal/versioninfo`, and only the smallest backend handler/API files needed by the command. Include full repo-relative paths. Keep the CLI as a thin HTTP client of the API. For build, artifact, project, or job discovery commands, verify the required API-token scopes first and preserve least-privilege defaults; when selectors depend on authorization or ambiguity checks, prefer server-side resolution over client-side guessing."

## Adding frontend polish

"Improve frontend polish for [page or interaction] without broad redesign. Start with `.github/copilot-instructions.md`, `docs/ai-context/current-priorities.md`, and `docs/ai-context/frontend-map.md`. Identify the smallest page and component files plus their colocated tests. Include full repo-relative paths. Preserve existing routes and API contracts unless explicitly changing them."

## Changing Slack integration or personal Slack identity

"Update Slack integration, personal Slack identity, or personal Slack DM delivery. Start with `.github/copilot-instructions.md`, `docs/ai-context/current-priorities.md`, `docs/ai-context/backend-map.md`, `docs/ai-context/frontend-map.md`, and `docs/ai-context/domain-model.md`. Stay local to `backend/internal/platform/slack`, `backend/internal/service/user_slack_identity_service.go`, `backend/internal/service/notification_service.go`, `backend/internal/service/notification_preferences.go`, `backend/internal/service/build/notifications.go`, `backend/internal/service/build/notification_planning.go`, `backend/internal/service/build/notification_destinations.go`, `backend/internal/service/build/notification_execution.go`, `backend/internal/service/build/notification_format.go`, `backend/internal/service/build/notification_recovery.go`, the Slack workspace repositories, `backend/internal/http/handler/notification_personal_slack_identity_handler.go`, `backend/internal/http/handler/notification_preferences_handler.go`, `backend/internal/http/handler/notification_slack_workspace_handler.go`, `frontend/src/pages/MyNotificationsPage.tsx`, `frontend/src/pages/MyNotificationsPage.sections.tsx`, `frontend/src/pages/NotificationsPage.tsx`, `frontend/src/pages/NotificationsPage.sections.tsx`, `frontend/src/api/notificationClient.ts`, and `frontend/src/types/notification.ts`. Never expose bot tokens, persist stable Slack member IDs rather than handles, keep personal identities separate from shared notification targets, keep personal DM delivery separate from shared webhook routing, keep delivery dedupe keyed by transport-aware logical destination identity rather than mutable display fields, verify workspace lifecycle dependency checks, and rerun architecture plus Swagger checks."

When Slack notifications include CLI follow-up hints, only surface commands that exist today. Prefer `coyote build status`, `coyote build logs`, and `coyote build retry`; use a failed-step-specific logs command only when the renderer already knows the actual step index; never invent artifact download or step-retry commands.

## Updating artifact behavior

"Update artifact behavior for [browse, lineage, version tags, release view, storage, or provenance]. Start with `docs/ai-context/backend-map.md`, `docs/ai-context/frontend-map.md`, `docs/ai-context/domain-model.md`, and `backend/docs/artifact-model.md`. Identify the smallest cross-backend/frontend file set and call out whether the change touches metadata, storage, lineage, or UI presentation. Include full repo-relative paths and call out trade-offs before editing."

## Asking for architecture advice without broad scans

"Give architecture advice for [question] without broad repo scanning. Start with `.github/copilot-instructions.md`, `docs/ai-context/product-context.md`, `docs/ai-context/current-priorities.md`, and the relevant map file. Answer using the smallest relevant file set, note assumptions and trade-offs, and say which source files should be read next only if needed."

## Reviewing whether AI context docs need updates

"Review whether this PR should update AI context docs. Check `.github/copilot-instructions.md` and `docs/ai-context/` only as context-maintenance surfaces. Update AI context docs only if the PR changes roadmap/current focus, backend/frontend structure, core domain relationships, or recommended AI workflow. Do not update AI context docs for small localized fixes unless the existing docs would become misleading."