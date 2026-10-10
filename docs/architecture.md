# Architecture

A map of what exists and where. Rules to follow are in `CLAUDE.md` and `.claude/rules/`; the reasoning behind each
choice is in the decision records, indexed by topic in `decisions.md`. `README.md` explains the layout.

## Overview

VocaTogether is a language-exchange app: a Go backend (`backend/`, module `vocatogether/backend`) and a Flutter
Android client (`mobile/`, package `vocatogether`, applicationId and namespace `com.vocatogether.app`).

The backend is a modular monolith: one Go service, one PostgreSQL database, stdlib where possible (`net/http` Go
1.22+ routing patterns, pgx, goose, x/crypto, x/text, x/image).

## Backend

### Startup (`cmd/api/main.go`)

`main` builds all dependencies in this order: config → email sender (`newEmailSender`) → Google verifier
(`newGoogleVerifier`, before the DB so bad config exits at once) → pool → migrations → rate limiters → services
(`auth`, `profile`, `language`, `avatar`, `safety`) → router. It runs the hourly retention cleanup (`authSvc.RunCleanup`) and handles graceful shutdown, after which
`authSvc.Wait()` drains background emails.

### Packages (`backend/internal/`)

| Package | Role |
|---|---|
| `server` | HTTP layer only: routing (`server.go`, `Options`), JSON decode/encode and error codes (`respond.go`), the `requireAccessToken` middleware (`authn.go`), per-IP and per-user limits and client IP (`limits.go`, `clientip.go`), security headers and the request deadline (`middleware.go`), the profile routes (`profile.go`), the language routes (`languages.go`), the caller's own picture routes (`avatar.go`), the member profile and member picture routes that compose `profile`, `language`, `avatar` and `safety` (`members.go`, decisions 031 and 033), the block and report routes that compose `profile` and `safety` (`safety.go`, decision 033), and the HTML pages that emailed links open (`pages.go`, templates embedded from `pages/`: `/verify-email` and `/reset-password`). |
| `config` | Reads the environment into `Config` (validation per `ENV`; secrets held as `config.Secret`). |
| `auth` | The domain: `Service` (business logic, argon2 slot limiter with a queue timeout, bounded best-effort background email sending), Google sign-in (`google.go`), per-account limits (`limits.go`), retention cleanup (`cleanup.go`), `store.go` (SQL), tokens, password policy/hashing, email content (text, plus HTML from `templates/` for link emails), typed errors (`errors.go`). |
| `profile` | The profile domain: `Service` (`Get`, `Save` for the owner; `Public` for a profile found by its public id), the public identifier (`public_id.go`, `ParsePublicID`), normalization and validation (`validate.go`), `store.go` (SQL), typed errors. Imports neither `auth` nor `server`. |
| `language` | The languages domain (decision 029): the catalog and a member's spoken and learning languages with a level. `Service` (`Catalog`, `Get`, `Save`; `Get` also serves the member profile read), the `Level` scale (`level.go`), validation (`validate.go`), `store.go` (SQL; a save is one transaction that locks the `users` row first), typed errors. Imports none of `auth`, `server` and `profile`. |
| `avatar` | The profile picture domain (decision 031): `Service` (`Get`, `Exists`, `Save`, `Delete`; `Exists` and `Get` also serve the member reads), the checks an upload must pass before it is decoded (`inspect.go`), the EXIF orientation reader (`exif.go`), the pipeline that turns an upload into the stored 512×512 JPEG, with its decode slots and queue timeout (`normalize.go`), `store.go` (SQL), typed errors. Imports none of `auth`, `server`, `profile` and `language`. |
| `safety` | What one member does about another (decision 033): blocks and reports. `Service` (`Block`, `Unblock`, `ListBlocked`; `Blocked`, the one question the member reads and every later feature ask; `Report`), `ParseReport` with the reasons and the details rules (`report.go`), `store.go` (SQL; a block is one transaction that locks the `users` row first, a report one statement), typed errors. It names members only by internal user id: `server` resolves the public id. Imports none of `auth`, `server`, `profile`, `language` and `avatar`. |
| `googleid` | Verifies Google ID tokens locally (stdlib RS256 + Google's key set, `Verifier` interface, `Fake` for tests). |
| `email` | Delivery only: `Sender` interface; `ResendSender` for production (stdlib HTTP client); `LogSender` for dev/test; `Recorder` for unit tests. |
| `ratelimit` | In-process per-key token bucket. |
| `db` | Pool + goose migrations embedded from `migrations/*.sql`, applied on every startup. Migration 00006 also seeds the `languages` catalog (107 rows). |
| `testutil` | `testutil.DB(t)`: the shared test database. It empties the user tables and keeps the seeded `languages` catalog. |

Tables of user data: `users`, `user_tokens`, `sessions`, `user_identities`, `profiles`, `user_languages`, `avatars`,
`blocks` and `reports` (migrations 00001 to 00010).

Dependency direction: `main` → `server` → `auth`, `profile`, `language`, `avatar`, `safety`; `auth` → `email`,
`googleid`. `profile`, `language`, `avatar` and `safety` import neither `auth` nor each other.

### Routes (`server.go`)

| Route | What it is |
|---|---|
| `GET /healthz` | Liveness. |
| `POST /v1/auth/register`, `verify-email`, `resend-verification`, `forgot-password`, `reset-password`, `login`, `google`, `refresh`, `logout` | The public auth routes, each behind its per-IP limit except `logout`. |
| `GET` and `POST /verify-email`, `/reset-password` | The pages that emailed links open. |
| `GET /v1/me` | The caller's account. |
| `GET`, `PUT /v1/me/profile` | The caller's own profile. |
| `GET /v1/languages`; `GET`, `PUT /v1/me/languages` | The catalog and the caller's own languages. |
| `GET`, `PUT`, `DELETE /v1/me/avatar` | The caller's own picture: a raw image in, the stored `image/jpeg` out. |
| `PUT`, `DELETE /v1/me/blocks/{id}` | The caller blocks or unblocks the member whose profile has that public id; 204 whatever the id names. |
| `GET /v1/me/blocks` | The members the caller has blocked: public id and name, newest first. |
| `PUT /v1/me/reports/{id}` | The caller's report of that member: a reason and optional details in, 204 out. Nothing reads a report back. |
| `GET /v1/profiles/{id}` | The public profile (name, text, languages, whether there is a picture) of the member whose profile has that public id. A 404 across a block. |
| `GET /v1/profiles/{id}/avatar` | That member's picture, as `image/jpeg`. A 404 across a block. |

Everything under `/v1/me` and `/v1/profiles`, and `/v1/languages`, requires an access token. Routes under
`/v1/profiles` are read-only and for signed-in members only; every write is under `/v1/me/…` and belongs to the
session, where a block or a report names another member only as its target. Reports are read by hand, with
database access (`moderation.md`).

Handlers read `auth.Identity` from the request context via `identityFrom` and pass `UserID` explicitly to services,
so domain packages never read the request context.

## Mobile

### Composition

`main.dart` is the composition root: config → `SessionManager` + `AccountApi` + `PhotoSource` → `VocaTogetherApp`
→ `createRouter`, which hands each screen only what it uses.

| File or directory | Role |
|---|---|
| `lib/config.dart` | Validates build-time config from `--dart-define-from-file`. |
| `lib/app.dart` | Owns and disposes the `GoRouter`. |
| `lib/session.dart` | `SessionManager`, the `ChangeNotifier` session (`unknown`/`signedOut`/`signedIn`) that the router listens to. |
| `lib/router.dart` | `Routes` and `authRedirect`, the only navigation policy: the exact signed-in routes (`/home`, `/profile`, `/profile/edit`, `/profile/languages`) and the one that names a member, `/members/<public id>` (decision 032). |
| `lib/api/` | `ApiClient` over `http.Client`, the API classes (`AccountApi`, `AuthApi`), `ApiPaths`, and the models screens may see (`Me`, `Profile`, `MemberProfile`, and in `languages.dart` `Language`, `LanguageLevel`, `UserLanguage`, `UserLanguages`). `ApiClient` carries JSON, a byte body and an image answer. |
| `lib/auth/` | `AuthTokens`, `TokenStore`/`SecureTokenStore`, `AuthClock`, `GoogleIdentity`/`PluginGoogleIdentity`. |
| `lib/media/` | `PhotoSource`, the device's photo chooser behind an interface, and `PluginPhotoSource`, the only file that imports `image_picker`. |
| `lib/screens/` | The screens and `failure_presentation.dart` (`presentFailure`). The profile is four files: `profile_screen.dart` (the read-only page), `profile_edit_screen.dart` (the form), `profile_avatar_editor.dart` (the picture control) and `member_profile_screen.dart` (a member's public profile); `profile_languages_section.dart` and `profile_language_lists.dart` show languages on the page and on the member screen. |
| `lib/ui/` | `theme.dart` (`AppTheme`, `Spacing`, `Radii`), `widgets/`, `previews/`. |
| `lib/l10n/` | `app_en.arb` and the generated localizations. |

### Networking and session

`SessionManager` → `TokenStore` + `AuthApi` → `ApiClient` → `http.Client`.

`SessionManager` holds the only in-memory tokens and owns refresh and logout. Tokens persist only in
`SecureTokenStore` (one `flutter_secure_storage` key). Screens reach the backend through `SessionManager`'s
token-free public API (`me()`, `profile()`, `saveProfile()`, `languageCatalog()`, `languages()`, `saveLanguages()`,
`avatar()`, `saveAvatar()`, `removeAvatar()`, `memberProfile()`, `memberAvatar()`, sign-in, logout) and through
`AccountApi` (register, resend, forgot). Of the three language methods, the profile page's languages section calls
`languageCatalog()` and `languages()`, and the languages editor calls all three: it is the only caller of
`saveLanguages()`. Pictures cross this boundary as bytes; no widget fetches an image itself.

### The profile screens (decision 032)

| Route | Screen | Calls |
|---|---|---|
| `/profile` | `ProfileScreen`: the caller's read-only page (picture, name, text, a Friends placeholder, languages), "Edit Profile" and "See public profile" | `profile()`, then `avatar()` and, in its languages section, `languageCatalog()` and `languages()` |
| `/profile/edit` | `ProfileEditScreen`: the name and text form, the picture control (`ProfileAvatarEditor`, applied at once through `PhotoSource`), the row that opens the languages editor | `profile()`, `saveProfile()`, `avatar()`, `saveAvatar()`, `removeAvatar()` |
| `/profile/languages` | `LanguagesScreen`, the languages editor, opened from the edit screen | `languageCatalog()`, `languages()`, `saveLanguages()` |
| `/members/<id>` | `MemberProfileScreen`: a member's public profile, read-only, reached for now only from "See public profile" with the caller's own id | `memberProfile()`, `languageCatalog()`, `memberAvatar()` |

Every screen is pushed, so back returns to the opener. The page loads again each time the member comes back from
the edit screen.

### Google sign-in

`SessionManager.signInWithGoogle()` asks `GoogleIdentity` for an ID token, posts it once in the body of
`/v1/auth/google` and drops it. The backend verifies it locally (`googleid`), finds or creates the account by
`(google, sub)` and returns an ordinary vt session. Logout also clears Google's credential state, best effort.

### Client status

The client is at roadmap stage 7: app shell, routing, design system, API client, secure token storage, session
management, the email/password auth screens (log in, register, forgot password, inline resend verification), a home
screen with `/v1/me` and logout, Google sign-in on log in and register, and the user's own profile (create and
edit, from home). The social profile of decisions 031 and 032 is built on top of it: the read-only profile page,
the edit screen, the profile picture and the member profile screen; its checks on the emulator are pending.

Stage 8 (languages) is done on the backend: the three routes of decision 029 (`GET /v1/languages`, `GET` and
`PUT /v1/me/languages`) are served and tested. On the client it is built:

| Part | State |
|---|---|
| Data, API and session layer: the models in `lib/api/languages.dart`, the two `ApiPaths`, the three `AuthApi` calls, `SessionManager.languageCatalog()`, `languages()` and `saveLanguages()`, the language cases of `presentFailure` and their strings | implemented and tested |
| Widgets and previews (`LanguageChip`, `LanguageRow` in `lib/ui/widgets/`), the level labels and descriptions (`lib/screens/language_labels.dart`) and the Languages summary on the profile page (`lib/screens/profile_languages_section.dart`, `profile_language_lists.dart`) | implemented and tested |
| The editor (`lib/screens/languages_screen.dart`) at `/profile/languages`, its picker and level choice (`lib/screens/language_picker_sheet.dart`), and the "Languages" row of the profile edit screen that opens it (032) | implemented and tested |
| Verification on the emulator, run by hand by the author | done |
| The final pass over these documents, and decision 030 to *in force* | pending |

So a member sees their languages on their profile page and edits them on their own screen, opened from the profile
edit screen: add from the catalog, choose a level, order, remove, save. Decision 030 (a draft until the last row is done) records the client-side decisions and
what is built; the rules are in `.claude/rules/languages.md`.
