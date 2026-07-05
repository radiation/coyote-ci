# API Tokens

API tokens provide first-class programmatic access for scripts, automation, and the future Coyote CLI. They avoid browser session cookie scraping and keep non-browser access on the same authentication and RBAC path as normal users.

## Model

- API tokens belong to a local Coyote user.
- Tokens inherit the owning user's global role and project memberships.
- A token cannot grant more access than its owner already has.
- Tokens persist an explicit sorted scope set.
- Session-authenticated browser requests keep their normal authorization behavior; API-token requests must satisfy both the owner's authorization and the token's required scope.
- Current scopes are `build:read`, `build:logs`, `build:run`, and `artifact:read`.
- Raw tokens are shown only once, immediately after creation.
- Coyote stores only a hash plus a short display prefix such as `coyote_pat_abc12345`.

## Routes

Authenticated users can manage their own tokens:

- `GET /api/me/tokens`: list token metadata
- `POST /api/me/tokens`: create a token
- `DELETE /api/me/tokens/{token_id}`: revoke a token

Signed-in users can also manage personal tokens from the web UI at `/settings/api-tokens`.

Create request:

```json
{
  "name": "fixture populator",
  "expires_at": "2026-06-12T00:00:00Z",
  "scopes": ["build:read", "build:logs"]
}
```

`expires_at` is optional. The create response includes `token` once; list responses never include raw tokens or token hashes.

Recommended defaults:

- IDE-agent diagnostic token: `build:read`, `build:logs`
- Artifact-inspection token: add `artifact:read`
- Build-triggering token: add `build:run` only when needed

## Calling the API

Send the raw token in the `Authorization` header:

```bash
curl -sS "$COYOTE_BASE_URL/api/me" \
  -H "Authorization: Bearer $COYOTE_API_TOKEN"
```

Fixture and CLI automation should read `COYOTE_API_TOKEN` and send `Authorization: Bearer <token>`. Do not export, parse, or scrape browser cookies for scripts.

Valid tokens can call `GET /api/me` without a resource scope so clients can verify authentication state. Resource APIs return `403 Forbidden` with code `missing_token_scope` when a valid token lacks the required scope.

Revoked, expired, invalid, or malformed bearer tokens are rejected with `401 Unauthorized`.
