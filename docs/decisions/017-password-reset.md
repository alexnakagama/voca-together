# 017: Password reset (M1 step 10)

> **Status:** in force.
>
> **Changed later:** 018 added rate limits and cleanup; 019 built the verify-email page; 020 excluded
> passwordless accounts (no reset token is created for them and reset refuses them); 026 accepts that a
> Google ID token that is still valid can open a session after a reset.
>
> **Current rules:** `.claude/rules/auth.md`.

- `POST /v1/auth/forgot-password` `{"email"}` → 202 `{"status":"accepted"}` for unknown, unverified and verified
  addresses alike, with identical headers (`Cache-Control: no-store`). An invalid address → 422 `email:invalid`, before
  any database access. An existing account (verified **or not**) gets a new reset token, which replaces the previous
  one (004), and a reset email sent in the background after commit. Unknown addresses get nothing, so forgot can't be
  used to mail arbitrary addresses. No argon2 work in any case. Accepted timing difference: one DELETE and one INSERT
  more for existing accounts, as for resend (012).
- **Unverified accounts can reset**, and completing a reset sets `email_verified_at` (if unset): the link proves
  control of the mailbox exactly like verification does. This is how the owner of an address that someone else
  registered (with a password the owner doesn't know) takes it back, and it is what the account-exists email (011)
  already tells people to do.
- Reset tokens: `NewToken("")` like verification (011), **30 minutes** (DB clock), single use. Access and refresh
  tokens have a different length and are rejected as malformed before any database access; verification tokens are
  rejected by the purpose filter.
- `POST /v1/auth/reset-password` `{"token","password"}` → 200 `{"status":"password_reset"}`. It does **not** log in
  (as for verification, 012). Every unusable token (malformed, unknown, expired, used, replaced, other purpose,
  access/refresh token) gets the same 422 `token:invalid`. Policy failures get the registration codes (`too_short`,
  `too_long`, `too_common`, `same_as_email`). Every response has `Cache-Control: no-store`.
- **Order of work:** (1) token format and the email-independent policy rules, all fields together, before any
  database access; (2) one unlocked read of the token's owner: a dead token → `token:invalid`, then the
  same-as-email rule; (3) argon2 through the shared slot limiter (013), holding no transaction or connection;
  (4) one transaction. **A policy failure never consumes the token** (a typo must not burn the link), and argon2 only
  runs for a live token, so well-formed junk can't use up hash slots. The lookup in (2) is not trusted: (4) decides.
- **The transaction** (the only place anything is written):
  1. `SELECT … FROM user_tokens JOIN users … FOR UPDATE OF u`: the user row lock, first (012).
  2. `UPDATE user_tokens SET used_at = now() WHERE … AND used_at IS NULL AND expires_at > now()`: 0 rows → invalid,
     rollback, nothing written. This statement is what makes the token single use.
  3. `UPDATE users SET password_hash, email_verified_at = COALESCE(…, now()), updated_at`.
  4. `UPDATE sessions SET revoked_at = now() WHERE user_id = … AND revoked_at IS NULL`: every session, so every
     access and refresh token issued before, dies.
  5. `DELETE FROM user_tokens WHERE user_id = … AND used_at IS NULL`: no other one-time secret (e.g. a pending
     verification link) outlives the account's recovery. The consumed token stays, as used.
- **The new password is committed at COMMIT, and only then**, together with the used token, the revoked sessions and
  the deleted tokens. No other transaction ever sees the new hash with live old sessions, or an unused token with the
  new hash. Any error, rollback or cancellation leaves the token usable, the old password and all sessions. If the
  COMMIT outcome is unknown (connection lost), the client gets 500; retrying either succeeds or reports the link used,
  and the new password then works.
- **Isolation:** READ COMMITTED (the default). The analysis relies on each statement's fresh snapshot and on
  PostgreSQL re-checking a waited-for row against its newest committed version. Stricter levels would only turn
  these races into serialization failures that need retries.
- **Races and lock order.** Reset takes the user row `FOR UPDATE` first, then token, user (held) and session rows.
  Every other flow either takes the user row first too (verify, resend, forgot, reset: they serialize per account),
  or waits for at most one lock and holds nothing else while waiting, so no cycle is possible:
  - **Login** takes the user row `FOR SHARE` as its first lock, then inserts its session (013). If login holds it,
    reset waits; reset's session UPDATE is a later statement, so it sees and revokes the new session. If reset holds
    it, login waits, re-checks the old hash against the committed row, finds it changed and creates nothing (401).
    The rehash compare-and-swap can't overwrite a reset either.
  - **Refresh and logout** lock one session row and never the user row (their UPDATEs don't touch the FK). If one
    holds a session row, reset waits for it and then revokes the committed (possibly just rotated) row, so freshly
    rotated tokens die too. If reset commits first, refresh sees `revoked_at` (401) and logout has no effect (204).
  - **Forgot** replacing the token: reset waits for the user row, then its UPDATE finds the token deleted (invalid).
    If reset commits first, forgot then issues a fresh token.
  - Why the user lock matters: token consumption would stay correct without it (the token row lock serializes
    concurrent consumers), but reset would then lock the token row *before* the user row, the opposite of forgot,
    resend and verify, and could deadlock with them. A test holds the user row, lets reset wait, and deletes the
    token row from the holding transaction; it deadlocks if reset takes the locks in the wrong order.
  - Authenticate takes no locks: tokens stop working when reset commits. As in 016, a request authenticated just
    before the commit may still complete.
- **Emails:** the reset email after forgot commits; the password-changed email (no link, no token) after reset
  commits. Both in the background (007); a failed send is logged (`kind`) and never undoes the committed change.
  Links carry only `?token=`.
- **HTML page** (006, reset only; the verify-email page came in 019): `GET /reset-password?token=…` renders a
  form with the token in a hidden field, without touching the database (prefetch-safe); a malformed token gets a 400
  "link can't be used" page that doesn't echo input. `POST /reset-password` (form body only; a query-string token is
  ignored; 8 KiB limit) checks the confirmation field in the handler (a UI concern, no service call) and then calls
  the same `ResetPassword`: 200 done, 422 form with messages, 400 invalid link, 500 opaque page. Headers:
  `Cache-Control: no-store`, `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY`, `nosniff`, and a CSP allowing
  only inline styles and posting to this origin. No CSRF token: the form carries the secret itself, and making a
  victim's browser reset the attacker's own account gains nothing. `html/template` escapes everything rendered.
- **No migration.** The 00001 schema already has the `password_reset` purpose, the unique token-hash index (lookup),
  one active token per user and purpose (partial unique index, which holds even if a lock were skipped),
  `sessions_user_active` (revoke all), and cascading deletes.
- **Logs:** `auth: password reset requested` (`user_id`); `auth: password reset request had no effect` (no ids);
  `auth: password reset succeeded` (`user_id`, `sessions_revoked`); `auth: password reset failed` (`reason` =
  `malformed` | `invalid_token` | `invalid_password`, no ids: a dead token isn't linked to an account). Unexpected
  failures go through the opaque 500 path at ERROR. Never logged: email, passwords, tokens or their hashes, links.
- **Deployment:** the GET URL carries the token, so reverse-proxy and CDN access logs must drop query strings for
  `/reset-password`. The app itself logs route patterns, never URLs. The token also stays in browser history; it is
  single use, expires in 30 minutes, and the page loads nothing from other origins.
- **Deferred:** rate limiting (forgot can mail an account's owner repeatedly: email bombing, as with resend),
  cleanup of used and expired token rows (used rows are kept as an audit trail; a cleanup job may need an
  `expires_at` index), durable email delivery (outbox), a password-change endpoint for logged-in users, the
  verify-email page (done in 019).
