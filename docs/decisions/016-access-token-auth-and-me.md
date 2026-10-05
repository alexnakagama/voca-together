# 016: Access-token authentication and `GET /v1/me` (M1 step 9)

> **Status:** in force.
>
> **Changed later:** 027 and 029 added protected routes and put per-user write limits inside the middleware.
>
> **Current rules:** `.claude/rules/backend.md`, `.claude/rules/auth.md`.

- **Middleware:** `requireAccessToken` (`internal/server/authn.go`) wraps each protected route individually in
  `server.New` (`mux.Handle("GET /v1/me", authn(handleMe(…)))`); public routes are never wrapped. It parses the header
  with logout's `bearerToken`, calls `auth.Service.Authenticate`, and puts the resulting `auth.Identity{UserID,
  SessionID}` in the request context under an unexported key (`identityFrom`). Handlers pass `UserID` explicitly to
  services, so domain packages never read the context. A handler mounted without the middleware answers an opaque 500
  (fails closed). The mux matches methods first, so non-GET requests get 405 without touching the database.
- **Uniform 401:** every unusable credential gets 401 `invalid_access_token` with `WWW-Authenticate: Bearer`: no
  header, several headers, another scheme, extra spaces, a refresh token, junk, and any well-formed token that is
  unknown, rotated out, revoked, access-expired or past the session's absolute `expires_at`. Malformed ones are
  rejected before any database access. A token in the query string or body doesn't count. DB failure → opaque 500.
  Every response of a protected route, errors included, has `Cache-Control: no-store`.
- **Store:** one unlocked read on the unique hash index: `SELECT id, user_id FROM sessions WHERE access_token_hash = $1
  AND revoked_at IS NULL AND access_expires_at > now() AND expires_at > now()`. The last condition is implied by the
  `sessions_expiry_order` CHECK but is stated so the rule doesn't rest on it. No migration was needed.
- **No `last_used_at` write per request:** it would turn every read request into a write. It keeps meaning "last login
  or refresh", which an active client does at least every 15 minutes, precise enough for a future session list.
- **Races:** READ COMMITTED sees the last committed row. During a refresh the old access token works until the rotation
  commits and never after; there is no moment where neither works. A request authenticated just before a concurrent
  logout commits may still complete: the check isn't held for the request's duration, which would need a lock on
  every request. A user deleted between authentication and the handler's lookup gets 401 (its sessions were
  cascaded away), not 500.
- **`GET /v1/me` → 200** `{"id","email","email_verified_at","created_at"}`, built from an explicit struct; the password
  hash is never selected. `email_verified_at` is always set, since only verified accounts can log in.
- **Logs:** successful authentication logs nothing (it runs on every protected request). Rejections log
  `auth: access token rejected` with `reason=malformed` or `reason=invalid`, without ids. Tokens and hashes are never
  logged.
