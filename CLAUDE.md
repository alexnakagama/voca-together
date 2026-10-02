# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

VocaTogether: a language-exchange app. The repo holds the Go backend (`backend/`, module `vocatogether/backend`) and
the Flutter Android client (`mobile/`, package `vocatogether`, applicationId and namespace `com.vocatogether.app`).
The client is at roadmap stage 4 (app shell, routing, design system, reusable auth widgets, API client, secure token
storage and session management; no real auth screens yet); `docs/decisions.md`
specifies its contracts with the backend.

## Commands

Run from the repo root (the Makefile `cd`s into `backend/`):

```sh
make db-up    # start local PostgreSQL 17 (docker compose); also creates the voca_test database
make test     # all tests against voca_test, with -p 1
make vet      # go vet ./...
make run      # run the API on :8080 (migrations apply on startup)
make db-down
```

Single test (from `backend/`):

```sh
TEST_DATABASE_URL=postgres://voca:voca@localhost:5432/voca_test?sslmode=disable \
  go test -p 1 ./internal/auth -run TestLogin
```

- DB-backed tests use `testutil.DB(t)`, which migrates and TRUNCATEs a single shared database. They **skip**
  silently when `TEST_DATABASE_URL` is unset, so a "passing" run without it proves little. Never use `t.Parallel`
  in them and always keep `-p 1`.
- `testutil.DB` truncates `users, user_tokens, sessions, user_identities, google_id_token_uses`; add new tables there
  when adding migrations.

Mobile (Flutter, Android only):

```sh
make mobile-analyze   # flutter analyze (FLUTTER=/path/to/flutter if it isn't on PATH)
make mobile-test      # flutter test
# from mobile/:
flutter run --dart-define-from-file=config/dev.json   # emulator → backend from `make run` at http://10.0.2.2:8080
flutter build apk --debug --dart-define-from-file=config/dev.json
```

- Build-time config comes only from `--dart-define-from-file` (`mobile/config/<env>.json`); `lib/config.dart`
  validates it at startup and throws if it's missing or invalid. Every value is compiled into the APK and is
  therefore public: never put a secret there. `API_BASE_URL` is an http(s) origin with no path, query, fragment or
  credentials. Release builds require https.
- Dart's own sockets (`dart:io` `HttpClient`, `package:http`'s default client on Android) ignore Android's Network
  Security Configuration: Flutter doesn't pass it to the Dart VM. The https requirement on `API_BASE_URL` in
  release builds (`lib/config.dart`) is the only cleartext control for Dart traffic, so every API request must be
  built from that base URL. The debug-only `android/app/src/debug/res/xml/network_security_config.xml` (cleartext
  only to `10.0.2.2`) governs only platform stacks: WebView, and `cronet_http`/`ok_http` if adopted.
  `INTERNET` is declared in the main manifest because Flutter's template grants it only in debug and profile builds.
- Structure (decision 021): `main.dart` is the composition root (config → `SessionManager` → `VocaTogetherApp`) and the
  only place long-lived objects are built; pass them down by constructor (no provider/riverpod/bloc/get_it, no
  top-level mutable state). `app.dart` owns and disposes the `GoRouter`; `session.dart` is `SessionManager`, the
  `ChangeNotifier` session (`unknown`/`signedOut`/`signedIn`) that the router listens to; `router.dart` holds `Routes`
  and `authRedirect`, the only navigation policy. Screens (`lib/screens/`) never read or change session state or
  decide access. Never put a token or email in a route. Android deep links are disabled in the manifest until designed.
- Networking and session (decision 023, read it before touching auth code): `SessionManager` → `TokenStore`
  (`lib/auth/`) + `AuthApi` → `ApiClient` (`lib/api/`) → `http.Client`. `ApiClient` holds no auth state, never retries,
  never follows redirects and never logs. `SessionManager` holds the only in-memory tokens and owns refresh (single
  flight, generation check, `/healthz` probe first, the 014/018 failure matrix in 023) and logout. **Token boundary:**
  screens may use only `AccountApi` (register/resend/forgot, token-free) and `SessionManager`'s public API, which
  takes and returns no app token (`signIn`/`signInWithGoogle`/`logout` → `void`, `me()` → `Me`). `AuthApi` (returns
  `AuthTokens`, takes raw tokens) is built only in `main` and held only by `SessionManager`; the generic request
  wrapper `_authorized` stays private, and each new protected route gets a typed `SessionManager` method.
  `test/architecture_test.dart` enforces the import allowlist and fails if `lib/screens/` or `lib/ui/` names a token
  type or value. `SecureTokenStore` (one `flutter_secure_storage` key, explicit `AndroidOptions`) is the only place
  tokens persist; app backup and device transfer are disabled in the manifest. Nothing in `api/`, `auth/` or
  `session.dart` may log; `test/leak_test.dart` checks redaction and where each secret travels. Session tests are
  host-only (`test/support/fakes.dart`: `FakeServer` on `MockClient`, `InMemoryTokenStore`, `FakeAuthClock`).
