# 023: Networking, token storage and session management (client stage 4)

> **Status:** in force, amended in four places.
>
> **Changed later:** 024: `main` also builds `AccountApi` and passes it down. 025: `signInWithGoogle()`
> takes no argument and the Google ID token never leaves the session layer (the text below notes it). 026:
> after a failed Google sign-in no new ID token is needed. 028: `ApiClient` also sends PUT, and
> `SessionManager` gained `profile()` and `saveProfile()`. 030: `SessionManager` gained `languageCatalog()`,
> `languages()` and `saveLanguages()`.
>
> **Current rules:** `.claude/rules/mobile.md` (token boundary), `.claude/rules/auth.md` (session internals).

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
    it to `signInWithGoogle` (020). (Superseded by 025: `signInWithGoogle()` takes no argument and screens never
    hold an ID token.)
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
