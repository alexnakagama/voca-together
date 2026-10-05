# 025: Google sign-in in the Flutter client (client stage 6)

> **Status:** in force. Written together with 026, which it refers to for the acceptance policy.
>
> **Backend side:** 020 and 026.
>
> **Current rules:** `.claude/rules/google-sign-in.md`.

- **Scope:** "Continue with Google" on log in and register, over the backend contract of 020 (stage 7), which is
  unchanged. No backend change, no new route, no deep link, no linking, no Firebase and no `google-services.json`.
  One package: `google_sign_in` ^7.2.0 (Credential Manager on Android), the version 020 was audited against, plus
  `google_sign_in_platform_interface` as a dev dependency so tests can install a fake platform.
- **The ID token stays inside the session layer** (amends 023, which had screens pass it in).
  `SessionManager.signInWithGoogle()` takes no argument: it asks a `GoogleIdentity` for a token, posts it with
  `AuthApi.google` and starts an ordinary session, exactly as password login does. The token is a `String` in three
  places only: the return value of `GoogleIdentity.idToken()`, a parameter of the closure that sends it, and the JSON
  body of `POST /v1/auth/google`. It is never a field, never stored, never logged, never in a route or an exception,
  and never sent anywhere else. Only `vt_` tokens are persisted, only by `SecureTokenStore`.
  - `lib/auth/google_identity.dart`: the interface (`idToken()`, `clear()`). Built only in `main`, held only by
    `SessionManager`.
  - `lib/auth/google_identity_plugin.dart`: `PluginGoogleIdentity`, the only importer of `package:google_sign_in`.
  - `lib/auth/google_identity_exception.dart`: `GoogleIdentityException(GoogleIdentityFailure)`, the only Google
    library screens may import. It holds the enum and nothing from the plugin, whose messages can name the account
    or the client.
  - `test/architecture_test.dart` enforces all of it: the import allowlists; no `idToken`, `id_token`,
    `GoogleIdentity`, plugin type or client ID named in `lib/screens/` or `lib/ui/`; no ID-token field or variable
    anywhere; `signInWithGoogle()` without parameters; `main` giving the identity to the session only; and no print
    or log in `api/`, `auth/` or `session.dart`.
