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

- Slack direct messages.
- Channel discovery.
- Project/job Slack destination routing.
- Slack OAuth install flow.

## Personal Slack identity V1

Coyote users can link their own account to one stable Slack member identity in the connected workspace.

- Matching uses Slack `users.lookupByEmail` against the authenticated Coyote email.
- Resolving a match persists nothing; confirmation re-runs the live lookup and verifies the current workspace before saving the link.
- This requires the Slack app scope `users:read.email`.
- After adding the scope in Slack, an administrator must reinstall or reauthorize the Slack app before Coyote can use it.
- Coyote stores Slack’s stable member ID for the linked user. Display name, handle, and profile image are metadata snapshots only.
- Linking does not enable Slack delivery yet.
- A previous failed admin connection test is shown as a warning only; users can still attempt a live lookup while the workspace remains enabled.
- Users can view, pause, resume, and unlink their own Slack identity from **Settings > My notifications**.

If the Slack app is missing `users:read.email`, Coyote returns an actionable error instructing the user to contact an administrator to add the scope and reinstall or reauthorize the Slack app.

## Personal user workflow

1. Open **Settings > My notifications**.
2. Use **Find my Slack account** to run a live email-based Slack member lookup.
3. Review the matched Slack account.
4. Confirm the match to persist the stable Slack member ID.
5. Optionally pause the link without deleting it, or unlink it entirely.

Pausing or unlinking the Slack identity does not change email notification preferences and does not send Slack messages.

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

Slack API scope documentation: https://api.slack.com/scopes/users:read.email

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

## Disconnect and workspace-switch restrictions

Once users have linked personal Slack identities:

- disconnect is blocked until those user identities are unlinked;
- switching the instance integration to a different Slack workspace is blocked until those identities are unlinked;
- disabling the current workspace remains allowed;
- rotating credentials for the same workspace preserves linked identities.
