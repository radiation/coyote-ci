# API Tokens

API tokens provide first-class programmatic access for scripts, automation, and the future Coyote CLI. They avoid browser session cookie scraping and keep non-browser access on the same authentication and RBAC path as normal users.

## Model

- API tokens belong to a local Coyote user.
- Tokens inherit the owning user's global role and project memberships.
- A token cannot grant more access than its owner already has.
- Token scopes, service accounts, and admin-vended constrained tokens are deferred.
- Raw tokens are shown only once, immediately after creation.
- Coyote stores only a hash plus a short display prefix such as `coyote_pat_abc12345`.

## Routes

Authenticated users can manage their own tokens:

- `GET /api/me/tokens`: list token metadata
- `POST /api/me/tokens`: create a token
- `DELETE /api/me/tokens/{token_id}`: revoke a token

Create request:

```json
{
  "name": "fixture populator",
  "expires_at": "2026-06-12T00:00:00Z"
}
```

`expires_at` is optional. The create response includes `token` once; list responses never include raw tokens or token hashes.

## Calling the API

Send the raw token in the `Authorization` header:

```bash
curl -sS "$COYOTE_BASE_URL/api/me" \
  -H "Authorization: Bearer $COYOTE_API_TOKEN"
```

Fixture and CLI automation should read `COYOTE_API_TOKEN` and send `Authorization: Bearer <token>`. Do not export, parse, or scrape browser cookies for scripts.

Revoked, expired, invalid, or malformed bearer tokens are rejected with `401 Unauthorized`.
