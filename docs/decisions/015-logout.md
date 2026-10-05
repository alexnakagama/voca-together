# 015: Logout (M1 step 8)

> **Status:** in force.
>
> **Changed later:** 016 built the middleware. 023 implements the client contract in a different order: it
> clears the local tokens first and calls the server afterwards. 025 makes logout also clear Google's
> credential state on the device.
>
> **Current rules:** `.claude/rules/auth.md`.

- `POST /v1/auth/logout` with `Authorization: Bearer vt_at_…` → **204** with no body. It revokes only the session whose
  *current* access token is presented (`revoked_at = now()`), which kills its access and refresh tokens alike. The
  user's other sessions are untouched. The body is never read; a token in the body or query string doesn't count.
  Every response, errors included, has `Cache-Control: no-store`. No migration was needed.
- **Idempotent and uniform:** every well-formed access token gets the same 204, whether its session was revoked now,
  already revoked (the original `revoked_at` is kept), expired, or is unknown or rotated out. **204, not 200**:
  there is nothing to return, and a status body would hint at the state we don't reveal.
- **Missing or malformed credentials → 401** `invalid_access_token` with `WWW-Authenticate: Bearer` (RFC 6750; unlike
  login's credential form, this is the HTTP Bearer scheme). This covers no header, several `Authorization` headers,
  another scheme, an empty token, or anything that isn't exactly a well-formed `vt_at_` token (a refresh token, extra
  spaces), and it happens before any database access. The token format is public, so this split reveals nothing about
  sessions. It does catch client bugs that a 204 would hide while the session stayed live. The scheme name is matched
  case-insensitively (RFC 7235). DB failure → opaque 500.
- **Expired access tokens can log out.** Logout only reduces privilege, and only the current access token matches
  (refresh replaces it). An idle client shouldn't have to rotate its tokens just to revoke them. The worst misuse of a
  leaked expired-but-current token is logging its owner out.
- **Store:** one statement, `UPDATE sessions SET revoked_at = now() WHERE access_token_hash = $1 AND revoked_at IS NULL
  RETURNING id, user_id`, uses the existing unique index. It locks only that session row, so like refresh it is exempt
  from the user-row-first lock order (012) and can't deadlock with a future password reset.
- **Races:** a concurrent refresh holds the same row lock, and the waiting side re-checks against the committed row.
  Logout first → the refresh sees `revoked_at` and returns 401. A rotation first → the old access token no longer
  matches, so logout is a no-op (204) and the session lives on with tokens only the refreshing client received. A
  rotation that rolls back → logout revokes the original row. Concurrent logouts → one revokes, all get 204.
- **Client contract (Flutter):** the token manager awaits any in-flight refresh, then logs out with the latest access
  token, and deletes the stored tokens whatever the outcome (including 401, 5xx and network errors).
- **Known limitation:** logout with a rotated-out access token does nothing. If an attacker holding a stolen refresh
  token rotated first, the victim's logout can't end the attacker's session (no previous access hash is kept, 014).
  Remedies are deferred: log out all sessions, a session list, or accepting `refresh_token` in the logout body and
  running it through reuse detection.
- **Logs:** `auth: logout succeeded` (`user_id`, `session_id`); `auth: logout had no effect` (no ids, so an unknown or
  stale token isn't linked to an account); `auth: logout failed` (`reason=malformed`). Tokens and hashes are never
  logged.
- **Deferred:** access-token middleware (it will reuse `bearerToken` and `ErrInvalidAccessToken`), logout-all, a session
  list, and cleanup of revoked sessions.
