# Coyote CI Frontend Map

Use this file to locate the smallest frontend slice before opening many components.

Start with this file, `docs/ai-context/current-priorities.md`, `docs/ai-context/product-context.md`, and `.github/copilot-instructions.md`. Frontend code and tests remain authoritative.

## App shell and routing

- `frontend/src/App.tsx`: app bootstrap, readiness gate, auth provider, query client, and router mount.
- `frontend/src/routes/router.tsx`: route-to-page map. Start here when a change is route-specific.
- `frontend/src/components/Layout.tsx`: shared frame for routed pages.

## Pages and routes

- `frontend/src/pages`: page-level containers. Start here for user-visible behavior, data loading, and page-specific interactions.
- Build detail UI: `BuildDetailPage.tsx`, `BuildDetailPage.sections.tsx`, and `BuildDetailPage.helpers.ts`; `BuildDetailPage.tsx` now also owns the `?step=` deep link that opens a specific step log panel for actionable notification links.
- Artifact browser/release/lineage UI: `ArtifactsPage.tsx`, `ArtifactLogicalBrowserPage.tsx`, `ArtifactDetailPage.tsx`, plus `components/ArtifactBrowser.tsx` and `components/BuildArtifactsSection.tsx`.
- Queue operations UI: `QueuePage.tsx` and `WorkersPage.tsx`.
- Project/job/build navigation: `ProjectsListPage.tsx`, `ProjectDetailPage.tsx`, `JobsListPage.tsx`, `JobDetailPage.tsx`, `JobCreatePage.tsx`, `BuildsListPage.tsx`.
- Auth/token/user/credentials UI: `APITokensPage.tsx` for `/settings/my-api-tokens`, `UsersPage.tsx`, `CredentialsPage.tsx`, token API helpers in `api/personalTokenClient.ts`, and auth state in `auth.tsx` and `auth-context.ts`.
- Notification target/subscription admin UI: `NotificationsPage.tsx` as the composition owner, plus `NotificationsPage.helpers.ts`, `NotificationsPage.sections.tsx`, `api/notificationClient.ts`, and `types/notification.ts`; this page handles both email and Slack webhook targets and keeps webhook secrets masked during edits.
- Personal notification self-service UI: `MyNotificationsPage.tsx` as the composition owner, plus `MyNotificationsPage.helpers.ts`, `MyNotificationsPage.sections.tsx`, `api/notificationClient.ts`, and `types/notification.ts` for personal email targets, per-event email/Slack commit-author preferences, and personal Slack identity linking.

## Shared components and state

- `frontend/src/components`: reusable visual sections and widgets. Inspect only the components rendered by the page you are changing.
- `frontend/src/api/client.ts`: shared API client export surface and common helpers.
- `frontend/src/api/personalTokenClient.ts`: personal API-token list/create/revoke helpers used by the self-service settings page.
- `frontend/src/api/notificationClient.ts`: notification-specific API functions for targets, subscriptions, defaults, Slack workspace integration, and personal Slack identity flows.
- `frontend/src/api/buildClient.ts`: build-focused API helpers.
- `frontend/src/queries`: shared query helpers for reused data-fetching logic.
- `frontend/src/types`: frontend API/domain types grouped by area such as builds, artifacts, jobs, projects, workers, and identity.
- `frontend/src/types/artifact.ts`: artifact browse/detail/lineage contracts, including logical artifact versions and channel labels.
- `frontend/src/types/build.ts`: shared `VersionTag` compatibility type still used by build and artifact responses.
- For frontend work that needs backend API details, check the generated OpenAPI docs in `backend/docs/swagger.yaml` or `backend/docs/swagger.json` and the existing frontend API client/types before scanning backend HTTP handlers.

## Common change areas

- Page or route behavior: start in `frontend/src/routes/router.tsx`, then the target page.
- Shared UI polish: start in the page, then the smallest component under `frontend/src/components` that owns the rendered section.
- API contract updates: start in `frontend/src/api/client.ts` and the matching file in `frontend/src/types` before editing pages.
- Build detail changes: start in `BuildDetailPage.tsx`, then `BuildDetailPage.sections.tsx`, `BuildDetailPage.helpers.ts`, and nearby components like `StepList.tsx` or `BuildArtifactsSection.tsx`.
- Artifact/release/lineage changes: start in artifact pages, `ArtifactBrowser.tsx`, `BuildArtifactsSection.tsx`, `frontend/src/api/client.ts`, `frontend/src/types/artifact.ts`, and `frontend/src/types/build.ts`.
- Queue operations changes: start in `QueuePage.tsx`, `WorkersPage.tsx`, and worker/build/job types.
- Auth, tokens, membership, or role-based UI: start in `auth.tsx`, `auth-context.ts`, `types/identity.ts`, `api/personalTokenClient.ts`, then the relevant settings or project page.
- Notification settings/admin changes: start in `pages/NotificationsPage.tsx`, then `pages/NotificationsPage.sections.tsx`, `pages/NotificationsPage.helpers.ts`, `api/notificationClient.ts`, `types/notification.ts`, and the colocated page test.
- Personal Slack identity or personal-notification changes: start in `pages/MyNotificationsPage.tsx`, then `pages/MyNotificationsPage.sections.tsx`, `pages/MyNotificationsPage.helpers.ts`, `api/notificationClient.ts`, `types/notification.ts`, and `pages/MyNotificationsPage.test.tsx`. This page owns cross-section state and mutation coordination for personal Slack linking plus the four independent failure/success x email/Slack preference controls.

## Tests and patterns

- Most frontend tests are colocated as `*.test.tsx` or `*.test.ts` next to pages, components, API modules, and theme/auth helpers.
- `frontend/src/api/client.test.ts` remains the main contract-level test surface for API client behavior, including notification client re-exports.
- Personal API-token UI coverage lives primarily in `frontend/src/pages/APITokensPage.test.tsx`, including one-time-secret dismissal and revoke confirmation behavior.
- Personal Slack identity UI coverage lives primarily in `frontend/src/pages/MyNotificationsPage.test.tsx`; admin Slack workspace coverage lives in `frontend/src/pages/NotificationsPage.test.tsx`.
- Extracted notification page helper coverage lives in `frontend/src/pages/NotificationsPage.helpers.test.ts` and `frontend/src/pages/MyNotificationsPage.helpers.test.ts`.
- For artifact lineage or version/channel contract work, check `frontend/src/api/client.test.ts` and the nearest artifact page/component test before scanning broader UI code.
- For page changes, start with the matching page test before scanning unrelated components.
- For shared UI changes, start with the nearest component test and only widen if the page composes behavior that the component test does not cover.