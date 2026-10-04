# Architecture

A map of what exists and where. Rules to follow are in `CLAUDE.md` and `.claude/rules/`; the reasoning behind each
choice is in `decisions.md`.

## Overview

VocaTogether is a language-exchange app: a Go backend (`backend/`, module `vocatogether/backend`) and a Flutter
Android client (`mobile/`, package `vocatogether`, applicationId and namespace `com.vocatogether.app`).

The backend is a modular monolith: one Go service, one PostgreSQL database, stdlib where possible (`net/http` Go
1.22+ routing patterns, pgx, goose, x/crypto, x/text).

## Backend

### Startup (`cmd/api/main.go`)

`main` builds all dependencies in this order: config → email sender (`newEmailSender`) → Google verifier
(`newGoogleVerifier`, before the DB so bad config exits at once) → pool → migrations → rate limiters → services →
router. It runs the hourly retention cleanup (`authSvc.RunCleanup`) and handles graceful shutdown, after which
`authSvc.Wait()` drains background emails.

### Packages (`backend/internal/`)

| Package | Role |
|---|---|
| `server` | HTTP layer only: routing (`server.go`, `Options`), JSON decode/encode and error codes (`respond.go`), the `requireAccessToken` middleware (`authn.go`), per-IP limits and client IP (`limits.go`, `clientip.go`), security headers and the request deadline (`middleware.go`), and the HTML pages that emailed links open (`pages.go`, templates embedded from `pages/`: `/verify-email` and `/reset-password`). |
| `config` | Reads the environment into `Config` (validation per `ENV`; secrets held as `config.Secret`). |
| `auth` | The domain: `Service` (business logic, argon2 slot limiter with a queue timeout, bounded best-effort background email sending), Google sign-in (`google.go`), per-account limits (`limits.go`), retention cleanup (`cleanup.go`), `store.go` (SQL), tokens, password policy/hashing, email content (text, plus HTML from `templates/` for link emails), typed errors (`errors.go`). |
| `googleid` | Verifies Google ID tokens locally (stdlib RS256 + Google's key set, `Verifier` interface, `Fake` for tests). |
| `email` | Delivery only: `Sender` interface; `ResendSender` for production (stdlib HTTP client); `LogSender` for dev/test; `Recorder` for unit tests. |
| `ratelimit` | In-process per-key token bucket. |
| `db` | Pool + goose migrations embedded from `migrations/*.sql`, applied on every startup. |
| `testutil` | `testutil.DB(t)`: the shared test database. |

Dependency direction: `main` → `server` → `auth` → `email`, `googleid`.

Handlers read `auth.Identity` from the request context via `identityFrom` and pass `UserID` explicitly to services,
so domain packages never read the request context.

## Mobile

### Composition

`main.dart` is the composition root: config → `SessionManager` + `AccountApi` → `VocaTogetherApp` → `createRouter`,
which hands each screen only what it uses.

| File or directory | Role |
|---|---|
| `lib/config.dart` | Validates build-time config from `--dart-define-from-file`. |
| `lib/app.dart` | Owns and disposes the `GoRouter`. |
| `lib/session.dart` | `SessionManager`, the `ChangeNotifier` session (`unknown`/`signedOut`/`signedIn`) that the router listens to. |
| `lib/router.dart` | `Routes` and `authRedirect`, the only navigation policy. |
| `lib/api/` | `ApiClient` over `http.Client`. |
| `lib/auth/` | `TokenStore`/`SecureTokenStore`, `AuthApi`, `GoogleIdentity`/`PluginGoogleIdentity`. |
| `lib/screens/` | The screens and `failure_presentation.dart` (`presentFailure`). |
| `lib/ui/` | `theme.dart` (`AppTheme`, `Spacing`, `Radii`), `widgets/`, `previews/`. |
| `lib/l10n/` | `app_en.arb` and the generated localizations. |

### Networking and session

`SessionManager` → `TokenStore` + `AuthApi` → `ApiClient` → `http.Client`.

`SessionManager` holds the only in-memory tokens and owns refresh and logout. Tokens persist only in
`SecureTokenStore` (one `flutter_secure_storage` key). Screens reach the backend through `SessionManager`'s
token-free public API and through `AccountApi` (register, resend, forgot).

### Google sign-in

`SessionManager.signInWithGoogle()` asks `GoogleIdentity` for an ID token, posts it once in the body of
`/v1/auth/google` and drops it. The backend verifies it locally (`googleid`), finds or creates the account by
`(google, sub)` and returns an ordinary vt session. Logout also clears Google's credential state, best effort.

### Client status

The client is at roadmap stage 6: app shell, routing, design system, API client, secure token storage, session
management, the email/password auth screens (log in, register, forgot password, inline resend verification), a home
screen with `/v1/me` and logout, and Google sign-in on log in and register.

## Decisions by topic

| Topic | Decisions |
|---|---|
| Stack and libraries | 001, 008 |
| Session tokens, login, refresh, logout, `/v1/me` | 002, 013, 014, 015, 016 |
| Passwords and email addresses | 003, 009, 010 |
| Registration, verification, password reset, one-time tokens | 004, 011, 012, 017 |
| Account enumeration | 005 |
| Email pages and delivery | 006, 007, 019 |
| Hardening, rate limits, retention | 018 |
| Google sign-in (backend) | 020, 026 |
| Flutter client | 021 (shell, routing), 022 (design system, l10n), 023 (networking, session), 024 (screens), 025 (Google) |
