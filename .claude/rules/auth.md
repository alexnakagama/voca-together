---
paths:
  - "backend/internal/auth/**"
  - "backend/internal/email/**"
  - "backend/internal/server/auth.go"
  - "backend/internal/server/authn.go"
  - "backend/internal/server/me.go"
  - "backend/internal/server/pages.go"
  - "backend/internal/server/pages/**"
  - "backend/internal/server/limits.go"
  - "backend/internal/server/clientip.go"
  - "backend/internal/db/migrations/00001_auth.sql"
  - "backend/internal/db/migrations/00002_sessions_checks.sql"
  - "mobile/lib/session.dart"
  - "mobile/lib/auth/**"
  - "mobile/lib/api/auth_api.dart"
  - "mobile/lib/api/account_api.dart"
---

# Auth rules (both sides)

These are the invariants. The reasoning, races and accepted risks are in the decision records
(`docs/decisions.md` lists them by topic); read the cited record before changing one of these. The never-log list
and the secrets rule are in `CLAUDE.md`. Google sign-in has its own file, `google-sign-in.md`: read it before
touching `/v1/auth/google`, `googleSignIn` or `signInWithGoogle`.

## Tokens and sessions (002, 013–016)

- Opaque DB-backed tokens, not JWT: access `vt_at_…` (15 min), refresh `vt_rt_…` (30 d sliding, 90 d absolute).
  Only SHA-256 hashes are stored. Refresh rotates every use; reuse of the previous refresh token revokes the session.
- Uniform errors for all unusable tokens (one code per token kind); malformed tokens are rejected before any DB
  access.
- `auth.Token`, `googleid.Claims` and `config.Secret` redact themselves in fmt, slog and JSON.

## Accounts (003–005, 009–012, 017, 020)

- Google sign-in (`handleGoogleSignIn` in `server/auth.go`, `googleSignIn` in `auth/store.go`, `signInWithGoogle` in
  `session.dart`): read `google-sign-in.md` first; it does not load with these files.

- No account enumeration: register/resend/forgot always 202; login gives the same 401 for unknown email and wrong
  password, with equal work (dummy-hash verification).
- Passwords: argon2id (PHC format, rehash on login), NFKC-normalized before hashing/verifying/policy checks. Never
  change the normalization form without a migration path.
- Emails: trimmed, lowercased, printable ASCII only.
- One-time tokens (`user_tokens`) are consumed with a single conditional `UPDATE … RETURNING`.
- Passwordless accounts (`password_hash` NULL, created by Google sign-in) get no reset token (forgot sends a no-link
  notice) and the uniform 401 on password login. A deferred trigger requires every user to keep a password or an
  identity; flows that remove one must lock the user row `FOR UPDATE`.

## Lock order (012, 017)

- Transactions that touch an existing user's `user_tokens` lock the `users` row `FOR UPDATE` first; session-only
  operations (refresh, logout) lock a single session row and nothing else. Login takes the user row `FOR SHARE`.
- Anything outside `auth` that needs to serialize on a user takes the `users` row first too (a language save does,
  `FOR NO KEY UPDATE`, 029), so it waits for these transactions and never deadlocks with them.
- Retention cleanup takes rows `FOR UPDATE SKIP LOCKED` and never waits for a lock (018).

## Rate limits (018)

- Per IP in `server` (wrapped around the route in `server.New`), per account in `auth.Service`.
- Per-account checks run after validation and before any DB or argon2 work, keyed by the normalized address whether
  or not the account exists, and every attempt spends a token whatever its outcome.
- Limiters degrade open (a new key at the cap is allowed and logged); resource bounds degrade closed.

## Email (007, 018, 019)

- `internal/email` handles delivery only; `auth → email`, never the reverse. `auth` owns content, links and expiry.
- Emails are sent in the background with a context derived via `context.WithoutCancel`, never the request context.
- 202 means accepted, not delivered: email is best effort (bounded in-flight sends, excess dropped, no retries)
  until an outbox exists.
- `ResendSender` does no retries and sanitizes its errors. Resend click/open tracking must stay disabled (links
  carry tokens).
- The email-link pages (`/verify-email`, `/reset-password`): GET never uses a token or the DB; POST calls the same
  service as the API.

## Client session layer (023; the boundary screens see is in `mobile.md`)

- `SessionManager` holds the only in-memory tokens and owns refresh (single flight, generation check, `/healthz`
  probe first, the 014/018 failure matrix in 023) and logout.
- A refresh is never resent after an outcome that may have committed: only a 429 allows the same refresh token again.
- `SecureTokenStore` (one `flutter_secure_storage` key, explicit `AndroidOptions`) is the only place tokens persist.