- UI (decision 022): `lib/ui/theme.dart` holds `AppTheme` (M3 light/dark) and the `Spacing`/`Radii` constants;
  `lib/ui/widgets/` holds the reusable widgets, which never import `Session`, the router or `AppConfig` and hardcode
  no user-visible string. `GoogleSignInButton` follows Google's branding guidelines (its colors and the official logo
  in `assets/google/` must not be changed). Previews in `lib/ui/previews/` use `@VocaPreview` and stay pure UI;
  add each new preview function to `test/ui/previews_test.dart`.
- Strings: add them to `lib/l10n/app_en.arb` with an `@` description; `flutter pub get` (also run by `flutter
  test/run/build`) regenerates the committed `lib/l10n/app_localizations*.dart`. Never edit the generated files.

Config (env): `DATABASE_URL` (required, secret, never log it), `ENV` (`development`|`test`|`production`),
`HTTP_ADDR` (default `:8080`), `APP_BASE_URL` (used in emailed links; https and a public host required in production),
`TRUSTED_PROXY_HOPS` (reverse proxies appending to `X-Forwarded-For`; default 0 = TCP peer; must be set explicitly in
production; only valid if the server is reachable solely through those proxies, otherwise clients can spoof their IP),
`RESEND_API_KEY` (secret, held as `config.Secret`, never log it) and `EMAIL_FROM` (canonical `local@domain` or
`Name <local@domain>` on a Resend-verified domain). Email sender by `ENV` (decision 019): production always uses Resend
and refuses to start without both variables; development uses `LogSender` unless `RESEND_API_KEY` is set (then
`EMAIL_FROM` is required too); test always uses `LogSender` and ignores both.
`GOOGLE_CLIENT_ID` (public, the Web OAuth client ID that Google ID tokens must name as `aud`; never an Android client
ID; decision 020 stage 6): required in production (startup fails without it), optional in development (unset
disables Google sign-in), ignored in test. No Google client secret, API key, service account or Firebase exists in this
backend.

## Architecture

Modular monolith: one Go service, one PostgreSQL database, stdlib only where possible (`net/http` Go 1.22+
routing patterns, pgx, goose, x/crypto, x/text). Keep dependencies minimal.

- `cmd/api/main.go` builds all dependencies (config → email sender (`newEmailSender`) → Google verifier
  (`newGoogleVerifier`, before the DB so bad config exits at once) → pool → migrations → rate limiters → services →
  router), runs the hourly retention cleanup (`authSvc.RunCleanup`), and handles graceful
  shutdown, then `authSvc.Wait()` drains background emails.
- `internal/server` is the HTTP layer only: routing (`server.go`, `Options`), JSON decode/encode and error codes
  (`respond.go`), the `requireAccessToken` middleware (`authn.go`), per-IP limits and client IP (`limits.go`,
  `clientip.go`), security headers and the request deadline (`middleware.go`), and the HTML pages that emailed links open
  (`pages.go`, templates embedded from `pages/`: `/verify-email` and `/reset-password`; GET never uses a token or the DB,
  POST calls the same service). Protected routes are wrapped **individually** in `server.New`; handlers read
  `auth.Identity` from context via `identityFrom` and pass `UserID` explicitly to services, so domain packages never
  read the request context.
- `internal/config` reads the environment into `Config` (validation per `ENV`; secrets held as `config.Secret`).
  Only `main` uses it; `auth` and `server` read no configuration.
- `internal/auth` owns the domain: `Service` (business logic, argon2 slot limiter with a queue timeout, bounded
  best-effort background email sending), Google sign-in (`google.go`), per-account limits (`limits.go`), retention
  cleanup (`cleanup.go`), `store.go` (SQL), tokens, password policy/hashing, email content (text, plus HTML from
  `templates/` for link emails). Errors are typed (`errors.go`) and mapped to HTTP in `server`.
- `internal/googleid` verifies Google ID tokens locally (stdlib RS256 + Google's key set, `Verifier` interface,
  `Fake` for tests) and knows nothing about users, the DB or HTTP routes. Dependency direction is `auth → googleid`;
  `server` never imports it, and only `main` constructs the verifier (`nil` = disabled, outside production only).
- `internal/email` handles delivery only (`Sender` interface; `ResendSender` for production, stdlib HTTP client, no
  retries, sanitized errors; `LogSender` for dev/test; `Recorder` for unit tests). Dependency direction is
  `auth → email`, never the reverse. Resend click/open tracking must stay disabled (links carry tokens).
