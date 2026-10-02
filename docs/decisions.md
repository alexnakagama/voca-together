# Architecture decisions

Short log of decisions and why they were made. Newest at the bottom.

## 001: Modular monolith: Go + PostgreSQL + REST
One deployable Go service, one database. Feature packages under `backend/internal/`.
Microservices would add operational cost with no benefit at this stage.

## 002: Opaque, DB-backed session tokens instead of JWT
- Access token (`vt_at_…`, 15 min) and refresh token (`vt_rt_…`, 30 days sliding, 90 days absolute), each 32 random bytes.
- Only SHA-256 hashes are stored (in `sessions`).
- Why: logout, revoke-all-on-password-reset and refresh-reuse detection all need server state anyway, so a JWT would add
  key management and algorithm pitfalls without making the system stateless. One indexed lookup per request is cheap
  at our scale. Clients treat tokens as opaque, so we can still switch later.
- Refresh rotates on every use. Presenting an already-rotated refresh token revokes the whole session.

## 003: argon2id for passwords
- OWASP baseline parameters (m=19 MiB, t=2, p=1), stored in PHC format so they can be raised later with rehash-on-login.
- Policy (NIST 800-63B): 10–128 characters, no composition rules, reject common passwords and passwords equal to the email.

## 004: One-time tokens (verification / reset)
- 32 random bytes; only the SHA-256 hash is stored, in `user_tokens` with a `purpose` column.
- Consumed with a single atomic `UPDATE … WHERE used_at IS NULL AND expires_at > now() RETURNING`, which makes them single-use and replay-safe.
- At most one active token per user and purpose (partial unique index). Issuing a new token deletes the old one.

## 005: No account enumeration
register, resend-verification and forgot-password always return 202. Login returns the same 401 for an unknown email
and a wrong password. `email_not_verified` (403) is returned only after the password is correct.

## 006: Email links open small backend-served HTML pages
- GET renders a confirm button or form, and only POST consumes the token (so mail-scanner prefetch is harmless).
- The pages call the same service as the JSON API, so in-app deep links can be added later with client work only.

## 007: Email behind an interface
`email.Sender` with a dev `LogSender` and a test `Recorder`. The production provider is Resend (019).
- `email` handles delivery only. `auth` owns the content, links and expiry wording. The dependency runs
  `auth → email`, never the reverse.
- Production must never run `LogSender`: it logs message bodies, which contain live tokens.
  There is no silent fallback (production requires Resend, 019).
- Auth sends email in the background (so response timing doesn't reveal accounts), with its own timeout
  derived via `context.WithoutCancel`, never the finished request's context. `Recorder` rejects cancelled
  contexts, so tests catch that mistake.

## 008: Minimal libraries
- Backend: stdlib `net/http` routing (Go 1.22+ patterns), pgx, goose, x/crypto, x/time/rate.
- Flutter: `http`, `flutter_secure_storage`, and `ChangeNotifier` for state.

## 009: Passwords are NFKC-normalized before hashing and policy checks
- Every password is normalized with Unicode NFKC before it is hashed, verified, or checked against the policy.
  The 10–128 character limits count Unicode code points after normalization.
- Why: the same visible password can arrive as different bytes. "ñ" can be one code point or "n" plus a combining
  tilde (from copy-paste, password managers, decomposed text sources, or Korean syllables vs separate letter parts).
  Japanese and Chinese input methods can also type Latin letters in full-width form (`ｐａｓｓ`). Without
  normalization, those users fail to log in for no visible reason. That matters for a language-exchange audience.
- Standards: NIST SP 800-63B recommends NFKC or NFKD before hashing. RFC 8265 (PRECIS OpaqueString) uses NFC and
  keeps full-width characters distinct. We follow NIST, which the password policy already cites.
- Cost: NFKC merges compatibility variants (`ﬁ` → `fi`, `²` → `2`, full-width → ASCII), a negligible entropy loss.
  ASCII passwords are unaffected.
- Constraint: **never change the normalization form without a migration path.** Once hashes exist, a change would
  lock out affected users. A migration would verify against both forms during a transition and rehash on login.

## 010: Email addresses are restricted to printable ASCII
- Addresses are trimmed and lowercased, and must be a plain `local@domain` in printable ASCII: no display names,
  quoted local parts, comments, IP-literal domains, or domains whose final label is all digits.
- Why ASCII-only:
  - **Delivery:** non-ASCII addresses need SMTPUTF8 support end to end, which email providers support unevenly.
    Verification is mandatory, so an address we can't reliably email can't complete signup.
  - **Look-alikes:** Unicode confusables (Cyrillic `а` vs Latin `a`) would allow distinct accounts that look identical.
  - **Canonicalization:** Unicode addresses have several normalization and case-folding forms, so one mailbox could
    map to several stored strings, i.e. duplicate accounts.
- Header-injection safety comes from rejecting whitespace and control characters, not from the ASCII restriction.
- Cost: users whose address contains non-ASCII characters can't register. Users of internationalized domains can
  still type the domain's ASCII (punycode) form (`ana@xn--espaa-rta.es`). Clients must show a clear message.
- Possible later relaxation: accept Unicode domains by converting them to punycode (`golang.org/x/net/idna`) before
  storing. Non-ASCII local parts only if a real need appears.

## 011: Registration (M1 step 4)
- `POST /v1/auth/register` answers 202 `{"status":"accepted"}` for a new **and** an existing address. The existing
  account is left untouched (no password change, no new token) and its owner gets the "account exists" email.
  Both paths hash the password and send email in the background, so timing doesn't reveal accounts either.
- User and verification token are inserted in one transaction (`ON CONFLICT DO NOTHING` on the email, so concurrent
  sign-ups for one address are safe). Email is sent only after commit.
- Verification links expire after **24 hours** (DB clock). Tokens use `NewToken("")`, without a prefix.
- Error responses: `{"error":{"code":"…"}}`; `validation_failed` (422) adds `"fields":[{"field":"…","code":"…"}]`
  with all failing fields. Malformed JSON, unknown fields, trailing data or a body over 8 KiB: 400
  `invalid_request`. Anything else: 500 `internal_error`, details only in server logs.
- **If the email fails after commit**, the failure is logged (kind and error only) and the response stays 202;
  there is no retry. The unverified account remains and the user recovers with resend-verification, which replaces
  the token. Durable delivery (an outbox) is deferred.
- Production never uses `LogSender` (007); it sends through Resend (019).
- **Deferred to the rate-limiting/hardening step:** registration can be abused to send email to any address
  (email bombing) and to spend argon2 CPU and memory (19 MiB per hash); background sends aren't capped either.

## 012: Email verification and resend (M1 step 5)
- `POST /v1/auth/verify-email` `{"token"}` → 200 `{"status":"verified"}`. It never creates a session: login stays a
  separate step. Every unusable token (malformed, unknown, expired, used, other purpose) gets the same 422
  `validation_failed` with `{"field":"token","code":"invalid"}`, so responses reveal nothing about token history.
  Malformed tokens are rejected before any database access.
- **Used tokens are rejected, even for verified accounts** (not reported as success). Single use stays a uniform
  rule, and an old link can't reveal an account's state. Double clicks are harmless (GET never consumes, see 006);
  clients say "invalid or already used, try logging in". If a live token ever reaches an already verified account,
  consuming it succeeds and keeps the original `email_verified_at`.
- `POST /v1/auth/resend-verification` `{"email"}` → 202 `{"status":"accepted"}` for unknown, unverified and verified
  addresses alike. Only an unverified account gets a new token (the old one is deleted, 004) and an email, sent in the
  background after commit. Nothing else is written or sent, and the password is never touched.
- **Lock order:** a transaction that changes an existing user's `user_tokens` rows first locks the `users` row
  (`FOR UPDATE`). Verification, resend and (later) password reset follow it, so they serialize per account and
  can't deadlock. Token consumption itself is still the conditional `UPDATE` of 004; the lock only orders.
  Consequences: concurrent uses of one token succeed once; a resend makes a concurrently submitted old token fail;
  a resend that waits for a verification sees the account verified and does nothing; concurrent resends leave
  exactly one live token (the last one emailed).
- No migration was needed; the 001 schema already has everything.
- Accepted timing difference: resend for an unverified account does one DELETE and one INSERT more than for other
  addresses (well under network jitter); email is sent in the background in all cases.
- **Deferred:** rate limiting (resend can mail any unverified account repeatedly: email bombing, together with
  register); the HTML pages behind emailed links (006; reset page in 017, verify page in 019); cleanup of used and expired
  token rows.

## 013: Login and session issuance (M1 step 6)
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

## 014: Refresh-token rotation (M1 step 7)
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

## 015: Logout (M1 step 8)
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

## 016: Access-token authentication and `GET /v1/me` (M1 step 9)
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

## 017: Password reset (M1 step 10)
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

## 018: Authentication hardening and abuse protection (M1 step 11)
- **Two limiter layers, in process** (`internal/ratelimit`: a token bucket per key, one mutex, no goroutine, no
  dependency). Per client IP in `server`, wrapped around individual routes in `server.New` like `authn`; per account in
  `auth.Service`, which owns `NormalizeEmail`. `main` builds both (`server.NewIPLimits`, `auth.NewAccountLimits`); a
  nil limiter allows everything, so tests pass zero values. `x/time/rate` (008) was not adopted: a ~100-line bucket
  gives fixed-size keys, a lossless sweep and a fake clock with no dependency.
- **Per IP** (checked before the body is read, so junk costs a token too):
  | Bucket | Routes | Burst | Refill |
  |---|---|---|---|
  | `ip_login` | login + google (shared, 020) | 20 | 1 / 3 s |
  | `ip_register` | register | 10 | 1 / min |
  | `ip_email` | resend-verification + forgot-password (shared) | 5 | 1 / 2 min |
  | `ip_token` | verify-email + reset-password JSON + reset form POST (shared) | 10 | 1 / 6 s |
  | `ip_refresh` | refresh | 60 | 1 / s |

  Generous enough for a household, a classroom or a carrier NAT behind one address; they bound what one source can
  make us do (argon2, outbound email, DB load). Token guessing is already infeasible (256-bit), so `ip_token` only
  bounds load. Not limited: `/healthz`, `GET /reset-password` (no DB), logout and `/v1/me` (malformed tokens never
  reach the DB; general API limiting comes with the rest of the API).
- **Per account** (key: SHA-256 of the normalized address, so no addresses sit in memory):
  `account_login` 10 then 1/min (≈ 1 450 guesses a day at most against one account, from any number of IPs);
  `account_mail` 5 then 1/h, **shared by register, resend and forgot** (≤ ≈ 29 emails a day reach any mailbox from
  us, whatever the mix of endpoints). Every attempt consumes, whatever its outcome (no refund on success), so the
  limit shows nothing about outcomes.
- **No enumeration:** the per-account key is the address **whether or not an account exists**, the check runs after
  input validation (422 first) and **before any DB or argon2 work**, and unknown addresses consume too (forgot and
  resend send them nothing but still count). Unknown, unverified and verified addresses therefore get the identical 429
  on the identical attempt with the same `Retry-After`, at the same cost.
- **Accepted cost:** anyone can keep a victim's `account_login` or `account_mail` bucket empty (about one request a
  minute, or five an hour). The victim then waits for `Retry-After`; no token or session is affected. Every per-account
  limit has this property; risk-based challenges are deferred.
