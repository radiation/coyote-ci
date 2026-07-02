# Coyote CI Prompt Recipes

These prompts are intentionally short. They are meant to help future assistants stay local, identify the smallest relevant file set, and avoid broad scans.

Treat source code, migrations, tests, and recent diffs as authoritative.

## Planning a backend change

"Plan a backend change for [feature or bug]. Start with `.github/copilot-instructions.md`, `docs/ai-context/current-priorities.md`, `docs/ai-context/backend-map.md`, and `docs/ai-context/domain-model.md`. Identify the smallest relevant file set across handler, service, repository, domain, and migrations. Explain where the behavior is controlled and what tests should be checked before editing. Do not scan unrelated packages."

## Planning a frontend change

"Plan a frontend change for [feature or bug]. Start with `.github/copilot-instructions.md`, `docs/ai-context/current-priorities.md`, `docs/ai-context/frontend-map.md`, and `docs/ai-context/domain-model.md`. Identify the smallest relevant route, page, component, API client, and test files. Explain which file likely owns the behavior. Avoid broad scans."

## Reviewing a PR diff

"Review this PR diff for correctness, regression risk, and missing tests. Use `.github/copilot-instructions.md` plus the AI context docs only as navigation aids, then inspect only the files changed in the diff and their nearest owning tests or helpers. Findings first, concise summary second."

## Fixing a failing test

"Fix the failing test [test name or file]. Start from the failing test and the nearest implementation file. Use `docs/ai-context/backend-map.md` or `docs/ai-context/frontend-map.md` only to narrow the search. State the likely owning file, make the smallest fix, and rerun the narrowest relevant test."

## Adding an API endpoint

"Add an API endpoint for [behavior]. Start with `.github/copilot-instructions.md`, `docs/ai-context/backend-map.md`, and `docs/ai-context/domain-model.md`. Identify the smallest handler, service, repository, domain, migration, and test file set needed. Keep handlers thin and business logic in services."

## Adding frontend polish

"Improve frontend polish for [page or interaction] without broad redesign. Start with `.github/copilot-instructions.md`, `docs/ai-context/current-priorities.md`, and `docs/ai-context/frontend-map.md`. Identify the smallest page and component files plus their colocated tests. Preserve existing routes and API contracts unless explicitly changing them."

## Changing Slack integration or personal Slack identity

"Update Slack integration, personal Slack identity, or personal Slack DM delivery. Start with `.github/copilot-instructions.md`, `docs/ai-context/current-priorities.md`, `docs/ai-context/backend-map.md`, `docs/ai-context/frontend-map.md`, and `docs/ai-context/domain-model.md`. Stay local to `backend/internal/platform/slack`, `backend/internal/service/user_slack_identity_service.go`, `backend/internal/service/notification_service.go`, `backend/internal/service/build/notifications.go`, the Slack workspace repositories, `backend/internal/http/handler/notification_personal_slack_identity_handler.go`, `backend/internal/http/handler/notification_handler.go`, `frontend/src/pages/MyNotificationsPage.tsx`, `frontend/src/pages/NotificationsPage.tsx`, `frontend/src/api/client.ts`, and `frontend/src/types/notification.ts`. Never expose bot tokens, persist stable Slack member IDs rather than handles, keep personal identities separate from shared notification targets, keep personal DM delivery separate from shared webhook routing, verify workspace lifecycle dependency checks, and rerun architecture plus Swagger checks."

## Updating artifact behavior

"Update artifact behavior for [browse, lineage, version tags, release view, storage, or provenance]. Start with `docs/ai-context/backend-map.md`, `docs/ai-context/frontend-map.md`, `docs/ai-context/domain-model.md`, and `backend/docs/artifact-model.md`. Identify the smallest cross-backend/frontend file set and call out whether the change touches metadata, storage, lineage, or UI presentation."

## Asking for architecture advice without broad scans

"Give architecture advice for [question] without broad repo scanning. Start with `.github/copilot-instructions.md`, `docs/ai-context/product-context.md`, `docs/ai-context/current-priorities.md`, and the relevant map file. Answer using the smallest relevant file set, note assumptions, and say which source files should be read next only if needed."

## Reviewing whether AI context docs need updates

"Review whether this PR should update AI context docs. Check `.github/copilot-instructions.md` and `docs/ai-context/` only as context-maintenance surfaces. Update AI context docs only if the PR changes roadmap/current focus, backend/frontend structure, core domain relationships, or recommended AI workflow. Do not update AI context docs for small localized fixes unless the existing docs would become misleading."