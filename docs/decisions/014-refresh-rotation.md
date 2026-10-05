# 014: Refresh-token rotation (M1 step 7)

> **Status:** in force.
>
> **Changed later:** 016 built the middleware; 018 added the limit, said which refresh failures a client may
> retry (a 429 yes, a 503 no) and the cleanup; 023 is the client contract as implemented.
>
> **Current rules:** `.claude/rules/auth.md`.

- `POST /v1/auth/refresh` `{"refresh_token":"vt_rt_…"}` → 200 with the same body as login (`access_token`,
  `token_type`, `expires_in`, `refresh_token`). Every response, errors included, has `Cache-Control: no-store`. Any
  `Authorization` header is ignored, so refresh works after the access token has expired.
- **Errors:** an empty or missing token → 422 `refresh_token:required`. Malformed (including an access token presented
  as a refresh token), unknown, expired (sliding or absolute), revoked and reused tokens all get the same 401
  `invalid_refresh_token`, so the client can't tell them apart and simply logs in again. Malformed tokens are rejected
  before any database access. Bad JSON → 400; anything else → opaque 500.
- **Rotation:** one transaction locks the session row (`SELECT … WHERE refresh_token_hash = $1 OR
  previous_refresh_token_hash = $1 FOR UPDATE`), checks it, then replaces **both** token hashes. The rotated-out refresh
  hash moves to `previous_refresh_token_hash`. `refresh_expires_at` slides to `now()+30d` and `access_expires_at` is
  `now()+15m`, both capped at the absolute `expires_at` (which never moves). `last_used_at` is updated. The new tokens
  are returned only after commit. No migration was needed.
- **The old access token dies at once.** The alternative (keeping it valid until its 15-minute expiry) needs extra
  columns and a two-hash lookup in the future auth middleware, and helps only requests in flight during a proactive
  refresh. The client handles that case instead (see the client contract below). This is reversible: a
  previous-access-hash column can be added later without client changes.
- **Reuse detection:** presenting the stored previous refresh token revokes the session (`revoked_at = now()`), which
  commits even though no tokens are issued. It is checked before expiry, so replays against expired sessions are
  still revoked and logged (WARN). Only that session is revoked, not the user's other devices. Only **one** previous
  hash is kept: a token two or more rotations old is merely unknown (401, no revocation). Full lineage tracking is
  deferred.
- **Strict race policy:** the row lock makes two concurrent refreshes with one token serialize. The loser re-evaluates
  its WHERE against the committed row, matches through `previous_refresh_token_hash`, and is treated as reuse: the
  session is revoked. There is no grace window. If the winner rolls back instead, the loser succeeds normally.
- **Near the absolute expiry:** refresh is refused (401, nothing written) when less than 1 minute of the session
  remains, instead of issuing an access token that expires on arrival. A successful refresh therefore always has
  `expires_in` ≥ 60, and `expires_in` is rounded down.
- **Lock order:** refresh locks a single session row and nothing else, so it is exempt from the user-row-first rule
  (012) and can't deadlock. A future password reset (user row `FOR UPDATE`, then revoke all sessions) either waits for
  a refresh and then revokes the rotated session, or commits first, and the refresh then sees `revoked_at`.
- **Client contract (Flutter):** keep one token manager that runs at most one refresh at a time; concurrent callers
  await its result. Never retry a refresh with the same token after a network error: log in again instead. On a 401 for
  an API request, first check whether the stored tokens changed since the request was sent; if so, retry once with the
  new access token. Only if not, refresh.
- **Logs:** `auth: refresh succeeded` (`user_id`, `session_id`); `auth: refresh failed` (`reason` = `malformed` |
  `unknown` | `revoked` | `session_expired` | `refresh_expired`, plus ids when a session matched); WARN
  `auth: refresh token reuse detected, session revoked` (`user_id`, `session_id`). Tokens and hashes are never logged.
- **Deferred:** rate limiting (refresh is cheap and brute force is infeasible with 256-bit tokens), a grace window,
  full rotation lineage, cleanup of revoked and expired sessions, and access-token middleware.
