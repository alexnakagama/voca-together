---
paths:
  - "backend/**"
  - "mobile/**"
  - "docs/decisions.md"
---

# Security rules

`docs/decisions.md` is the source of truth and holds the reasoning. These are the invariants; read the cited decision
before changing any of them. The never-log list and the secrets rule are in `CLAUDE.md`.

## Auth invariants

- Opaque DB-backed tokens, not JWT: access `vt_at_…` (15 min), refresh `vt_rt_…` (30 d sliding, 90 d absolute).
  Only SHA-256 hashes are stored. Refresh rotates every use; reuse of the previous refresh token revokes the session.
- No account enumeration: register/resend/forgot always 202; login gives the same 401 for unknown email and wrong
  password, with equal work (dummy-hash verification).
- Emails are sent in the background with a context derived via `context.WithoutCancel`, never the request context.
- 202 means accepted, not delivered: email is best effort (bounded in-flight sends, excess dropped, no retries)
  until an outbox exists.
- Uniform errors for all unusable tokens (one code per token kind); malformed tokens are rejected before any DB
  access.
- Passwords: argon2id (PHC format, rehash on login), NFKC-normalized before hashing/verifying/policy checks. Never
  change the normalization form without a migration path.
- Emails: trimmed, lowercased, printable ASCII only.
- One-time tokens (`user_tokens`) are consumed with a single conditional `UPDATE … RETURNING`.
- Lock order: transactions that touch an existing user's `user_tokens` lock the `users` row `FOR UPDATE` first;
  session-only operations lock a single session row.
- `auth.Token`, `googleid.Claims` and `config.Secret` redact themselves in fmt, slog and JSON.

## Google sign-in (`POST /v1/auth/google`; decision 020, stage 7 summarizes the contract)

- Google establishes identity only. The Google ID token travels only in that request's body and is never an API
  credential; the response is an ordinary vt session, exactly as login returns it. Only `vt_at_`/`vt_rt_` tokens
  authenticate anything else.
- Verification is local and its security boundary is the **exact** `aud` match against `GOOGLE_CLIENT_ID` (the Web
  client ID). `azp` and nonce are deliberately unchecked. That rests on our deployment assumptions (020 Stage 6
  preconditions), not on a Google guarantee; see 020 Stage 2 before changing the Google Cloud project or adding a
  web client.
- An ID token is accepted every time it is presented while it verifies (decision 026, which supersedes 020's single
  acceptance): Google returns the same token to the app again while it is valid, so nothing records a token's use
  and `google_id_token_uses` was dropped by migration 00004. Accepted risk: a captured valid token can open a session
  until it expires (about an hour), even after a logout or a password reset. Don't reintroduce a replay table; real
  replay protection needs a per-request nonce, which `google_sign_in` can't send (026).
- Accounts are found by `(google, sub)`; the token's email is never synced. A new identity creates a passwordless
  account only if Google says its email is verified (no Gmail/Workspace-only restriction; don't add one). An address
  that already has an account gets 409 `account_exists`: nothing is linked automatically, and that account may have
  no password. Its 409 is the one deliberate enumeration exception (020).
- Passwordless accounts (`password_hash` NULL) get no reset token (forgot sends a no-link notice) and the uniform 401
  on password login. A deferred trigger requires every user to keep a password or an identity; flows that remove one
  must lock the user row `FOR UPDATE`.