- **429** `{"error":{"code":"rate_limited"}}` with `Retry-After` (whole seconds, rounded up, ≥ 1) and
  `Cache-Control: no-store`; the reset form gets a "Too many attempts" page. A 429 did nothing, so retrying after
  `Retry-After` is safe everywhere, **including refresh with the same refresh token** (an explicit exception to 014's
  "never retry a refresh": that rule is about responses that may have been lost, a 429 never rotated anything).
- **Limiter memory:** ≈ 100 bytes per key; entries whose bucket is full again are swept (lossless: a full bucket
  equals a missing one) at most once a minute, lazily inside `Allow`. Each limiter tracks at most 100 000 keys (≈ 10 MB;
  seven limiters ≈ 70 MB worst case). A new key at the cap is **allowed untracked** and logged. Principle: *limiters
  degrade open, resource bounds degrade closed.* Failing closed would let anyone who can mint keys (attacker-chosen
  emails, IPv6 /48s) lock everyone out; the process stays safe regardless, because argon2 slots, the queue timeout, the
  DB pool, the request deadline and the email cap are hard bounds. Logs are aggregated per limiter and minute
  (`ratelimit: rejected` with a count; WARN when the cap is hit) and never contain keys.
- **Single instance only:** buckets are per process, so N instances behind a load balancer give ≈ N× the limits, and
  a restart refills every bucket (one extra burst per deploy, accepted). Before scaling out, move limiting to the edge
  (proxy/CDN per-IP limits) or to shared state. Redis would be exact but is a new stateful service to run and secure,
  with its own failure mode on the login path; not justified for one instance. The first candidate is a
  PostgreSQL-backed counter (no new infrastructure, one write per limited request).
- **Client IP and trusted proxies:** the app speaks plain HTTP; TLS ends at a reverse proxy. `TRUSTED_PROXY_HOPS=N`
  takes the client IP from the N-th `X-Forwarded-For` entry from the right (all XFF headers as one list; entries
  further left are client-controlled and never used); 0 uses the TCP peer. This is valid **only under the deployment
  invariant that the server is reachable exclusively through those N proxies**. If the server is also exposed directly
  while trusting XFF, any client can send its own header and choose its IP, i.e. its bucket: **client IP spoofing**.
  Keep the listener on a private network or firewall it to the proxy. Fail-safe: when the chain is shorter than N or
  the entry isn't an IP, the TCP peer is used, so such requests share the proxy's bucket rather than pick their own.
  IPv4-mapped addresses are unmapped and IPv6 is keyed by its /64 (one subscriber usually holds a whole /64).
  Production requires `TRUSTED_PROXY_HOPS` to be set explicitly (0 included), so the topology is always stated.
