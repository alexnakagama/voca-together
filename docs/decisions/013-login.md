# 013: Login and session issuance (M1 step 6)

> **Status:** in force.
>
> **Changed later:** 018 added rate limits, the 5 s argon2 queue timeout (503) and session cleanup, closing
> those deferrals; 020 gave passwordless accounts the same 401. A cap on sessions per user, request IDs and
> metrics are still deferred.
>
> **Current rules:** `.claude/rules/auth.md`.

- `POST /v1/auth/login` `{"email","password"}` → 200
  `{"access_token":"vt_at_…","token_type":"Bearer","expires_in":900,"refresh_token":"vt_rt_…"}` (OAuth 2.0 token
  response shape; `expires_in` is relative, so client clock skew doesn't matter). Unknown email, wrong password, a
  malformed stored hash and a password changed mid-login all get the same 401 `invalid_credentials` (005);
  `email_not_verified` (403) only after the password is correct. Invalid email format or an empty password: 422
  (`email:invalid`, `password:required`) before any hashing or database access. The registration password policy
  is not re-applied at login. Every login response, errors included, has `Cache-Control: no-store`. No
  `WWW-Authenticate` on 401: this is a credential form, not an HTTP auth scheme.
- **Timing:** an unknown email verifies the password against a dummy hash made with the current parameters at startup.
  A malformed stored hash also verifies against it, is logged as an error, and gets the usual 401. So unknown email and
  wrong password do the same work: one SELECT and one argon2 verification.
- **Argon2 concurrency limit:** every hash and verification (register and login) waits for one of `GOMAXPROCS` slots,
  so memory stays at or below `GOMAXPROCS × 19 MiB` however many requests arrive. Without the limit, a few hundred
  concurrent unauthenticated requests could exhaust memory. With p=1, argon2 uses one core, so extra concurrency would
  add memory but not throughput. Waiters give up when their request context ends. All account states share one
  queue, so waiting reveals nothing. This is a memory bound, not rate limiting (still deferred).
- **Session row** (one per login, never reusing earlier credentials; the request's own tokens are ignored, so there
  is no session fixation): hashes of fresh `vt_at_`/`vt_rt_` tokens, `access_expires_at = now()+15m`,
  `refresh_expires_at = now()+30d`, `expires_at = now()+90d`, `previous_refresh_token_hash` and `revoked_at` NULL,
  `created_at = last_used_at`. All values come from one transaction clock. Tokens are returned only after commit.
  Migration 00002 enforces `access_expires_at, refresh_expires_at <= expires_at` (refresh must cap its sliding expiry)
  and a 256-byte `user_agent`.
- **Password-change race:** the session insert runs in a transaction that first re-checks
  `SELECT … WHERE id = $1 AND password_hash = <verified hash> FOR SHARE`. A password change or reset holding the user
  row `FOR UPDATE` either commits first (login then sees the new hash and creates nothing) or waits for login and then
  sees the new session to revoke it. Logins share the lock, so they never block each other. Argon2 never runs while a
  transaction or connection is held.
- **Rehash on login** (003): if the verified hash has outdated parameters, it is recomputed after the session commits
  and written with a compare-and-swap (`WHERE password_hash = <old>`), so a concurrent password change is never
  overwritten. This is best effort: a failure is logged and the login still succeeds.
- **User-Agent** is stored only so users can recognize their sessions. It is never trusted or logged. Invalid UTF-8
  and control characters are removed, surrounding space trimmed, and the rest truncated to 256 bytes on a character
  boundary; an empty result is stored as NULL. IP addresses are not stored.
- **Logs:** `auth: login succeeded` (`user_id`, `session_id`); `auth: login failed` (`reason` =
  `invalid_credentials` | `email_not_verified` | `password_changed`, plus `user_id` only if the account exists);
  malformed stored hash (error); rehash failure (warn). Never logged: email, password, tokens or their hashes, user
  agent, or the request body. `auth.Token` redacts itself in fmt, slog and JSON (`MarshalJSON` added: slog's JSON
  handler encodes nested values with encoding/json, which ignores `String`).
- A session committed just before the client disconnects is never returned. Its tokens are unreachable and it expires.
- **Deferred:** per-IP and per-account rate limiting (credential stuffing can still use up argon2 capacity, slowing
  logins but not exhausting memory), a cap on sessions per user, cleanup of expired and revoked sessions, request IDs
  and metrics, and a 503 with `Retry-After` when the argon2 queue wait is too long.
