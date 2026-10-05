# 026: Google ID tokens are accepted while valid, not once (supersedes single acceptance in 020)

> **Status:** in force. **Supersedes** the single-use rule for Google ID tokens in 020; the header of 020
> lists the passages it replaced.
>
> **Current rules:** `.claude/rules/google-sign-in.md`.

- **What changes:** `POST /v1/auth/google` accepts an ID token every time it is presented, for as long as it passes
  the verification of 020 (signature against Google's keys, `iss`, exact `aud`, `exp`/`iat`/`nbf` with the skew,
  `sub`). 020 accepted each verified token at most once and answered a second use with 401. That rule is gone,
  with the `google_id_token_uses` table and everything that wrote, read or cleaned it. Everything else in 020
  stands: accounts are found by `(google, sub)`, a new identity needs a Google-verified email, an address that
  already has an account is a 409 with nothing linked, `azp` and nonce are unchecked.
- **Why:** on Android, `google_sign_in` 7.2.0 (Credential Manager) returned the same ID token from repeated
  `authenticate()` calls, including in a new app process, about 35 minutes after the token was issued (025). With
  single acceptance, the first Google sign-in on a device worked and every later one (after a logout, a reinstall,
  an expired session) was refused as a replay until Google issued another token, up to about an hour later. The
  client can't ask for a new one: the plugin has no such call, `signOut()` isn't documented to do it, and
  `disconnect()` revokes the user's grant, which a logout must not do.
- **What Google asks of a backend:** its verification guides (ID token verification, backend authentication for
  Android) list the signature, `aud`, `iss` and `exp`, then finding or creating the user by `sub`. They don't say a
  token may be accepted only once. Replay protection is offered through the nonce, which the Credential Manager
  guide calls optional ("To enable enhanced security").
- **Accepted risk, stated exactly:** whoever holds a valid ID token issued for our Web client ID can obtain a
  VocaTogether session for that Google account's VocaTogether account until the token expires: at most its lifetime
  (about an hour, set by Google) plus the verifier's one-minute skew. In particular:
  - a VocaTogether logout doesn't make the token unusable;
  - neither does revoking the account's sessions (password reset, 017): for an account with a Google identity, a
    still-valid token can open a new session afterwards;
  - the per-subject and per-IP limits (018) bound how often, not whether.
  An ID token is only that. It is not a Google access or refresh token and gives no access to the user's Google
  account, and it is not a VocaTogether token: holding one yields nothing but the ability to call this endpoint
  with it. The app requests no Google authorization, so no Google access or refresh token exists to be taken with
  it. The token travels only from Google's on-device component to the app's memory and, over TLS, in the body of
  this one request; it is never stored or logged on either side (020, 025). So the risk needs a compromise of the
  device, of that TLS connection or of the API process.
- **Not chosen:**
  - *Keep single acceptance:* fails for real users, as above.
  - *Keep the table as an audit record:* a repeated token is now the normal case, so "seen before" says nothing, and
    token hashes are never logged.
  - *Authorization-code flow:* needs the Web client's secret on the backend and a call to Google on every sign-in
    (020 has neither), and yields Google tokens this product has no use for. The plugin documents that server auth
    codes may be available only on the first sign-in.
  - *A nonce per sign-in:* see below.
- **Nonce stays deferred** (020), now for a stated reason. A server-issued, single-use nonce in each request is the
  one replay protection Google documents, and it would bind every token to one sign-in attempt. `google_sign_in`
  can't do it: its only nonce is the one given to `initialize`, which may be called once per process, so every
  request would carry the same nonce. It needs our own Android integration with Credential Manager
  (`GetSignInWithGoogleOption.Builder.setNonce` per request) in place of the plugin, plus an endpoint and storage for
  issued nonces. That is the upgrade path if the accepted risk stops being acceptable. Our inference, not verified:
  a request with a different nonce can't be answered with a cached token, because the nonce is a claim inside it.
- **Backend:**
  - Migration `00004_drop_google_id_token_uses.sql` drops the table (with any rows). Its down recreates the empty
    table exactly as 00003 defined it, so 00003's own down still runs. 00003 is unchanged.
  - `googleSignIn` no longer consumes the token. Its transaction starts at the identity lookup; the token never
    reaches the store. The `googleReplayed` outcome, the `replayed` log reason, the token-use cleanup and its
    `google_token_uses_deleted` count, and the service's check for claims without an expiry are removed.
    `Claims.AcceptedUntil` stays in `googleid` (the verifier's own cut-off) but nothing outside it reads it.
  - Requests carrying the same token at once: one inserts the user and identity; the others wait on the email's
    unique constraint, find the identity on the re-run lookup, and sign in. One account, one identity, a session
    each. The lock-order argument of 020 holds with one key fewer (email, then subject).
  - 403 and 409 are no longer "spent": presenting the same token again gets the same answer while the accounts are
    unchanged, as before, and a token refused once would be accepted later if the refusal stopped applying.
- **Client retry rule (replaces 020's):** after a 500, a 503 or no response the outcome is still unknown, and the
  user's next attempt, with the same ID token or another, signs in to the same account. A session created by a lost
  attempt is orphaned and expires. The app still never resends by itself (025).
- **HTTP:** the table in 020 stage 7 holds without its "Token spent?" column, and 401 `invalid_google_token` now
  means only "rejected by the verifier, or Google sign-in not configured".
- **Removed from the client:** the debug-only token-fingerprint trace that established the cause (and its `crypto`
  dependency). With no record of accepted tokens there is nothing left for a fingerprint to be compared with.
- **Tests:** the same token twice, concurrently, and after a 403 or 409 (store, service and endpoint); no duplicate
  user or identity; nothing of the token in the database; the verifier's signature, `aud`, `iss` and expiry
  rejections unchanged; migration 00004 up with a row present, down, and through a full rollback and rebuild. Client:
  025.
- **Deployment:** migrations run at startup, so the first start of this version drops the table. An older binary
  started afterwards would fail every Google sign-in with a 500 (the table it writes to is gone); roll back by
  migrating down first.