- **argon2 queue bound** (closes 013's deferral): waiting for a hash slot ends after 5 s with `auth.ErrOverloaded` →
  503 `service_unavailable`, `Retry-After: 5`. One queue serves every account state, so a 503 reveals nothing.
- **Request deadline:** each request context gets 10 s (below the 15 s `WriteTimeout`, so the 503 can be written;
  the `http.Server` timeouts never cancel the context, so a request could otherwise wait indefinitely for a DB
  connection). `context.DeadlineExceeded` → 503 (WARN); a client disconnect (`Canceled`) stays an opaque 500.
  **The deadline can fire after a transaction committed but before the response is written.** The client then sees a
  503 for work that was done, exactly like an unknown COMMIT outcome (017), which stays as is. What a retry does:
  - Safe, same end state: **verify-email** and **reset-password** (the retry gets `token:invalid`: the change stands,
    and the client tells the user to log in); **logout** (idempotent 204).
  - Safe, with a side effect: **register**, **resend-verification**, **forgot-password** (another email, or nothing for
    an existing account; each retry spends `account_mail`); **login** (another session; the orphaned one expires).
  - Not retriable with the same credential: **refresh**. If the rotation committed, the old refresh token is now the
    previous one and retrying it is reuse: the session is revoked (014) and the client logs in again, as after any lost
    refresh response. Clients must treat a 503 from refresh like a network error, and a 429 as safe to retry.
- **Email is best effort until a durable outbox exists.** A 202 from register, resend or forgot means *accepted*, never
  *delivered*. At most 32 background sends run at once (≥ 3 emails/s even if every send takes its 10 s timeout); when
  all are busy the message is **dropped** and logged (`auth: email dropped, too many sends in flight`, `kind` only). The
  response is unchanged, so neither enumeration nor timing is affected; the user asks again (within `account_mail`).
  This bounds goroutines when a slow provider meets abuse. A future outbox replaces `sendInBackground`'s body (insert in
  the request's transaction, deliver from a worker) without changing callers, and makes the drop policy obsolete.
- **Retention cleanup:** `auth.Service.RunCleanup`, started by `main` (first run after 1 min, then hourly; stopped
  and waited for on shutdown). It deletes, in 1 000-row batches, sessions whose absolute expiry, sliding refresh expiry
  or revocation is more than **30 days** past, and one-time tokens (used or not) that expired more than **30 days**
  ago; users are never deleted. Responses don't change: those tokens were already rejected. Rows are taken with
  **`FOR UPDATE SKIP LOCKED`**, so cleanup **never waits for a lock** and can't join a deadlock cycle with the token
  flows (user row, then token rows) or refresh/logout (one session row); a locked row is left for the next run.
  Deleting a referencing row locks nothing in `users`. Idempotent, so several instances may run it. Logs
  `auth: cleanup` with counts only.
- **Headers:** every response gets `X-Content-Type-Options: nosniff`, `Content-Security-Policy: default-src 'none';
  frame-ancestors 'none'` and `Referrer-Policy: no-referrer`; the reset page replaces the CSP with its own (017).
  `Strict-Transport-Security: max-age=31536000` in production only (no `includeSubDomains`: we don't own the policy of
  sibling hosts). Register, verify-email and resend now also send `Cache-Control: no-store`, like every other auth route.
- **Transport and config:** `MaxHeaderBytes` 32 KiB (default 1 MiB). `APP_BASE_URL` must not carry credentials, a
  query or a fragment (links append a path and `?token=`), and in production must not be localhost, a loopback or the
  unspecified address.
- **Password reset** needs nothing beyond the above: forgot is limited per address and IP, reset submissions per IP,
  tokens are 256-bit, single use and live 30 min, argon2 runs only for a live token, and policy failures don't consume
  it (017).
- **No migration.** Limits live in memory; cleanup uses existing columns. Its predicates scan both tables once an hour
  in small batches, fine at M1 scale; an `expires_at` index waits for measured need.
- **Deferred:** shared or edge rate limiting before running several instances; a per-user session cap (bounded by
  `account_login` for now; revoking other sessions inside login needs its own lock analysis, since two concurrent logins
  could deadlock); logout-all, session list, rotation lineage (014, 015); risk-based challenges and failed-login
  notifications; metrics, request IDs and IP logging for abuse forensics (needs a privacy decision); durable email
  outbox and a production email sender (sender and verify-email page done in 019); enforcing TLS in `DATABASE_URL`; the
  verify-email page; password change for logged-in users.

## 019: Production email delivery with Resend (M1 step 12)
- **Provider: Resend** (`email.ResendSender`, `POST https://api.resend.com/emails`), called with a stdlib `net/http`
  client rather than the Resend SDK (008): one JSON request needs no dependency, and owning the client lets us set
  timeouts, redirects and error handling ourselves. `email.Sender` stays provider-agnostic: `auth` builds
  `email.Message` (`To`, `Subject`, `Text`, optional `HTML`) and never knows which sender runs.
- **Sender selection** (`newEmailSender` in `main`, logged by provider name only, never the key or sender address):
  | `ENV` | Sender |
  |---|---|
  | `production` | Resend, always; `RESEND_API_KEY` and `EMAIL_FROM` are required, startup fails without them |
  | `development` | Resend only when `RESEND_API_KEY` is set (then `EMAIL_FROM` is required), `LogSender` otherwise |
  | `test` | `LogSender`, always; both variables are ignored even if set, so tests never reach the provider |

  There is no silent fallback: production never runs `LogSender`, which logs live tokens (007). `main` re-checks the
  key even though `config.Load` already requires it. `LogSender` logs only the text body.
- **`RESEND_API_KEY`** is a secret. Config holds it as `config.Secret`, redacted in every fmt verb, slog, JSON and text
  marshalling (a pointer inside, so even `%p` shows only an address); `ResendSender` redacts it the same way. Only
  its shape is checked (printable ASCII without spaces, `re_` prefix), which also keeps it safe in a header. No error,
  log or startup message echoes it, nor `EMAIL_FROM` (a misplaced key could land there).
- **`EMAIL_FROM`** must be on a domain verified in Resend and already be in canonical form: `local@domain` or
  `Name <local@domain>` (surrounding space trimmed). Any other spelling `net/mail` accepts is rejected (comments,
  quoted names or local parts, bare `<local@domain>`, repeated spaces), as are non-ASCII, a display name with anything
  but ASCII letters, digits and single spaces, and a domain without a dot or with an IP literal. What is validated is
  exactly what is sent.
- **Content:** verification and password-reset emails have a plain-text body (canonical, always present) and an HTML
  alternative (`auth/templates/action_email.html`: table layout, inline styles, no scripts, images or external
  resources, the link both as a button and as visible text; `html/template` escapes every field). If HTML rendering
  failed, the message would go out as text only. The account-exists and password-changed notifications are text
  only. Links are `APP_BASE_URL` + `/verify-email` or `/reset-password` + `?token=…` and nothing else.
- **Pages (006):** `GET /verify-email?token=…` now exists alongside `GET /reset-password`. Both render without
  touching the database or using the token (prefetch-safe: mail scanners change nothing); a malformed token gets a
  400 invalid-link page that doesn't echo input. `POST /verify-email` (form body only, query token ignored) calls the
  same `VerifyEmail`: 200 done, 400 invalid link (every unusable token alike), 429 / 503 / 500 pages. It shares
  `ip_token` with the JSON routes and the reset form (018); the GET, like `GET /reset-password`, has no limit; headers and CSP are the reset page's (017). Proxy and CDN
  access logs must drop query strings for `/verify-email` too.
- **Request hardening:** `User-Agent: vocatogether-backend` (Resend rejects requests without one), `Accept` and
  `Content-Type: application/json`, bearer auth. Timeouts: 10 s per request (client timeout, whatever the caller's
  context allows), 5 s TLS handshake, 10 s response header; the caller's context (the 10 s background send timeout,
  018) bounds it as well. At most **64 KiB** of the response is read; normal responses are drained and the connection
  reused, oversized ones are truncated and the connection dropped. **Redirects are not followed**: a 3xx is an
  error, since following it would resend the message (and possibly the key) elsewhere.
- **Error sanitization:** errors carry only the HTTP status and the provider's error `name` if it matches
  `^[a-z_]{1,64}$` (e.g. `validation_error`). The provider's free-text `message` is never used (it may repeat request
  data). Errors never contain the key, recipient or bodies; a caller's cancellation or deadline is reported as such.
  On a 2xx, a failed read of the (unused) body doesn't fail the send. `ResendSender` itself never logs.
- **No retries and no `Idempotency-Key` yet**, and **no durable outbox**: delivery stays best effort exactly as in
  018 (bounded in-flight sends, excess dropped, a failed send logged with `kind` and never undoing committed work).
  A 202 still means accepted, not delivered.
- **Tokens reach Resend.** One-time tokens are in the email bodies, so the provider receives, and may retain, live
  links; that is inherent to sending them by email. Mitigations: 256-bit single-use tokens with short lives (24 h
  verification, 30 min reset), consumed only by POST. **Resend click and open tracking must stay disabled** on the
  sending domain: click tracking rewrites links through the provider's redirector (the token would pass through
  another URL and its logs) and open tracking adds a remote image. The sender doesn't set tracking per request;
  this is a deployment requirement on the Resend domain settings.
- **No migration.** Configuration and code only.
- **Deferred:** durable outbox (with retries and an `Idempotency-Key` per message); bounce and complaint handling
  (webhooks, suppression); provider-specific rate limiting (our sends are bounded only by 018's in-flight cap and
  per-account limits); HTML versions of the notification emails (account exists, password changed).

## 020: Google sign-in and passwordless accounts (M1 step 13)
- "Continue with Google", built in stages 1–7 and recorded here in stage order: passwordless accounts (stage 1, the
  bullets up to Stage 2), the ID-token verifier (2), store (3), service (4), endpoint (5), configuration (6), and the
  backend contract with deferred work (7, at the end). Google only establishes identity; sessions stay the vt
  sessions of 002.
- **Migration 00003:** `users.password_hash` may be NULL for accounts created with Google
  (`users_password_hash_not_empty` forbids `''`, so an empty string never stands in for "no password").
  `user_identities` (`provider` IN ('google'), `subject` = Google `sub`, 1–255 bytes) is unique per
  `(provider, subject)` (one owner per identity) and per `(user_id, provider)` (one Google identity per user), and is
  deleted with its user. `google_id_token_uses` (SHA-256 of an accepted ID token, `expires_at`) is created for the
  later single-use check; nothing writes to it yet.
- **Invariant: every user has at least one authentication method**, a password or an identity. A deferred constraint
  trigger (`users_auth_method_required`, checked at COMMIT) enforces it on user insert, `password_hash` updates, and
  identity delete or move. It raises 23514, and deleting the whole user is allowed. A passwordless user and its
  identity must therefore be inserted in one transaction. Under READ COMMITTED the check can't see other
  transactions' uncommitted changes, so any future flow that removes a method must lock the user row `FOR UPDATE`
  (lock order, 017). Rolling 00003 back fails while passwordless users exist, rather than deleting them.
- **Password login** of a passwordless account returns the same 401 `invalid_credentials` as a wrong password, after
  the same argon2 work: the dummy hash stands in for the missing one (013, 005). The log reason is `no_password` (with
  `user_id`), deliberately more specific than the response, so support can see why. Clients can't tell it apart.
- **No mailbox recovery for passwordless accounts.** `email_verified=true` from Google proves only that Google once
  verified the address. A third-party mailbox (not Gmail or Workspace) can later belong to someone else while the
  Google account stays with its original owner. If reset worked, the new mailbox owner could set a password and share
  or take over the account. So:
  - forgot-password answers the usual neutral 202 and spends `account_mail`, as always, but **creates no reset
    token**. It sends only a no-link notice ("Your VocaTogether account signs in with Google").
  - reset-password also refuses a passwordless owner as defense in depth: the unlocked lookup filters it out before
    any argon2 work, and the reset transaction refuses it under the user lock. It gets the uniform `token:invalid`,
    and nothing is consumed or written.
  - This differs from 017's "mailbox wins" rule for unverified password accounts, which is unchanged. Accepted
    costs: a user who loses their Google account needs support, and a former owner of a third-party address can
    keep it occupied. Availability was traded for the account's confidentiality.
- **Account-exists email** (011) now names both ways in: password, or "Continue with Google" for accounts created
  with Google, plus "Forgot password" for a forgotten password. It still carries no link.
- **Logs:** `auth: password reset requested for passwordless account` (`user_id`); email kind
  `passwordless_account`. Never logged: the email address.
