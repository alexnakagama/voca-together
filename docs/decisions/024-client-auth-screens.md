# 024: Authentication screens (client stage 5)

> **Status:** in force.
>
> **Changed later:** 025 gave the Google error codes their own messages; 028 added the profile's field
> errors and one more client check (an empty name).
>
> **Current rules:** `.claude/rules/mobile.md`.

- **Scope:** log in, register, forgot password, resend verification and a home screen, on the stage 4 layers. No
  backend change, no new endpoint, no new package. Google sign-in (the plugin, the button on log in, ID tokens) is
  stage 6.
- **Wiring:** `main` now builds `AccountApi` (023 deferred it until a screen needed it) and passes it with the session
  to `VocaTogetherApp`, which passes both to `createRouter`; each route builder gives its screen only what it uses
  (log in: session + `AccountApi`; register and forgot: `AccountApi`; home: session). The architecture test's "main
  passes only config and session" rule became "config, session and `AccountApi`". `Routes` and `authRedirect` are
  unchanged.
- **Screens own only ephemeral state** (controllers, focus nodes, `_busy`, errors, form vs. success view) in plain
  `StatefulWidget`s; no notifier, controller class or global state. They never read `session.status`: success needs no
  navigation code, because the session changes and the redirect reacts. The architecture test forbids `SessionStatus`,
  `.status` and `authRedirect` in `lib/screens/`, and any print or log in `lib/screens/` and `lib/ui/`.
- **One failure mapping:** `presentFailure(error, l10n)` (`lib/screens/failure_presentation.dart`) is the only place
  that turns failures into words. It switches on the exception type, then on the server code, then on the status for
  code-less bodies (a proxy's page): `validation_failed` → field errors for `email:invalid` and the five password
  codes, plus a generic banner for any field or code it can't show; `invalid_credentials`; `email_not_verified`;
  `rate_limited`/429 and `service_unavailable`/503 with the `Retry-After` wait in words (seconds under a minute, then
  minutes, then hours, rounded up); `invalid_access_token`/`invalid_refresh_token` → "couldn't confirm your session";
  network and timeout; everything else (5xx, 400, unknown codes, protocol errors, storage failures) → one generic
  message. The Google codes are listed explicitly and map to the generic message until stage 6. Screens branch only
  on the resulting `FailureKind`; codes are switch keys, so no server text is ever displayed. The API layer stays free
  of wording.
- **Client validation is minimal by design:** blank email, empty password, empty confirmation, confirmation ≠
  password. No length, policy, email-format or normalization rule is duplicated; values are sent exactly as typed
  (spaces and Unicode included). Password-policy messages are generic and carry no numbers, so nothing can drift from
  `PasswordMinLength`/`PasswordMaxLength`.
- **Log in:** a 401 clears and focuses the password; a 403 clears the password and shows a notice plus the resend
  section for the address of that attempt (editing the email leaves that state); a 422 puts errors on fields and
  focuses the first; every other failure keeps both fields (it says nothing about the credentials, and 018 makes a
  retry safe). On success the form stays busy until the router replaces it. No autofocus on any screen.
- **Register / forgot password:** a 202 switches the screen in place to a confirmation that reads the same whatever
  the account's state. Register's text ("we've sent a message … with the next steps") is true for a new address
  (verification link) and an existing one (account-exists notice), so it never says whether an account was created;
  forgot's says "if there's an account". Register calls `TextInput.finishAutofillContext()` so a password manager can
  save the new password, then clears both password fields. The emailed links open the backend's pages (deep links
  stay off, 021).
- **Resend verification is inline, not a route:** `ResendVerificationSection(accountApi, email)` appears on log in after
  a 403 and on register's confirmation. A route would have to carry the address, and routes never carry emails (021);
  go_router `extra` avoids the URL but would still need a new auth route. A user who left after registering reaches
  it again through log in → 403. Its confirmation is neutral; the button stays available after success (the server's
  `account_mail` limit decides) and a late answer for an earlier address is dropped.
- **Home:** loads `me()` with a request id so stale answers (a retry, logout) are ignored; shows a labelled spinner,
  then the email and the account's creation date (no id), or the mapped error with "Try again". A
  `SignedOutException` shows nothing: the session is already signed out and the router is leaving. Network, timeout,
  the refresh probe failing, 429 and 5xx keep the session (offline users stay signed in). "Log out" calls
  `SessionManager.logout()`, which never throws; the server's answer changes nothing.
- **No automatic retries and no client lockout:** every retry is the user's action. A 429 shows the wait; the button
  stays enabled and screens hold no timers (the server is authoritative, and a 429 did nothing).
- **Navigation:** log in opens register and forgot password with `push`, so Android back returns to log in; "Back to
  log in" uses `go`. `push` still passes through the redirect, and a session change re-runs it over the whole stack
  (tested: signed in while register is on top lands on `/home` with nothing below).
- **Widgets:** `FormNoticeBanner` (the neutral counterpart of `FormErrorBanner`: secondary-container colors, an info
  icon with a semantic label, a live region) and `SecondaryButton` (full-width outlined, with the same busy behavior
  as `PrimaryButton` minus the tap latch; callers guard with their own busy flag) join `lib/ui/widgets/`, with
  previews.
- **Tests (host only):** screens are tested through the real app (`test/screens/harness.dart`: router,
  `SessionManager` and `AccountApi` over one `FakeServer`), with fake time for timeouts. Covered: the mapping for
  every code, status class and exception; each screen's validation, busy and double-submit guards (including the
  keyboard path), success, every failure class and field placement; home's load, failure, retry, session-ended and
  logout races; end-to-end navigation; accessibility guidelines (tap targets, labels, contrast in light and dark) for
  every screen state, 2.0 text at 320×480 and an open keyboard; and a privacy run with marked secrets and echoing
  error bodies checking that nothing is printed, routes carry no personal data, and no token, password or server
  text is rendered.
- **Deferred:** Google sign-in (stage 6); verify-email and reset-password in the app (with deep links); refreshing
  home on resume; state restoration of forms; a visible back button on register and forgot password (Android back
  and the in-page link cover it); more locales; golden tests.