- `internal/ratelimit`: in-process per-key token bucket (limits are per process; see decision 018 before scaling out).
- `internal/db`: pool + goose migrations embedded from `internal/db/migrations/*.sql` and applied on every startup.
  Add new numbered files; never edit applied ones.

### API conventions

- Errors are `{"error":{"code":"…"}}`; `validation_failed` (422) adds `fields:[{field,code}]` with all failing
  fields. Malformed JSON / unknown fields / trailing data / oversized body → 400 `invalid_request`. Anything else →
  opaque 500 `internal_error`, details only in logs.
- Auth endpoints and protected routes send `Cache-Control: no-store` on every response, errors included.
- Rate limits → 429 `rate_limited` with `Retry-After` (nothing was done; retry is safe). Argon2 queue timeout, Google
  keys unavailable, or the 10 s request deadline → 503 `service_unavailable` with `Retry-After` (work may have
  committed; see 018 for which endpoints are safely retriable, 020 for Google).
- New public routes that do real work get a per-IP limit in `server.New`; per-account checks run after validation and
  before any DB/argon2 work, keyed by the normalized address whether or not the account exists. Google sign-in is the
  exception: it shares `ip_login`, and its `account_login` key is `google:` + the verified `sub` (known only after
  verification, still before the DB).

## Decisions log — read before changing auth

`docs/decisions.md` is the source of truth for security-relevant behavior and records the reasoning, lock orders,
race analyses, and deferred work for each step (numbered, newest at the bottom). When a change makes or alters a
design decision, append a new entry there in the same style. Key invariants:

- Opaque DB-backed tokens, not JWT: access `vt_at_…` (15 min), refresh `vt_rt_…` (30 d sliding, 90 d absolute).
  Only SHA-256 hashes are stored. Refresh rotates every use; reuse of the previous refresh token revokes the session.
- No account enumeration: register/resend/forgot always 202; login gives the same 401 for unknown email and wrong
  password, with equal work (dummy-hash verification). Emails are sent in the background with a context derived via
  `context.WithoutCancel`, never the request context.
- Uniform errors for all unusable tokens (one code per token kind); malformed tokens are rejected before any DB access.
- Passwords: argon2id (PHC format, rehash on login), NFKC-normalized before hashing/verifying/policy checks — never
  change the normalization form without a migration path. Emails: trimmed, lowercased, printable ASCII only.
- One-time tokens (`user_tokens`) are consumed with a single conditional `UPDATE … RETURNING`. Lock order:
  transactions that touch an existing user's `user_tokens` lock the `users` row `FOR UPDATE` first; session-only
  operations lock a single session row.
- 202 means accepted, not delivered: email is best effort (bounded in-flight sends, excess dropped, no retries) until an
  outbox exists.
- Never log emails, passwords, tokens, token hashes, user agents, IPs, or request bodies. Google ID tokens, `sub`,
  and the client ID count too. `auth.Token`, `googleid.Claims` and `config.Secret` redact themselves in fmt, slog and
  JSON.

### Google sign-in (`POST /v1/auth/google`; decision 020, stage 7 summarizes the contract)

- Google establishes identity only. The Google ID token travels only in that request's body and is never an API
  credential; the response is an ordinary vt session, exactly as login returns it. Only `vt_at_`/`vt_rt_` tokens
  authenticate anything else.
- Verification is local and its security boundary is the **exact** `aud` match against `GOOGLE_CLIENT_ID` (the Web
  client ID). `azp` and nonce are deliberately unchecked. That rests on our deployment assumptions (020 Stage 6
  preconditions), not on a Google guarantee; see 020 Stage 2 before changing the Google Cloud project or adding a
  web client.
- Each verified ID token is accepted at most once: the first write of the sign-in transaction inserts its hash into
  `google_id_token_uses`, and once that commits the token is spent (200, 403 or 409). After 500/503 or no response
  clients sign in with a **new** ID token; replaying the old one is 401 `invalid_google_token`.
- Accounts are found by `(google, sub)`; the token's email is never synced. A new identity creates a passwordless
  account only if Google says its email is verified (no Gmail/Workspace-only restriction; don't add one). An address
  that already has an account gets 409 `account_exists`: nothing is linked automatically, and that account may have
  no password. Its 409 is the one deliberate enumeration exception (020).
- Passwordless accounts (`password_hash` NULL) get no reset token (forgot sends a no-link notice) and the uniform 401
  on password login. A deferred trigger requires every user to keep a password or an identity; flows that remove one
  must lock the user row `FOR UPDATE`.
