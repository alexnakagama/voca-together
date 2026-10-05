# Decisions

The index of the project's decision records. Each record is one file, `docs/decisions/NNN-topic.md`. Open the
ones your task needs; nobody has to read them all.

A reference to "decision 017", "(017)" or "`docs/decisions.md` 017", in code, tests, rules or another record, means
the file `docs/decisions/017-*.md`.

## What a record is, and what it is not

- **A record says why**: the reasoning, the alternatives turned down, the race and lock analyses, the accepted
  risks, what was deferred and what was tested. It was written when the work was done.
- **A record is history and is not rewritten.** When a later decision changes an earlier one, the later one says
  so, and the earlier file gets a note in its header (the quoted block under the title) naming what changed. The
  text under the header stays as it was written. **Read the header first.**
- **What holds now** is in `CLAUDE.md` and `.claude/rules/`. They are short, they are kept current, and they
  load by themselves with the files they concern. Start there; come here for the why, or before changing a rule.
- If two records disagree, the later one wins. If a record and the code disagree, the code and its tests are
  right, and the documentation is what gets fixed.

`docs/README.md` says where each kind of information belongs and how to add a record.

## What to read for a task

| Working on | Rules that load with the files | Records, most useful first |
|---|---|---|
| Any backend change | `backend.md` | 016 (protected routes), 018 (limits, 503s and retries), 011 (error format) |
| Sessions, login, refresh, logout | `auth.md` | 013, 014, 015, 016, 002 |
| Registration, verification, password reset | `auth.md` | 011, 012, 017, 004, 005 |
| Passwords and email addresses | `auth.md` | 003, 009, 010 |
| Rate limits, client IP, cleanup, headers | `auth.md`, `backend.md` | 018 |
| Email content, pages and delivery | `auth.md`, `config.md` | 019, 007, 006 |
| Google sign-in, backend | `google-sign-in.md` | 026, then 020 (Stage 7 first) |
| Google sign-in, client | `google-sign-in.md` | 025, 026 |
| Profile | `profile.md` | 027 (backend), 028 (client) |
| Languages | `languages.md` | 029 (backend), 030 (client, draft) |
| A new member-owned resource | `backend.md` | 027 and 029 as the two worked examples |
| Client structure and routing | `mobile.md` | 021, 028 |
| Client networking, tokens, session | `mobile.md`, `auth.md` | 023, then 014 and 015 for the server side |
| Client screens and failure messages | `mobile.md` | 024, 022 |
| Client design system, widgets, strings | `mobile.md` | 022 |
| Configuration and startup | `config.md` | 019 (email), 020 Stage 6 (Google), 018 (proxy hops) |
| Dependencies | none | 008 |

## All records

Status is one of: **in force**; **amended** (in force, with parts changed by the records named); **partly
superseded** (a part no longer holds); **draft** (approved, not implemented).

