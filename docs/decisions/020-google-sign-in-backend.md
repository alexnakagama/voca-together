# 020: Google sign-in and passwordless accounts (M1 step 13)

> **Status:** in force, except the single-use rule for ID tokens, which 026 superseded.
>
> **How to read it:** the record follows the seven stages it was built in. **Stage 7**, at the end, is the
> contract in summary; read it with 026. Stage 2 (the verifier, why `azp` and nonce are unchecked) and
> Stage 6 (configuration, deployment preconditions) hold reasoning that is still in force.
>
> **Superseded by 026**, and no longer true: in Stage 2, *What replaces it* under the nonce (nothing
> replaces it now; 026 states the accepted risk); step 1 of Stage 3 and the *Cleanup* bullet after it (the
> `google_id_token_uses` table is gone); in Stage 4 the `replayed` outcome and the check for claims without
> `AcceptedUntil`; "503 or 500 after commit" in Stage 5; and in Stage 7 the second rule for the ID
> token, the "Token spent?" column and *Retries*.
>
> **Client side:** 025.
>
> **Current rules:** `.claude/rules/google-sign-in.md`.

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
  1. *(Superseded by 026: tokens are no longer single-use and this step is gone.)* **Single use of the ID token:** the
     first statement is
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
  - **503 or 500 after commit** *(026: the token isn't spent; a retry with the same token works too)*: the deadline
    can fire after `googleSignIn` committed (018). The ID token is then
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
  - **Two rules for the ID token** *(the second is superseded by 026: a token is accepted whenever it verifies)*:
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
  - **Retries** *(replaced by the client retry rule in 026)*:
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
