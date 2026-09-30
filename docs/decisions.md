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
`email.Sender` with a dev `LogSender` and a test `Recorder`. The real provider will be chosen later.
- `email` handles delivery only. `auth` owns the content, links and expiry wording. The dependency runs
  `auth → email`, never the reverse.
- Production must refuse to start with `LogSender`: it logs message bodies, which contain live tokens.
  There is no silent fallback.
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
- Production refuses to start while only `LogSender` exists (see 007).
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
  register); the HTML pages behind emailed links (006), so those links don't work yet; cleanup of used and expired
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