- **What the adapter asks Google for:** one interactive `authenticate()` with no scopes, and from its result only
  `authentication.idToken`. It never reads the email, name, photo or id, never calls
  `attemptLightweightAuthentication`, never listens to `authenticationEvents`, and never requests authorization, so no
  Google access token or server auth code exists. `initialize` gets only `serverClientId`: no `clientId` (Android
  ignores it), no nonce and no hosted domain (020).
  - *Read in the plugin's source* (`google_sign_in` 7.2.0, `google_sign_in_android` 7.2.17, 2026-10-03): `authenticate`
    uses the button flow (`useButtonFlow: true`); `account.authentication` is a synchronous getter over the token
    that came with the sign-in; `signOut()` calls `clearCredentialState`; a missing `serverClientId` is reported as
    `clientConfigurationError`.
  - *Documented by the plugin:* `initialize` must be called exactly once, and calling it again is undefined behavior.
    So the adapter initializes lazily on first use and keeps the outcome, a failure included, for the life of the
    process: after a failed initialization Google sign-in reports `unavailable` until the app restarts.
  - *Observed on a device* (emulator, Google Play services, 2026-10-04): repeated `authenticate()` calls returned
    the **same** ID token, byte for byte, about 35 minutes after it was issued and in a newly started app process.
    Nothing in the app or the plugin cached it (the plugin builds a new Credential Manager request per call), so the
    reuse is on Google's side. Google's documentation doesn't describe it. Whether `signOut()` ends it was not
    observed. The first design assumed a new token per call and a backend that accepted each token once; together
    they made every Google sign-in after the first fail until that token expired. 026 changes the backend rule.
  - The token is checked for shape only before it is sent: present, at most 4096 bytes (the backend's cap), three
    non-empty base64url segments. Nothing is decoded and no claim is read; `aud`, `iss`, signature, expiry and
    `email_verified` are the backend's checks, on the assumptions of 020. A token that passes them is accepted every
    time it is presented until it expires: see 026 for the current acceptance policy.
  - Failures become `GoogleIdentityFailure`: `canceled` → `cancelled`; `interrupted`; `uiUnavailable` and
    `providerConfigurationError` → `unavailable`; `clientConfigurationError` → `misconfigured`; no token or a
    misshapen one → `malformed`; everything else, including a `PlatformException` or an `Error` from the plugin →
    `unknown`. One call at a time (a second one is a `StateError`).
- **Session integration:** `signInWithGoogle` shares the sign-in slot with `signIn` (signed out, no other sign-in or
  logout running, else `StateError`), and holds it while the account chooser is open. The access-token deadlines are
  read after the ID token arrives, just before the request, so a slow choice doesn't shorten the session's first
  token. If the manager is disposed while the chooser is open, the token is dropped unsent. Without a
  `GoogleIdentity` (`googleSignInAvailable` false) the method throws `StateError`.
- **No retries:** every attempt is the user's tap and asks Google again; the app sends each answer in one request
  and keeps nothing. After 401, 403, 409, 429, 500, 503, a timeout or a network error nothing is resent
  automatically. The token of a later attempt may be the same one (see above); the backend accepts it (026).
- **New and returning users** get the same 200 and the client doesn't tell them apart. Account creation,
  `email_verified`, the 409 for an address that already has an account, and passwordless accounts' forgot-password
  and password-login behavior are all the backend's (020) and need no client code. Nothing is linked.
- **Logout** also clears Google's credential state on the device: after the local session is gone,
  `GoogleIdentity.clear()` (the plugin's `signOut`) runs unawaited and its failure is swallowed, so it can neither
  delay nor fail logout. It revokes nothing at Google. It runs after password sessions too, because the Google account
  may have been chosen in an earlier run. Startup never touches Google: the session is restored from `TokenStore`.
- **A Google sign-in waits for the Google sign-out of the last logout.** This is call order for the plugin, not
  replay protection, and it was not the cause of the replays seen in the first smoke tests (those were Google
  returning the same token, above).
  - *Documented by the plugin:* clients "should not call `authenticate` to obtain a new `GoogleSignInAccount` instance
    until after a call to `signOut`". We read "after" as after its future completes.
  - `SessionManager` keeps the running sign-out as `_googleSigningOut` (cleared when it ends, whatever its outcome).
    Logout returns without waiting for it. `signInWithGoogle()` waits for it, inside the sign-in slot and before
    `GoogleIdentity.idToken()`. With no sign-out running, nothing is delayed.
  - The wait is bounded (`googleSignOutWait`, 10 s): after a sign-out that never finishes the sign-in goes ahead
    rather than leave the user unable to sign in with Google. A sign-out that fails ends the wait at once.
- **Configuration:** `GOOGLE_SERVER_CLIENT_ID` in `config/<env>.json` is the Web OAuth client ID, the same value as
  the backend's `GOOGLE_CLIENT_ID`. It is public. `parseGoogleServerClientId` applies the backend's structural rule
  (printable ASCII without spaces, at most 255, ending in `.apps.googleusercontent.com` after a non-empty prefix) and
  never echoes the value. Release builds refuse to start without a valid one; a debug build may omit it, and then no
  Google button is shown. An invalid value fails in every build. No client secret, API key, service account, Android
  client ID or Firebase file is in the app.
- **Screens:** `GoogleSignInSection` (`lib/screens/`; a divider, the stage 3 `GoogleSignInButton` unchanged, a labelled
  progress bar) calls `signInWithGoogle()` and hands the mapped failure to its host, which shows it in its own
  banner. Log in and register show the section only when `googleSignInAvailable` (register only on its form view),
  lock their fields, button and links while it runs, and disable it while their own request runs. Success needs no
  navigation code: the session changes and `authRedirect` reacts. The section catches `Object`, not only `Exception`,
  so a `StateError` ends as the generic message and a usable form.
- **Messages** (`presentFailure`): a closed chooser → `cancelled`, nothing shown; every other Google failure →
  "Google sign-in isn't available right now"; 401 `invalid_google_token` → "didn't work, try again";
  403 `google_email_unusable` → Google hasn't confirmed the email; 409 `account_exists` → an account for that
  address exists, "log in the way that account was set up". The 409 text never mentions a password (the account may
  have none) and never shows an address (the client never reads one). 429, 503, 5xx, network and timeout keep the
  messages of 024.
- **Google Cloud and Android setup** (one project, per 020's deployment assumptions):
  - A **Web application** OAuth client: its ID is `GOOGLE_CLIENT_ID` and `GOOGLE_SERVER_CLIENT_ID`. No JavaScript
    origins, no redirect URIs; its secret is never downloaded.
  - An **Android** OAuth client per signing key: package `com.vocatogether.app` and the certificate's SHA-1. Today
    that is each developer's debug keystore, which also signs `--release` builds (the Gradle template's TODO). A real
    release adds the upload key and, on Play, the Play App Signing key; without the latter, store builds fail while
    local ones work. SHA-256 isn't registered for this flow. Android client IDs appear nowhere in the app or backend.
  - No manifest or Gradle change and no Google Services plugin. A device needs Google Play services and a Google
    account. A consent screen in "Testing" admits only listed test users.
- **Known limits:**
  - The plugin documents that a misconfigured client (wrong SHA-1, package or client ID) can surface as `canceled`.
    This design shows a cancel as silence, so that misconfiguration looks like a button that does nothing. It is a
    setup error, caught by the smoke test, not something a user can fix.
  - If the user leaves register with Android back while a Google sign-in is still running, the log in screen's
    button is enabled; tapping it is refused by the session (one sign-in at a time) and shows the generic message.
    The first attempt still completes.
- **Tests (host only):** the adapter over a fake `GoogleSignInPlatform` (every plugin code, malformed tokens, single
  initialization, concurrency, nothing but `authenticate` and `signOut` called); the session (body-only, one request
  per Google answer, Google asked again on every attempt, every backend failure, concurrency with password sign-in
  and logout, dispose, deadlines, logout ordering and Google failures); signing in again through the real adapter
  over a fake platform that returns the identical token every time (after a logout, after a revoked session, in a
  new process), and the order of sign-out and sign-in (the wait, a failed sign-out, a sign-out that never ends);
  config; the mapping; the section on both screens (success, cancel, every failure, duplicate taps, keyboard submit,
  leaving mid-flow); accessibility for the new states; and leak and privacy runs with marked ID tokens and echoing
  error bodies. Each boundary was also broken on purpose once to see its test fail.
- **Deferred:** real release signing and the SHA-1 of the upload and Play App Signing keys (a blocker for a store
  release, not for this stage); a published consent screen; a release (R8) build smoke-tested on a device; and
  everything 020 defers (linking, `azp` and nonce, RISC, recovery for lost Google accounts, metrics).