| # | Record | Side | Status |
|---|---|---|---|
| [001](decisions/001-modular-monolith.md) | Modular monolith: Go, PostgreSQL, REST | backend | in force |
| [002](decisions/002-opaque-session-tokens.md) | Opaque, DB-backed session tokens instead of JWT | backend | in force |
| [003](decisions/003-argon2id-passwords.md) | argon2id and the password policy | backend | in force |
| [004](decisions/004-one-time-tokens.md) | One-time tokens for verification and reset | backend | in force |
| [005](decisions/005-no-account-enumeration.md) | No account enumeration | both | in force; one exception in 020 |
| [006](decisions/006-email-link-pages.md) | Emailed links open backend-served pages | backend | in force |
| [007](decisions/007-email-behind-an-interface.md) | Email behind an interface | backend | in force |
| [008](decisions/008-minimal-libraries.md) | Minimal libraries | both | amended: its lists are out of date |
| [009](decisions/009-passwords-nfkc.md) | Passwords are NFKC-normalized | backend | in force |
| [010](decisions/010-ascii-email-addresses.md) | Email addresses are printable ASCII | backend | in force |
| [011](decisions/011-registration.md) | Registration; the API's error format | backend | in force |
| [012](decisions/012-email-verification.md) | Email verification and resend; the user-row lock order | backend | in force |
| [013](decisions/013-login.md) | Login and session issuance | backend | in force |
| [014](decisions/014-refresh-rotation.md) | Refresh-token rotation and reuse detection | backend | in force |
| [015](decisions/015-logout.md) | Logout | backend | in force |
| [016](decisions/016-access-token-auth-and-me.md) | Access-token authentication and `GET /v1/me` | backend | in force |
| [017](decisions/017-password-reset.md) | Password reset | backend | in force |
| [018](decisions/018-hardening-and-rate-limits.md) | Rate limits, deadlines, retries, cleanup, headers | backend | amended by 027, 029 |
| [019](decisions/019-resend-email-delivery.md) | Production email with Resend; the verify-email page | backend | in force |
| [020](decisions/020-google-sign-in-backend.md) | Google sign-in and passwordless accounts | backend | partly superseded by 026 |
| [021](decisions/021-client-shell-and-routing.md) | App shell: session state and routing | client | amended by 023, 028 |
| [022](decisions/022-client-design-system.md) | Design system, auth widgets, localization | client | in force |
| [023](decisions/023-client-networking-and-session.md) | Networking, token storage, session management | client | amended by 024, 025, 026, 028 |
| [024](decisions/024-client-auth-screens.md) | Authentication screens and the failure mapping | client | in force |
| [025](decisions/025-client-google-sign-in.md) | Google sign-in in the client | client | in force |
| [026](decisions/026-google-id-token-reuse.md) | Google ID tokens are accepted while valid, not once | both | in force; supersedes part of 020 |
| [027](decisions/027-profile-backend.md) | User profile | backend | in force |
| [028](decisions/028-client-profile-screen.md) | Profile screen | client | in force |
| [029](decisions/029-languages-backend.md) | Languages: catalog and a member's own languages | backend | in force |
| [030](decisions/030-client-languages.md) | Languages in the profile | client | draft, not implemented |

## What later records changed

Each of these is also noted in the header of the earlier file.

| Earlier | Changed by | What changed |
|---|---|---|
| 020 | 026 | A Google ID token is accepted every time it verifies, not once. The `google_id_token_uses` table and everything about "spent" tokens are gone. |
| 023 | 025 | `signInWithGoogle()` takes no argument; the Google ID token never leaves the session layer. |
| 023 | 028 | `ApiClient` sends PUT as well as GET and POST; `SessionManager` gained `profile()` and `saveProfile()`. |
| 021 | 023 | `Session` and `markSignedIn`/`markSignedOut` were replaced by `SessionManager`. |
| 021 | 028 | A signed-in user may be on any route of `Routes.signedInRoutes`, not only `/home`. |
| 018 | 027, 029 | Protected writes have per-user limits; 018 had left protected routes unlimited. |
| 015 | 023 | The client clears its tokens first and calls the server afterwards. |
| 008 | 018, 009, 021, 022, 023, 025 | The library lists; `go.mod` and `pubspec.yaml` are current. |
| 005 | 020 | Google sign-in's 409 `account_exists` is a deliberate exception to "no enumeration". |

## Deferred work, and where it is discussed

Pointers only; the records hold the conditions and the reasons. Check the code before relying on a line here.

| Deferred | Records |
|---|---|
| Durable email outbox, retries, bounce handling | 018, 019 |
| Shared or edge rate limiting before running several instances | 018 |
| Logout-all, a session list, a cap on sessions per user, rotation lineage | 014, 015, 018 |
| Password change for a signed-in user | 017, 018 |
| Metrics, request IDs, IP logging | 013, 018 |
| Unicode email domains | 010 |
| Account linking, `azp` and a per-sign-in nonce, RISC, recovery for a lost Google account | 020, 026 |
| Release signing, a published consent screen, a release-build smoke test | 025 |
| Deep links; verify-email and reset-password inside the app | 021, 023, 024 |
| More locales, golden tests, a Google Sans subset | 022, 024 |
| Profile: deletion and export, a handle, moderation and blocking, a profile gate | 027, 028 |
| Languages: regional variants, per-locale names, catalog administration, a catalog cache | 029 |
| Limiting protected reads; optimistic locking for member-owned resources | 027, 029 |