- **Stage 2: ID-token verifier** (`internal/googleid`; recorded in stage 7). Local verification with the standard
  library rather than `google.golang.org/api/idtoken` (008). The package checks one narrow token profile, knows nothing
  about users, sessions, the database or HTTP routes, and leaves account policy and replay protection to `auth`.
  - **Order of work** bounds what a token can make us do: size and structure, then header and payload syntax, all
    before any key lookup (a malformed token never causes a fetch); then the signature; only then the claims.
  - **Token profile:** at most 4096 bytes; exactly three non-empty segments of strict unpadded base64url; a header of
    exactly `alg` = `RS256`, a printable-ASCII `kid` and optionally `typ` = `JWT`. Any other header member (`crit`,
    `jku`, `x5u`, `jwk`, `x5c`, …) is rejected, never ignored, so nothing in a token chooses the algorithm or the key
    source. The signature is RSASSA-PKCS1-v1_5 with SHA-256 over the received bytes, with the algorithm fixed in code.
  - **Claims**, each with exactly its JSON type (nothing coerced; an array `aud` is rejected):
    - `iss` is `https://accounts.google.com` or `accounts.google.com`;
    - `aud` is a string **exactly equal** to a configured client ID (no trimming, case folding or prefix matching);
    - `exp` > `iat`;
    - the token verifies while now < `exp` + 1 min, and `iat` and `nbf` (if present) may be at most 1 min in the
      future;
    - `sub` is 1–255 bytes of printable ASCII, kept exactly as issued;
    - `email`, `email_verified` and `hd` are optional but typed when present.

    Unknown claims, `azp` and `nonce` included, are ignored and never returned.
  - **Returned:** `googleid.Claims` with `Subject`, `Email`, `EmailVerified`, `HostedDomain` (as Google asserted them,
    unnormalized) and `AcceptedUntil` (`exp` + the skew, the instant from which `Verify` rejects the token; Stage 3
    uses it as the replay record's expiry). Claims redact themselves in every fmt verb, slog, JSON and text
    marshalling, behind a pointer so `%p` shows only an address.
  - **Errors:** `*InvalidTokenError` (`ErrInvalidToken`) with a fixed, loggable reason (`malformed`, `unsupported_alg`,
    `unknown_key`, `bad_signature`, `wrong_issuer`, `wrong_audience`, `expired`, `issued_in_future`, `bad_subject`,
    `bad_claims`); `*UnavailableError` (`ErrUnavailable`) when no usable key can be obtained, meaning nothing was
    decided about the token; or the context's error. No error carries token contents.
  - **Keys:** `https://www.googleapis.com/oauth2/v3/certs`, a fixed https URL that nothing in a token can change.
    - Fetched lazily, only when a token needs a key that isn't held fresh; never in the background.
    - At most one fetch runs at a time, in its own goroutine with a 5 s timeout, detached from the request: a client
      that disconnects stops waiting without aborting the fetch others wait for.
    - At most one fetch attempt a minute, successful or not. Unknown `kid`s are attacker-chosen, so junk tokens
      can't make us call Google more often.
    - Freshness: the response's `Cache-Control` `max-age`, clamped to [5 min, 24 h]; 1 h when it is missing or
      malformed. A set past freshness stays usable for a 6 h **stale grace, only while refreshing it fails**.
    - An unknown `kid` against a current set is `unknown_key`. If the refresh just failed, it is unavailable (it might
      be a rotated key we couldn't fetch). No usable set at all is unavailable.
    - Response limits: body ≤ 64 KiB, ≤ 16 keys, no redirects, no cookies (stage 6 adds a 16 KiB header cap).
    - Only RSA keys usable for RS256 (`alg` absent or `RS256`, `use` absent or `sig`, no `key_ops`), with a
      2048–8192-bit odd modulus and exponent 65537. A kid on two usable keys rejects the whole set.
  - **`azp` is not checked; the exact `aud` match is the security boundary.** Sources checked 2026-10-01: Google's
    ID token verification guides, its OpenID Connect claim reference, and the Android Credential Manager
    implementation guide.
    - *Documented by Google:* a backend must check the signature, `iss`, `exp`, and that `aud` "is equal to one of
      your app's client IDs". `azp` isn't on that list. `azp` is "the `client_id` of the authorized presenter",
      needed only when the party requesting the token isn't its audience. An Android app passes our **Web** client
      ID as its server client ID, so that is the `aud` we check. The guides say the `aud` check stops "ID tokens
      issued to a malicious app" from being used on our backend.
    - *Not documented; our inference:* which clients can obtain a token whose `aud` is our Web client ID. None of the
      pages above says so, or says the Android client must be in the same project. We assume such tokens go only
      to clients registered in our Google Cloud project, plus, for the Web client itself, its authorized JavaScript
      origins and redirect URIs. We expect an Android token's `azp` to name our Android OAuth client, but we rely
      on nothing about its value.
    - *Deployment assumption* (the preconditions of stage 6): the project contains only VocaTogether's clients, and
      the Web client has no authorized JavaScript origins or redirect URIs. Under that assumption and the inference
      above, every token with our `aud` came from one of our own clients. Checking `azp` would then only choose
      among them, and it would cost an allow-list of every Android client ID (debug keys, upload key, Play App
      Signing), where one missing entry breaks sign-in. Adding any other client to the project, or origins or
      redirect URIs to the Web client, voids the assumption and requires revisiting this.
    - *Implementation:* `azp` is ignored like any unknown claim and never returned, so it can't be used as an
      identity either.
  - **No nonce is requested or checked.**
    - *Documented by Google:* Credential Manager can attach an optional nonce to each request (`setNonce`) "to
      prevent replay attacks", and the server then checks that the token's nonce equals the one it issued. Google's
      ID token verification checklist doesn't require one.
    - *Documented by the Flutter plugin* (`google_sign_in` 7.2.0 API reference, checked 2026-10-01): the only
      `nonce` parameter is on `initialize`. That method "must be called exactly once" (calling it again is undefined
      behavior), and the nonce is "passed as part of any authentication requests". `authenticate` takes no nonce.
      Under that contract an app sets at most one nonce for the life of `GoogleSignIn.instance`, shared by every
      sign-in, so it can't send a fresh server-issued nonce per sign-in. A reused nonce binds a token to nothing.
      We haven't read the plugin's platform code, so this rests on its documented API, not on observed behavior.
    - *What replaces it:* single acceptance (Stage 3) covers replay. That is stronger than a nonce for replay: a
      nonce ties a token to one attempt but doesn't stop reuse within that attempt. Injection (a token the user
      didn't request in this flow) relies on the same inference and deployment assumption as `azp`. Only our own
      clients can obtain our `aud`, so an attacker can inject only a token for their own Google account, and the
      mobile app has no browser session to swap. Accepted residual risk: a token stolen *before* its legitimate use
      can be redeemed once by whoever presents it first. Body-only transport over TLS and never logging it limit
      that.
    - *Implementation:* a `nonce` claim, if present, is ignored.
  - Tests use `googleid.Fake` (no network, keys or clock) or a local TLS key server with a fake clock.
- **Stage 3: Google sign-in store** (`googleSignIn` in `store.go`; no service, endpoint or config yet). One READ
  COMMITTED transaction, in this order:
  1. **Single use of the ID token:** the first statement is
     `INSERT INTO google_id_token_uses … ON CONFLICT DO NOTHING RETURNING`, storing only SHA-256(raw token) and
     the verifier's `Claims.AcceptedUntil()` (exp plus its clock skew, the instant from which `Verify` rejects the
     token) as `expires_at`, so the row lasts exactly as long as the token would verify and auth holds no copy of
     the skew. A conflict is `googleReplayed`: nothing else is written and no session is
     created. Two requests with the same token serialize on the primary key. The token use commits with every outcome
     that follows (sign-in, ineligible, account exists): once a verified token reaches the database it is spent. A
     rollback (DB error) removes it, so a retry after a 500 isn't refused as a replay.
  2. **Resolve `(google, sub)`** and lock the user row `FOR SHARE` (017 lock order, like login). Found → session. The
     token's current email is ignored and `users.email` is never synced.
  3. Unknown identity: the store applies the `eligible` flag decided by the service (`googleIneligible`). It holds no
     email policy of its own.
  4. **Insert the user** (`password_hash` NULL, `email_verified_at = now()`) `ON CONFLICT ON CONSTRAINT
     users_email_key DO NOTHING`. On conflict the lookup runs again as a new statement: a concurrent first sign-in of
     the same subject committed user and identity together, so it is found (then a plain sign-in). Otherwise
     `googleAccountExists`, returning the existing user's id for logs. Nothing is linked and the account is untouched.
  5. **Insert the identity** `ON CONFLICT (provider, subject) DO NOTHING`. A conflict means the same subject raced with
     another email and lost: `errGoogleIdentityRace` rolls everything back (the loser's user too) and `googleSignIn`
     **retries the whole transaction once**. The retry consumes the token again (the rollback removed it) and finds
     the identity. A second race can't happen while identities are never deleted, so it is returned as an error.
     *Deviation from the plan:* the retry lives in the store wrapper, not in the service, so the SQL that causes the
     race and its handling stay together and are tested against PostgreSQL directly.
  6. **`insertSessionTx`**, extracted unchanged from `createSession`: the only session INSERT (same lifetimes on the
     DB clock, same user-agent handling). The caller must hold the user row lock. The auth-method trigger checks
     the new user at commit.
- **No deadlocks:** each attempt inserts at most one row per unique key (token hash, email, subject), always in that
  order, and only waits for a transaction inserting the same key. That transaction inserted its earlier keys before
  this one could wait on it and never waits back on this one's. The `FOR SHARE` waits only for `FOR UPDATE` holders
  (reset, verification), which never touch the Google tables. PostgreSQL's unique constraints are the only arbiter;
  no advisory locks or extra mechanism.
- **Cleanup:** `RunCleanup` also deletes `google_id_token_uses` rows past `expires_at` (no 30-day retention: past
  it the verifier rejects the token itself), in batches with `FOR UPDATE SKIP LOCKED`, logged as
  `google_token_uses_deleted`. This assumes the API servers' and the database's clocks are in sync (NTP), which the
  verifier's exp and iat checks already require.
- **Stage 4: `Service.SignInWithGoogle(ctx, rawIDToken, userAgent)`** (`google.go`; no endpoint or config yet).
  `NewService` takes a `googleid.Verifier`; `nil` means not configured (`main` passes `nil` until
  `GOOGLE_CLIENT_ID` exists; superseded by stage 6), and the router never builds Google infrastructure. In this order:
  1. Empty token → `*ValidationError` (`id_token:required`). No verifier → `ErrInvalidGoogleToken`.
  2. **Verify.** Any rejection → `ErrInvalidGoogleToken`, logged with the verifier's fixed reason. Keys
     unavailable → `ErrGoogleUnavailable` (nothing decided, nothing written: a retry with the same token is
     safe). Context errors are wrapped unchanged. Only verified claims are used from here on. Claims without
     `AcceptedUntil` are an internal error, since a zero expiry would let cleanup reopen replay at once.
  3. **Per-subject limit:** `account_login` keyed `"google:"+sub` (never collides with an email key), after
     verification (sub is unknown before) and before the database. A 429 writes nothing.
  4. **Email policy (service only):** a new account needs an email that is present, `email_verified`, and passes
     `NormalizeEmail`. This yields an `eligible` flag and the normalized address. It never fails early: a linked
     identity signs in whatever its email, which only the store can tell.
  5. Fresh vt tokens (hashes only to the store), then one `googleSignIn` call. The service has **no transaction,
     lock or retry** of its own, so there are no new lock orders and no double side effects. An unknown commit
     outcome is a 500 whose raw session tokens were discarded. The ID token may be spent, so the client retries
     with a new one.
  6. Outcomes:
     - signed in or created → `Credentials` exactly as `Login` returns them;
     - replayed → `ErrInvalidGoogleToken` (indistinguishable from an invalid token);
     - ineligible → `ErrGoogleEmailUnusable`;
     - email taken → `ErrAccountExists`.
     An unverified email is refused before the collision check, so it can't probe for accounts.
- **Logs:**
  - `auth: google sign-in succeeded` (`user_id`, `session_id`, `new_account`);
  - `auth: google sign-in failed` (`reason`: a verifier reason, `invalid_token`, `not_configured`, `replayed`,
    `email_missing`, `email_unverified`, `email_invalid`, or `account_exists` with the existing `user_id`);
  - `auth: google keys unavailable` (Warn, fixed fetch `reason`).
  - Never logged: the ID token, its hash, the subject, the email or the user agent.
- **Stage 5: `POST /v1/auth/google`** (`handleGoogleSignIn` in `server/auth.go`). A thin handler shaped like refresh:
  `Cache-Control: no-store`, then `decodeJSON` (8 KiB `maxAuthBodyBytes`, which holds any token the verifier's
  4096-byte cap accepts), then one `SignInWithGoogle(r.Context(), id_token, r.UserAgent())` call (no retry; the
  service normalizes the user agent). The router builds no Google infrastructure and imports no `googleid`.
  - Request `{"id_token":"…"}`. **Only the body carries the token.** `Authorization`, the query string and other
    headers are ignored. Malformed, unknown-field, trailing or oversized bodies → 400 `invalid_request`. A missing,
    null or empty `id_token` → 422 `id_token:required`. Neither reaches the verifier.
  - 200 returns the login/refresh token response (`writeTokens`).
  - Errors:
    | Service error | Response |
    |---|---|
    | `ErrInvalidGoogleToken` (rejected, replayed, not configured) | 401 `invalid_google_token` |
    | `ErrGoogleEmailUnusable` | 403 `google_email_unusable` |
    | `ErrAccountExists` | 409 `account_exists` |
    | `ErrGoogleUnavailable` | 503 `service_unavailable`, `Retry-After: 5` |

    Shared mappings are unchanged: 429, deadline 503, `Canceled`/other 500. The 401 has **no `WWW-Authenticate`**:
    the ID token is a credential in the body, like login's (013), not an HTTP authentication scheme. The server
    adds no distinctions the service collapsed. Replay and every verifier reason stay one 401.
  - **Per IP:** shares `ip_login` with password login. Both are sign-in attempts (Stage 4 likewise reuses
    `account_login`), and a separate bucket would double what one source gets. The IP check runs before the body is
    read, and the per-subject check runs before the database. A 429 therefore never spends the ID token.
  - **503 or 500 after commit:** the deadline can fire after `googleSignIn` committed (018). The ID token is then
    spent, and a retry with it gets `invalid_google_token`. The client contract is: **after a 503 or 500, retry with
    a new ID token** (Google's SDKs mint one silently). The retry finds the account by subject. The orphaned
    session expires. A Google-keys 503 spent nothing, but clients can't tell 503s apart, so the same rule applies.
  - Until `GOOGLE_CLIENT_ID` is configured (stage 6), the verifier is `nil` and every request gets the 401. (Superseded by
    stage 6.)
- **Stage 6: configuration and wiring** (`config.loadGoogle`, `newGoogleVerifier` in `main`). No migration.
  - **One credential: `GOOGLE_CLIENT_ID`**, the **Web application** OAuth client ID, which the Android app passes as
    `serverClientId` and Google puts in `aud`. The verifier checks ID tokens locally against Google's public key set,
    so nothing exchanges a code or calls Google as the app. **Not required, on purpose:** a client secret
    (`GOCSPX-…`), an API key, a service account, and the Android client IDs (at most one appears in the ignored `azp`
    claim, see Stage 2). The client ID is public, a plain string, not a `config.Secret`.
  - **Format check (`googleid.ValidClientID`), structural only.** Google documents an example
    (`1234567890-abc123def456.apps.googleusercontent.com`) but no grammar, and older IDs have no dash, so the prefix is
    not constrained. It requires 1–255 bytes of printable ASCII without spaces, ending in exactly
    `.apps.googleusercontent.com` after a non-empty prefix. Nothing is trimmed or case-folded. This catches
    misconfiguration (empty, a pasted client secret, stray whitespace). It is not the security boundary: that is
    `Verify`'s exact `aud` match, and a well-formed wrong ID still fails closed (every sign-in 401,
    `wrong_audience`). Syntax can't tell a Web client ID from an Android one, so using the Web ID is a deployment
    requirement. Errors name the variable and never echo the value, since a secret or token could have been
    pasted there.
  - **By environment** (the 019 pattern):
    | `ENV` | `GOOGLE_CLIENT_ID` | Google sign-in |
    |---|---|---|
    | `production` | required, valid | enabled; **startup fails** if missing or invalid |
    | `development` | optional, validated when set | enabled when set; unset → disabled (`nil` verifier: 401, `not_configured`) |
    | `test` | ignored, even if set or invalid | disabled, so a test binary never reaches Google |

    Disabled happens only when the variable is absent outside production. A missing configuration is a startup
    error, never a runtime `ErrGoogleUnavailable`.
  - **Composition:** `config.Load` validates, then `main` builds the verifier with `newGoogleVerifier(cfg, logger,
    googleid.Options{})` (Google's key set, the hardened client, the real clock). That happens right after the email
    sender and **before the database connects**, so a configuration failure exits at once. `main` re-checks
    presence and format, as `newEmailSender` does, so production never falls back to disabled. Disabled is an
    untyped `nil` (a typed nil pointer would look configured). The verifier is passed once, to `auth.NewService`;
    `server` still imports no `googleid`, and `auth` reads no configuration. Tests inject `googleid.Fake` or a local
    TLS key server. Startup logs `google sign-in` with `status` `enabled` or `disabled`, never the client ID.
  - **No startup prefetch** of Google's keys: it would make the API's availability depend on Google's at boot.
    Keys are fetched on the first sign-in. An outage stays a runtime 503 (Stage 5), and the stale grace covers
    later outages. The verifier owns no goroutine but one in-flight fetch (at most 5 s), so shutdown has nothing to
    close.
  - **JWKS hardening:** response headers are capped at 16 KiB (`MaxResponseHeaderBytes`; net/http's default is
    1 MiB), on the default transport and on a clone of an injected `*http.Transport`. Everything else was already in
    place (Stage 2): body ≤ 64 KiB, ≤ 16 keys, 5 s timeout, no redirects, no cookies, Cache-Control max-age clamped to
    [5 min, 24 h], at most one fetch a minute (unknown kids included), 6 h stale grace only while refresh fails.
    Outbound proxies follow the standard `HTTPS_PROXY` variables.
  - **Deployment preconditions** (our assumptions, not Google guarantees; Stage 2 relies on them to leave `azp` and
    nonce unchecked): the GCP project holds only VocaTogether's clients, and the Web client has no authorized
    JavaScript origins or redirect URIs. Its client secret is never downloaded, used or deployed.
- **Stage 7: backend contract (summary) and deferred work.** Documentation only; no behavior changed. This
  summarizes the stages above, which keep the reasoning.
  - **Credentials, kept distinct:**
    | Credential | What it is | Where the backend uses it |
    |---|---|---|
    | Web OAuth client ID | `GOOGLE_CLIENT_ID`; public; the only Google configuration | the exact `aud` every accepted ID token must carry |
    | Android OAuth client ID | registered in Google Cloud for the Android app | nowhere: never configured or checked; at most it appears in the ignored `azp` claim (Stage 2) |
    | Google ID token | a JWT Google mints for the Web client ID (Android passes it as `serverClientId`) | only the body of `POST /v1/auth/google`, to establish identity |
    | VocaTogether access token `vt_at_…` | opaque, DB-backed, 15 min (002) | `Authorization: Bearer` on protected routes and logout |
    | VocaTogether refresh token `vt_rt_…` | opaque, DB-backed, 30 d sliding / 90 d absolute, rotated on use (002, 014) | the body of `POST /v1/auth/refresh` |

    **Only VocaTogether tokens are API credentials.** A Google ID token is never accepted by `requireAccessToken`,
    refresh or logout, and sign-in returns VocaTogether tokens exactly as login does. Not used anywhere: a Google
    client secret, API key, service account, Firebase, an OAuth authorization-code exchange, Google access or refresh
    tokens. The backend's only outbound call to Google is the public key-set fetch.
  - **Two rules for the ID token:**
    - It is **sent only in the body** of `POST /v1/auth/google` (`{"id_token":"…"}`, 8 KiB body cap). The
      `Authorization` header and the query string are ignored, and it is never a VocaTogether API credential.
    - The backend **accepts each verified ID token at most once.** The token-use record (SHA-256 of the token,
      expiring at `AcceptedUntil`) is the first write of the sign-in transaction. Once it commits, the token is spent
      whatever the outcome: 200, 403 or 409. A rollback removes it.
  - **Verification:** local, against Google's key set: RS256, `iss` Google, `aud` exactly `GOOGLE_CLIENT_ID`, `exp`,
    `iat` and `nbf` with 1 min skew, `sub` as the identity key (Stage 2).
  - **Accounts:**
    - A `(google, sub)` identity linked to an account signs in to that account, whatever the token's email says now;
      `users.email` is never synced from Google.
    - An unlinked identity creates a **passwordless** account (`password_hash` NULL, email verified) with its
      identity and a session in one transaction. The token's email must be present, `email_verified`, and pass
      `NormalizeEmail`, else 403. Any such address may create an account: there is no Gmail or Workspace-only
      restriction, and `hd` is not used for policy.
    - If the address already belongs to an account, the answer is 409 and nothing is linked or changed.
  - **Passwordless accounts** sign in only with their Google identity. Password login gets the uniform 401
    `invalid_credentials`. Forgot-password sends a no-link notice and creates no reset token, and reset-password
    refuses them. The deferred trigger guarantees every user keeps a password or an identity.
  - **HTTP** (every response `Cache-Control: no-store`):
    | Status | Code | Meaning | Token spent? |
    |---|---|---|---|
    | 200 | — | tokens, same body as login | yes |
    | 400 | `invalid_request` | malformed, unknown-field, trailing or oversized body | no |
    | 422 | `validation_failed` (`id_token:required`) | missing, null or empty `id_token` | no |
    | 401 | `invalid_google_token` | rejected by the verifier, already used, or Google sign-in not configured; no `WWW-Authenticate` | no new spend |
    | 403 | `google_email_unusable` | unlinked identity whose email is missing, unverified or invalid | yes |
    | 409 | `account_exists` | the email belongs to an existing VocaTogether account not linked to this Google identity | yes |
    | 429 | `rate_limited` + `Retry-After` | per-IP or per-subject limit | no |
    | 503 | `service_unavailable` + `Retry-After: 5` | Google's keys unavailable (nothing spent), or the 10 s deadline (may have committed) | unknown to the client |
    | 500 | `internal_error` | anything else, including an unknown commit outcome | unknown to the client |
  - **409 does not imply a password.** The existing account may have a password (verified or not) or may be a
    passwordless account created by a different Google identity with the same address. The client tells the user to
    use that account's existing way in. That can be password login (after verifying the email, or via "Forgot
    password" if needed) or the Google account the existing account was created with. Nothing links automatically.
  - **Retries:**
    - 400, 422 and 429 spend nothing. After a 429 the same token may be sent again after `Retry-After`.
    - A 503 for unavailable keys also spends nothing, but clients can't tell it from a deadline 503.
    - **After a 500, a 503 or no response the commit outcome is unknown, so the client gets a new Google ID token**
      (Google's SDKs mint one silently) and signs in again. The new token finds the account by subject. A session
      created by the lost attempt is orphaned and expires.
    - Replaying the old token gets the 401 `invalid_google_token`, like any unusable token.
    - Retrying a 403 or 409, with the same token or a new one, gives the same answer while the accounts are unchanged.
  - **Rate limits:** `ip_login` (20, then 1 every 3 s) per client IP, shared with password login, checked before the
    body is read. `account_login` keyed by `google:` + `sub` (never an email key), after verification and before any
    database work. Both are in process (018).
  - **Enumeration (exception to 005):** a 409 reveals that an address has a VocaTogether account. It needs a valid,
    unspent ID token in which Google asserts that address as verified (an unverified address gets 403 before the
    collision check), so only the holder of a Google account for that address can learn it. Accepted: that caller has
    a plausible claim to the address, and 005 still holds for everyone else.
  - **Configuration** (stage 6): production requires a valid `GOOGLE_CLIENT_ID` (the Web client ID, never an Android
    one) and fails at startup otherwise. In development an unset value disables Google sign-in (401). Test always
    disables it. Keys are not fetched at startup.
  - **Logging and privacy:** `auth: google sign-in succeeded` (`user_id`, `session_id`, `new_account`), `auth: google
    sign-in failed` (fixed `reason`), `auth: google keys unavailable` (Warn, fixed fetch reason), `google sign-in`
    at startup (`status` only). Never logged: the ID token or its hash, `sub`, the email, the user agent, or the client
    ID.
  - **Key-set failure behavior:** a Google or network outage becomes a 503 only when no cached key set is usable
    (fresh, or within the 6 h stale grace while refresh fails), or when a token names a key the cache lacks just after
    a refresh failed. Fetches are throttled to one a minute, with a 5 s timeout each (Stage 2, stage 6).
  - **Deployment assumptions:**
    - The Google Cloud project holds only VocaTogether's clients, and the Web client has no authorized JavaScript
      origins or redirect URIs.
    - The Web client's secret is never downloaded, used or deployed.
    - The Android `applicationId` is `com.vocatogether.app`. It is registered on the Android OAuth client in Google
      Cloud and is not backend configuration.
    - API servers and PostgreSQL keep NTP-synced clocks (verifier time checks; token-use expiry cleanup).
    - Outbound HTTPS to `www.googleapis.com` is allowed, directly or through the standard `HTTPS_PROXY` variables.
    - The API runs as a single instance behind the trusted-proxy setup of 018.
  - **Deferred:**
    - **Account linking:** linking a Google identity to an existing account (the 409 case), unlinking, and adding
      a password to a passwordless account. Any flow that removes an auth method must lock the user row (stage 1).
    - **`azp` check and a per-sign-in nonce**, required if the Stage 6 preconditions stop holding (another client in
      the project, Web origins or redirect URIs, e.g. for a Flutter web client), or once the client can send a
      fresh nonce per sign-in (a `google_sign_in` API change or a different client library).
    - **RISC / Cross-Account Protection:** Google account events (account disabled or deleted, sessions revoked,
      credential changes) are not received. VocaTogether sessions end only by their own expiry, logout or password
      reset.
    - Email sync from Google: an account keeps the address it was created with.
    - A support or recovery path for users who lose access to their Google account (stage 1).
    - Shared or edge rate limiting before running several instances (018).
    - Metrics and alerting for Google sign-in outcomes and key-set failures.
    - Client work: the Flutter/Android implementation, Google Cloud Console setup, and Play Store signing.

## 021: Flutter app shell: session state and routing (client stage 2)
- **`go_router`** (flutter.dev, BSD-3) is added to the Flutter libraries of 008. Routes are flat, top-level and
  parameterless, defined as constants in `Routes` (`lib/router.dart`); `go_router_builder` and code generation are not
  worth it until routes take parameters.
- **Session state:** `Session` (`lib/session.dart`) is a `ChangeNotifier` holding a `SessionStatus`: `unknown` (startup
  hasn't decided yet), `signedOut` or `signedIn`. It stores no tokens and does no I/O; whatever learns the status
  reports it through `markSignedIn` / `markSignedOut`, and listeners are notified only on an actual change. There is no
  way back to `unknown`. Until the token manager exists (Stage 3), `main` resolves startup to `signedOut` at once.
- **One navigation policy:** `authRedirect(status, uri)` is a pure function and the router's only redirect:
  `unknown` → `/splash`; `signedOut` → stays on `/login`, `/register` or `/forgot-password`, otherwise `/login`;
  `signedIn` → `/home`. Paths are matched exactly (query ignored), so unmatched or unexpected locations go to the
  status's default route instead of an error page, and every destination is final (no redirect loop). Screens may
  `go` to a route to express intent, but never decide access; the redirect runs on every navigation and, via
  `refreshListenable`, on every session change.
- **Ownership:** `main` is the composition root and owns the `Session` for the life of the process. The router is
  created and disposed by `VocaTogetherApp`'s `State` (recreated if the session instance changes); disposing it
  removes its listener from the session. No top-level mutable state, no service locator or state-management package.
- **Android deep links are off** (`flutter_deeplinking_enabled=false`): `MainActivity` is exported, so any app could
  otherwise choose our initial route. The redirect would contain it, but deep links (e.g. the email links of 006)
  need their own design and decision.
- Routes never carry tokens, email addresses or other personal data, and router diagnostics logging stays off.
- **Deferred:** token manager and session restore from `flutter_secure_storage` (014/015 client contracts), real
  screens, logout, Google sign-in, deep links and a return-to location after login, state restoration.

## 022: Design system, reusable auth widgets and localization (client stage 3)
- **Identity:** Material 3. `ColorScheme.fromSeed` with a deep-teal seed (`#00696B`); the four tertiary roles are the
  primary roles of a coral-seeded scheme (`#E8735A`) of the same brightness, so each on-color keeps the contrast
  Material computed for its pair (tests require ≥ 4.5:1 for every text pair used). Platform font (Roboto), system
  light/dark. Buttons are at least 48 dp tall; filled and outlined buttons are stadium-shaped; fields are outlined with
  a 12 dp radius. `Spacing` (4/8/16/24/32) and `Radii` in `lib/ui/theme.dart` are the only tokens: no theme
  extensions or per-component token classes until a real need appears.
- **Reusable widgets** (`lib/ui/widgets/`: `AppTextField`, `PrimaryButton`, `GoogleSignInButton`, `FormErrorBanner`,
  `AuthScaffold`) take data and callbacks by constructor, never import `Session`, the router or `AppConfig`, and hold
  only ephemeral UI state (password visibility, the tap latch). User-visible strings come from `AppLocalizations` or
  from the caller. Screens keep owning forms, `AutofillGroup`, validation and submission.
- **Fields:** `AppTextField.email` (email keyboard and autofill, no autocorrect) and `AppTextField.password`
  (obscured with a show/hide toggle; autocorrect, suggestions, smart punctuation and IME personalized learning off;
  autofill `password`, or `newPassword` for registration and reset). Neither trims nor validates. Server field errors
  (422 `fields`) are shown through `errorText`, which overrides any `Form` validator error.
- **Double submission:** the screen owns `busy` and passes it to `PrimaryButton`, which then shows a spinner, ignores
  taps and keeps its size and enabled colors. The button also drops a second tap in the same frame (before the parent
  can rebuild); the latch releases on the next frame, so it can't wedge. The keyboard's done action bypasses the
  button, so the submit handler remains the final guard and must check its own busy flag.
- **Google button** (presentation only; sign-in itself is a later stage): Google's light (`#FFFFFF` fill, `#747775`
  stroke, `#1F1F1F` text) or dark (`#131314`, `#8E918F`, `#E3E3E3`) button theme following the app's brightness, never
  the app's scheme; pill shape (matching `PrimaryButton`); 12 dp / 10 dp / 12 dp padding around a 20 dp logo; text
  "Continue with Google" (an approved label, localizable). The logo is Google's gradient G, cropped unaltered
  (pixel copy of the 20×20 dp region at offset 10,10) from the icon-only Square Light/Dark PNGs at @1x/@3x/@4x of
  `https://developers.google.com/static/identity/images/signin-assets.zip` (downloaded 2026-10-02), so each crop
  carries its theme's fill and must only be shown on that fill. It is painted with `Ink.image` so ripples cover it.
  Deliberate deviations: Roboto Medium 14/20 instead of Google Sans Medium (OFL, but only a 5 MB variable font;
  revisit with a subset if needed), a 48 dp minimum height instead of 40 dp to pair with `PrimaryButton` and meet tap
  target size, and a disabled appearance of our own (the whole button at 38% opacity, colors unchanged), since Google
  defines no disabled state and forbids recoloring the logo.
- **Errors** use `FormErrorBanner`: error-container colors plus an icon with a semantic label and the message text, so
  meaning never rests on color alone, inside a live region so TalkBack announces it. Mapping API error codes to
  messages is a later stage.
- **`AuthScaffold`:** a title (semantic header) and children in a scroll view that is centered when it fits, scrolls
  under the keyboard, small screens and large text, dismisses the keyboard on drag, and caps the content at 480 dp.
- **Localization:** `flutter_localizations` and `intl` (imported by the generated code; its version is pinned by
  `flutter_localizations`) join the Flutter libraries of 008. gen-l10n runs through `flutter: generate: true` with
  `l10n.yaml` (`nullable-getter: false`, `required-resource-attributes: true`, so every string needs a description).
  English only; the generated `lib/l10n/app_localizations*.dart` is committed so string-API changes show in review;
  never edit it by hand. `MaterialApp.router` gets the delegates, locales, themes and its title from l10n. Existing
  placeholder screens keep their hardcoded text until they are rebuilt.
- **Widget previews** (`lib/ui/previews/`) use `@VocaPreview`, a `Preview` subclass that applies the app theme for the
  preview's brightness through `wrapper` and the app localizations through `localizations`. The unstable
  `PreviewThemeData` interface is avoided. Previews are pure UI (no `dart:io`, plugins, HTTP, session or config);
  their text is sample content. `test/ui/previews_test.dart` builds every preview in both brightnesses.
- **Deferred:** a Google Sans subset, more locales, golden tests, app icon and native splash colors, screens adopting
  these widgets.

## 023: Networking, token storage and session management (client stage 4)
- **Layers** (constructor injection from `main`; no state-management or HTTP framework packages):
  `SessionManager` (`lib/session.dart`) → `TokenStore` + `AuthApi` + `AuthClock`; `AuthApi` and `AccountApi`
  (`lib/api/`) → `ApiClient` (`lib/api/api_client.dart`) → `http.Client`. `ApiClient` holds no auth state; the two API
  classes are stateless and the only code that knows routes (`ApiPaths`) and bodies; `SessionManager` holds the only
  in-memory tokens and owns every token policy. `Session` and its `markSignedIn`/`markSignedOut` are gone;
  `SessionStatus` and the router contract (021) are unchanged, with `unknown` meaning "restoring". Dependencies: `http`
  and `flutter_secure_storage` (008), plus `fake_async` for tests only.
- **Token boundary (enforced, not by convention).** The API is split by audience:
  - `AccountApi` (register, resend-verification, forgot-password): the only API screens may hold. None of its
    operations carries or returns a token.
  - `AuthApi` (login, google, refresh, logout, me, healthz): takes raw tokens and returns `AuthTokens`, so only `main`
    builds it and only `SessionManager` holds it.
  - `SessionManager`'s public API neither takes nor returns an app token: `status`, `restore()`,
    `signIn()`/`signInWithGoogle()` → `void`, `me()` → `Me`, `logout()` → `void`. Its generic request wrapper
    (`_authorized`, which hands the raw access token to a callback) is library-private; each protected route gets a
    typed method that calls it with a single `AuthApi` request. (The first version of this stage exposed it publicly as
    `authorized(call)`, which let any caller's callback receive the token; it was made private before stage 5.)
  - Screens and widgets may import from these layers only `account_api.dart`, `api_exception.dart`, `me.dart` and
    `session.dart`. Dart can't make a class visible to only some files, so `test/architecture_test.dart` enforces the
    rules over the real source: an import allowlist for every token-bearing library (`auth_api`, `api_client`,
    `auth_tokens`, `token_store`, `auth_clock`, `package:http`, `flutter_secure_storage`); no token type, token field
    or `vt_` prefix named in `lib/screens/` or `lib/ui/`; no token in a public `SessionManager` signature; and `main`
    passing only config and session to the widget tree. A Google ID token is not an app token: stage 5/6 screens pass
    it to `signInWithGoogle` (020).
  - Within the session layer the tokens are ordinary `String`s; the boundary is the layer edge, not an opaque type.
- **Transport (`ApiClient`):** the base URL is re-validated with `parseApiBaseUrl` (https required in release); paths
  must match `^/(healthz|v1(/[a-z0-9-]+)+)$` and are joined with `Uri.replace`, never concatenated, and the result must
  keep the origin with no query or fragment. GET/POST only. Headers: `Accept: application/json`,
  `User-Agent: VocaTogether-Android` (no version or device data), `Content-Type: application/json` only with a body,
  and `Authorization: Bearer` only when given a token that has the exact `vt_at_` shape (anything else is an
  `ArgumentError` that doesn't echo the value). **Redirects are never followed** (`followRedirects = false`; a 3xx is a
  protocol error), so a body or header carrying a token is never resent elsewhere. Each request has a timeout
  (15 s; logout 10 s; `/healthz` 5 s) that both completes an `AbortableRequest` trigger (closing the socket) and
  bounds the Dart future. Bodies are read streaming and capped at 64 KiB. 2xx: an empty body or `application/json`
  that decodes; 204 must be empty. 4xx/5xx: `ApiHttpException` from the error body when it is JSON, else status only.
  1xx and ≥ 600: `unexpectedStatus`. `http.ClientException` and `IOException` → `ApiNetworkException`. It never
  retries and never logs. The production client is one `IOClient` (10 s connect timeout) for the process.
- **Errors (`ApiException`, sealed):** `ApiHttpException(statusCode, code?, fields, retryAfter?)`,
  `ApiNetworkException`, `ApiTimeoutException`, `ApiProtocolException(failure, statusCode?)`. Codes and field names
  are kept only if they match `^[a-z][a-z0-9_]{0,63}$`; `Retry-After` only as delay-seconds (the backend's only form,
  018), clamped to [1 s, 24 h]. No exception holds a body, header, URL, token or the underlying error's message, and
  `toString` uses fixed formats.
- **Token shapes** are checked exactly as the backend issues them: `vt_at_`/`vt_rt_` + 43 characters of strict
  unpadded base64url (the last one's two low bits zero). Token responses must have `token_type` exactly `Bearer` and an
  integer `expires_in` in [1, 86 400]; anything else is a protocol error. `AuthTokens`, `StoredSession`, `Me` and the
  session's internals print `<redacted>`.
- **Storage (`TokenStore`, `SecureTokenStore`):** one key, `vt_session_v1`, holding one JSON value
  `{"v":1,"at","rt","exp":<epoch ms UTC>,"ttl":<s>}`, written with a single plugin call, so the tokens and the access
  expiry can never be split. Decoding is strict (exact keys, version, types and token shapes); corrupt data is cleared
  and the app starts signed out. Platform errors are wrapped without their message. It is the only persistent
  location of tokens: nothing goes to SharedPreferences directly, files, logs, URLs or widget state.
- **flutter_secure_storage 11.2.0, checked in its source** (pub cache, 2026-10-02) rather than assumed:
  `EncryptedSharedPreferences` is gone; data is AES-GCM encrypted with a key wrapped by an RSA-OAEP key in the
  Android Keystore, stored in the SharedPreferences files `FlutterSecureStorage` (plus config and wrapped-key files);
  minSdk 24 (Flutter's default). Dart-side defaults: `resetOnError` true (the Java default is false, but Dart always
  sends the flag), `migrateOnAlgorithmChange` true, `migrateWithBackup` false, no biometrics. We pass all of these
  explicitly (`SecureTokenStore.androidOptions`), so a changed default can't silently change protection. **Writes use
  `SharedPreferences.apply()`:** atomic (whole-file replace) but persisted asynchronously, so a process killed right
  after a refresh may keep the previous refresh token on disk. The next start then presents a rotated-out token, the
  backend treats it as reuse and revokes the session (014), and the app ends signed out: it fails closed, like any lost
  refresh response. Accepted; a `commit()`-based store would need a different plugin or our own platform code.
  (The plan said the package had no `setMockInitialValues`; 11.2.0 does have it. Tests use a fake
  `FlutterSecureStorage` instead, which also records the options passed.)
- **Backup:** the manifest had no `allowBackup`, so tokens would have gone into Auto Backup and device-to-device
  transfer, and come back undecryptable (Keystore keys never migrate). Now `android:allowBackup="false"` (API 24–30)
  and `android:dataExtractionRules` excluding every domain from both `cloud-backup` and `device-transfer` (API 31+,
  where `allowBackup` alone doesn't stop device transfer). The app keeps nothing else worth restoring. Should data
  still arrive from an old backup, `resetOnError` wipes what can't be decrypted and the app starts signed out.
- **Restore:** one storage read (10 s timeout), no network, so an offline start opens signed in. Empty or corrupt
  (cleared) → `signedOut`; a storage error or timeout → `signedOut` without clearing (the next launch tries again).
- **Expiry and clocks:** expiry is a hint; a 401 is authoritative. Deadlines count from when the request was *sent*.
  In process the access token is stale when **either** the monotonic clock (immune to clock changes, but it may stop
  while Android sleeps) or the wall clock (survives restarts, user-changeable) says it is within **30 s** of expiry;
  forward jumps cause an early refresh, backward ones are caught by the monotonic clock. After a restart only the
  stored wall-clock expiry exists: it is trusted only if the remaining time is positive and at most the stored
  lifetime (more means the clock went back); otherwise the token is stale and the first request refreshes. Proactive
  refresh happens only when a request is made: no timers, so app pause, resume and process death need no handling.
- **Authenticated requests (`_authorized`, behind `me()` and future typed methods), at most two sends and one
  refresh per call:** a stale token is refreshed
  first. On a 401: if the session's generation changed while the request was in flight, resend once with the current
  token without refreshing (014); otherwise refresh and resend once. A 401 for a token fresh from this call's refresh
  is rethrown and marks the token stale, so the next call refreshes and a revoked session ends there; it never loops
  and a 401 alone never signs the user out. The session is re-checked after every wait, so nothing is sent once a
  logout has begun. Other errors pass through and don't touch the session.
- **Refresh (single flight):** every token change creates a new session object with a new generation; one refresh
  runs at a time and concurrent callers share its Future (its slot is released before waiters resume), so N
  simultaneous 401s rotate the refresh token once. **Reachability probe:** every refresh is preceded by
  `GET /healthz` (no token, not rate-limited). If the probe fails (network, timeout, any non-2xx, redirect), no refresh
  is sent, the tokens and status are kept, and the caller gets the probe's error. This keeps the strict contract of
  014/018 while not signing out users who simply are offline: only a connection lost between a successful probe and
  the refresh still ends the session. Outcomes of the refresh itself:
  | Outcome | Tokens | Status | Caller gets |
  |---|---|---|---|
  | 200 valid | rotated, stored | signedIn | retry result |
  | 200, store write fails | rotated, memory only; store cleared (a restart must not present the old token: reuse) | signedIn | retry result |
  | 429 | kept; no refresh before `Retry-After` (default 5 s), then the **same** refresh token may be sent (018) | signedIn | `ApiHttpException(429, retryAfter)`; before the deadline a synthetic 429 with the remaining time and no request |
  | 401, other 4xx, any 5xx, timeout, network error, malformed or unexpected 2xx/3xx | cleared | signedOut | `SignedOutException` |

  The refresh is **never resent** after an outcome that may have committed (014: reuse revokes the session); 503 is
  treated like a network error (018). While refresh is blocked by a 429, a stale token is still tried, since the
  server may accept it.
- **Sign-in** (`signIn`, `signInWithGoogle`): only while signed out with no sign-in or logout running; the tokens are
  stored before the status becomes `signedIn` (a store failure throws `TokenStoreException` and leaves an orphaned
  server session that expires). API errors propagate unchanged; nothing is retried (after a Google 500/503 the UI needs
  a new ID token, 020).
- **Logout (015):** waits for a running restore, sign-in or refresh (so the latest access token is used and no
  rotation lands afterwards), blocks new token use, deletes the tokens from memory and storage (one retry), goes
  `signedOut`, and only then calls `POST /v1/auth/logout`; every outcome is ignored. Clearing first means process
  death mid-logout can't leave a local session. Concurrent calls share one logout; a sign-in started after the local
  phase is independent of the old logout's server call. **Residual risk:** if storage can't be cleared twice in a row
  and the server call also fails, the next launch restores a session the server didn't revoke.
- **Disposal:** after `dispose` no listener is notified, in-flight work completes or fails quietly, and public calls
  throw `StateError`. `main` never disposes it.
- **Logging:** nothing in `api/`, `auth/` or `session.dart` logs or prints. A test runs every flow with marked
  secrets (tokens, password, email, Google ID token, including in hostile error bodies) and checks every `toString`,
  exception and print, and that each secret appears only in its one place on the wire: the access token in
  `Authorization` on `/v1/me` and logout, the refresh token in the refresh body, the ID token in the google body.
- **Deferred:** auth screens, mapping error codes to messages and per-endpoint retry rules in the UI (stage 5); the
  Google sign-in plugin; wiring `/v1/me` and logout into the UI, and passing `AccountApi` to the screens (`main` builds
  it once a screen needs it); typed `SessionManager` methods for later protected routes; an app version in the
  User-Agent; verify-email and reset-password in `AccountApi` (with deep links, 021); connectivity monitoring and background refresh; biometric
  gating; certificate pinning; durable (`commit()`) storage writes; logout-all and a session list (015).
