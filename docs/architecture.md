# Architecture

A map of what exists and where. Rules to follow are in `CLAUDE.md` and `.claude/rules/`; the reasoning behind each
choice is in the decision records, indexed by topic in `decisions.md`. `README.md` explains the layout.

## Overview

VocaTogether is a language-exchange app: a Go backend (`backend/`, module `vocatogether/backend`) and a Flutter
Android client (`mobile/`, package `vocatogether`, applicationId and namespace `com.vocatogether.app`).

The backend is a modular monolith: one Go service, one PostgreSQL database, stdlib where possible (`net/http` Go
1.22+ routing patterns, pgx, goose, x/crypto, x/text).

## Backend

### Startup (`cmd/api/main.go`)

`main` builds all dependencies in this order: config → email sender (`newEmailSender`) → Google verifier
(`newGoogleVerifier`, before the DB so bad config exits at once) → pool → migrations → rate limiters → services
(`auth`, `profile`, `language`) → router. It runs the hourly retention cleanup (`authSvc.RunCleanup`) and handles graceful shutdown, after which
`authSvc.Wait()` drains background emails.

### Packages (`backend/internal/`)

| Package | Role |
|---|---|
| `server` | HTTP layer only: routing (`server.go`, `Options`), JSON decode/encode and error codes (`respond.go`), the `requireAccessToken` middleware (`authn.go`), per-IP and per-user limits and client IP (`limits.go`, `clientip.go`), security headers and the request deadline (`middleware.go`), the profile routes (`profile.go`), the language routes (`languages.go`), and the HTML pages that emailed links open (`pages.go`, templates embedded from `pages/`: `/verify-email` and `/reset-password`). |
| `config` | Reads the environment into `Config` (validation per `ENV`; secrets held as `config.Secret`). |
| `auth` | The domain: `Service` (business logic, argon2 slot limiter with a queue timeout, bounded best-effort background email sending), Google sign-in (`google.go`), per-account limits (`limits.go`), retention cleanup (`cleanup.go`), `store.go` (SQL), tokens, password policy/hashing, email content (text, plus HTML from `templates/` for link emails), typed errors (`errors.go`). |
| `profile` | The profile domain: `Service` (`Get`, `Save`), normalization and validation (`validate.go`), `store.go` (SQL), typed errors. Imports neither `auth` nor `server`. |
| `language` | The languages domain (decision 029): the catalog and a member's own spoken and learning languages with a level. `Service` (`Catalog`, `Get`, `Save`), the `Level` scale (`level.go`), validation (`validate.go`), `store.go` (SQL; a save is one transaction that locks the `users` row first), typed errors. Imports none of `auth`, `server` and `profile`. |
| `googleid` | Verifies Google ID tokens locally (stdlib RS256 + Google's key set, `Verifier` interface, `Fake` for tests). |
| `email` | Delivery only: `Sender` interface; `ResendSender` for production (stdlib HTTP client); `LogSender` for dev/test; `Recorder` for unit tests. |
| `ratelimit` | In-process per-key token bucket. |
| `db` | Pool + goose migrations embedded from `migrations/*.sql`, applied on every startup. Migration 00006 also seeds the `languages` catalog (107 rows). |
| `testutil` | `testutil.DB(t)`: the shared test database. It empties the user tables and keeps the seeded `languages` catalog. |

Dependency direction: `main` → `server` → `auth`, `profile`, `language`; `auth` → `email`, `googleid`. `profile` and
`language` import neither `auth` nor each other.

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
| `lib/api/` | `ApiClient` over `http.Client`, the API classes (`AccountApi`, `AuthApi`), `ApiPaths`, and the models screens may see (`Me`, `Profile`, and in `languages.dart` `Language`, `LanguageLevel`, `UserLanguage`, `UserLanguages`). |
| `lib/auth/` | `AuthTokens`, `TokenStore`/`SecureTokenStore`, `AuthClock`, `GoogleIdentity`/`PluginGoogleIdentity`. |
| `lib/screens/` | The screens and `failure_presentation.dart` (`presentFailure`). |
| `lib/ui/` | `theme.dart` (`AppTheme`, `Spacing`, `Radii`), `widgets/`, `previews/`. |
| `lib/l10n/` | `app_en.arb` and the generated localizations. |

### Networking and session

`SessionManager` → `TokenStore` + `AuthApi` → `ApiClient` → `http.Client`.

`SessionManager` holds the only in-memory tokens and owns refresh and logout. Tokens persist only in
`SecureTokenStore` (one `flutter_secure_storage` key). Screens reach the backend through `SessionManager`'s
token-free public API (`me()`, `profile()`, `saveProfile()`, `languageCatalog()`, `languages()`, `saveLanguages()`,
sign-in, logout) and through `AccountApi` (register, resend, forgot). Of the three language methods, the Profile
screen's languages section calls `languageCatalog()` and `languages()`; nothing calls `saveLanguages()` yet.

### Google sign-in

`SessionManager.signInWithGoogle()` asks `GoogleIdentity` for an ID token, posts it once in the body of
`/v1/auth/google` and drops it. The backend verifies it locally (`googleid`), finds or creates the account by
`(google, sub)` and returns an ordinary vt session. Logout also clears Google's credential state, best effort.

### Client status

The client is at roadmap stage 7: app shell, routing, design system, API client, secure token storage, session
management, the email/password auth screens (log in, register, forgot password, inline resend verification), a home
screen with `/v1/me` and logout, Google sign-in on log in and register, and the user's own profile (create and
edit, from home).

Stage 8 (languages) is done on the backend: the three routes of decision 029 (`GET /v1/languages`, `GET` and
`PUT /v1/me/languages`) are served and tested. On the client it is partly done:

| Part | State |
|---|---|
| Data, API and session layer: the models in `lib/api/languages.dart`, the two `ApiPaths`, the three `AuthApi` calls, `SessionManager.languageCatalog()`, `languages()` and `saveLanguages()`, the language cases of `presentFailure` and their strings | implemented and tested |
| Widgets and previews (`LanguageChip`, `LanguageRow` in `lib/ui/widgets/`), the level labels (`lib/screens/language_labels.dart`) and the read-only Languages summary on Profile (`lib/screens/profile_languages_section.dart`) | implemented and tested |
| The editor screen and its route `/profile/languages`, with the button on Profile that opens it | not implemented |
| Completing decision 030, the final pass over these documents, and verification on the emulator | pending |

So a member sees their languages on Profile but can't edit them in the app yet: no screen saves, and `LanguageRow`
is built and tested but used by no screen until the editor exists. Decision 030
(still a draft) records the client-side decisions and what is built; the rules for both the built layer and the
screens to come are in `.claude/rules/languages.md`.
