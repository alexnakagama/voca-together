---
paths:
  - "backend/internal/googleid/**"
  - "backend/internal/auth/google.go"
  - "backend/internal/auth/google_test.go"
  - "backend/internal/server/google_test.go"
  - "backend/cmd/api/google_test.go"
  - "backend/internal/config/google_test.go"
  - "backend/internal/db/migrations/00003_google_identities.sql"
  - "backend/internal/db/migrations/00004_drop_google_id_token_uses.sql"
  - "mobile/lib/auth/google_identity.dart"
  - "mobile/lib/auth/google_identity_plugin.dart"
  - "mobile/lib/auth/google_identity_exception.dart"
  - "mobile/lib/screens/google_sign_in_section.dart"
  - "mobile/lib/ui/widgets/google_sign_in_button.dart"
  - "mobile/test/**/*google*"
---

# Google sign-in rules (`POST /v1/auth/google`, both sides)

Records: 026 (the current acceptance policy), 020 (backend; its Stage 7 summarizes the contract, and its header
says which passages 026 replaced), 025 (client). The handler is in `server/auth.go`, the transaction is
`googleSignIn` in `auth/store.go`, and the client entry point is `SessionManager.signInWithGoogle()`.

## Contract

- Google establishes identity only. The Google ID token travels only in that request's body and is never an API
  credential; the response is an ordinary vt session, exactly as login returns it. Only `vt_at_`/`vt_rt_` tokens
  authenticate anything else.
- Verification is local and its security boundary is the **exact** `aud` match against `GOOGLE_CLIENT_ID` (the Web
  client ID). `azp` and nonce are deliberately unchecked. That rests on our deployment assumptions (020 Stage 6
  preconditions), not on a Google guarantee; see 020 Stage 2 before changing the Google Cloud project or adding a
  web client.
- An ID token is accepted every time it is presented while it verifies (026): Google returns the same token to the
  app again while it is valid, so nothing records a token's use and `google_id_token_uses` was dropped by migration
  00004. Accepted risk: a captured valid token can open a session until it expires (about an hour), even after a
  logout or a password reset. Don't reintroduce a replay table; real replay protection needs a per-request nonce,
  which `google_sign_in` can't send.
- Accounts are found by `(google, sub)`; the token's email is never synced. A new identity creates a passwordless
  account only if Google says its email is verified (no Gmail/Workspace-only restriction; don't add one). An address
  that already has an account gets 409 `account_exists`: nothing is linked automatically, and that account may have
  no password. Its 409 is the one deliberate enumeration exception.
- Never log the ID token, its hash, `sub`, the email or the client ID.

## Backend

- `internal/googleid` knows nothing about users, the DB or HTTP routes. `server` never imports it, and only `main`
  constructs the verifier (`config.md`).
- Limits: it shares `ip_login` with password login, and its `account_login` key is `google:` + the verified `sub`
  (known only after verification, still before the DB).

## Client

- `SessionManager.signInWithGoogle()` gets an ID token from `GoogleIdentity`, posts it once in the body of
  `/v1/auth/google` and drops it. An ID token is never a field, never stored, never kept and never retried: every
  attempt asks Google again (Google may answer with the same token).
- `PluginGoogleIdentity` is the only importer of `package:google_sign_in` and is built only in `main`. It reads only
  the ID token (no email, no scopes, no silent sign-in, no nonce) and initializes the plugin once.
- Screens use `GoogleSignInSection` and may import only `google_identity_exception.dart` from the Google files.
- Logout also clears Google's credential state, best effort, and the next Google sign-in waits for it (call order for
  the plugin, not security; bounded at 10 s).
- `GOOGLE_SERVER_CLIENT_ID` is the Web OAuth client ID (the backend's `GOOGLE_CLIENT_ID`; never an Android client ID
  or a secret): required in release builds, optional in debug, where leaving it out hides the Google button.
- `GoogleSignInButton` follows Google's branding guidelines: its colors and the official logo in `assets/google/`
  must not be changed.
- No `google-services.json`, no Firebase.
