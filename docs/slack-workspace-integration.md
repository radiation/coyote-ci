# Slack Workspace Integration V1

Slack Workspace Integration V1 lets a global administrator connect one Slack workspace to a Coyote instance using a Slack bot token.

This integration is instance-level infrastructure and is separate from existing `slack_webhook` notification targets.

## What V1 does

- Connects one Slack workspace at the instance level.
- Validates the bot token with Slack `auth.test`.
- Persists stable Slack identity metadata (workspace ID and related bot/app metadata where available).
- Stores bot credentials server-side and never returns the token in API responses.
- Supports connection status, enable/disable, explicit replace, test connection, and disconnect.

## What V1 does not do

- Personal Slack identity linking for Coyote users.
- Slack direct messages.
- Channel discovery.
- Project/job Slack destination routing.
- Slack OAuth install flow.

## Admin workflow

1. Open **Settings > Notifications > Slack workspace**.
2. Enter a Slack bot token (`xoxb-...`) from your Slack app installation.
3. Connect the workspace.
4. Optionally disable/enable, test connection, replace credentials, or disconnect.

If a different workspace is already connected, replacement requires explicit confirmation (`replace_existing`).

## Slack app scopes

V1 only requires a valid token for `auth.test` verification.

Future slices are expected to require additional scopes such as:

- `users:read.email` (identity resolution by email)
- `users:read` (member profile lookup)
- `chat:write` (message delivery)

## Secret handling

- Bot tokens are accepted only on write requests and are never returned in responses.
- Tokens are not logged in structured errors.
- Credentials are stored in the application database in V1.
- Coyote currently relies on database/storage encryption controls for at-rest protection.
- The Slack integration code is isolated behind dedicated repository/service seams so application-level encryption or an external secret manager can be introduced later without changing API consumers.

## Relationship to existing Slack webhooks

Existing `slack_webhook` notification targets and their subscriptions remain unchanged.

- Connecting/disabling/disconnecting Slack workspace integration does not modify existing webhook targets.
- Existing webhook delivery behavior remains independent.
