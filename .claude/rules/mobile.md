---
paths:
  - "mobile/**"
---

# Mobile rules (Flutter, Android only)

The layer map is in `docs/architecture.md`. Test fakes and harnesses are in `testing.md`.

## Build config

- Build-time config comes only from `--dart-define-from-file` (`mobile/config/<env>.json`). `lib/config.dart`
  validates it at startup and throws if it's missing or invalid. Every value is compiled into the APK and is public:
  never put a secret there.
- `API_BASE_URL` is an http(s) origin with no path, query, fragment or credentials. Release builds require https.
- `GOOGLE_SERVER_CLIENT_ID` is the Web OAuth client ID (the backend's `GOOGLE_CLIENT_ID`; never an Android client ID
  or a secret): required in release builds, optional in debug, where leaving it out hides the Google button.

## Cleartext and the manifest

- Every API request must be built from `API_BASE_URL`. Dart's own sockets (`dart:io` `HttpClient`, `package:http`'s
  default client on Android) ignore Android's Network Security Configuration (Flutter doesn't pass it to the Dart
  VM), so the https requirement on `API_BASE_URL` in release builds is the only cleartext control for Dart traffic.
- The debug-only `android/app/src/debug/res/xml/network_security_config.xml` (cleartext only to `10.0.2.2`) governs
  only platform stacks: WebView, and `cronet_http`/`ok_http` if adopted.
- `INTERNET` is declared in the main manifest because Flutter's template grants it only in debug and profile builds.
- Android deep links are disabled in the manifest until designed. App backup and device transfer are disabled too.

## Structure (decision 021)

- `main.dart` is the only place long-lived objects are built; pass them down by constructor (no
  provider/riverpod/bloc/get_it, no top-level mutable state). `createRouter` hands each screen only what it uses.
- `app.dart` owns and disposes the `GoRouter`. `router.dart` holds `Routes` and `authRedirect`, the only navigation
  policy.
- Screens (`lib/screens/`) never read or change session state or decide access: they call
  `SessionManager`/`AccountApi` and the redirect reacts. `SessionStatus`, `.status` and `authRedirect` are forbidden
  in `lib/screens/`.
- Never put a token or email in a route.

## Networking and session (decision 023, read it before touching auth code)

- `ApiClient` holds no auth state, never retries, never follows redirects and never logs.
- `SessionManager` holds the only in-memory tokens and owns refresh (single flight, generation check, `/healthz`
  probe first, the 014/018 failure matrix in 023) and logout.
- **Token boundary:** screens may use only `AccountApi` (register/resend/forgot, token-free) and `SessionManager`'s
  public API, which takes and returns no token (`signIn`/`signInWithGoogle()`/`logout` → `void`, `me()` → `Me`).
- `AuthApi` (returns `AuthTokens`, takes raw tokens) is built only in `main` and held only by `SessionManager`. The
  generic request wrapper `_authorized` stays private; each new protected route gets a typed `SessionManager` method.
- `SecureTokenStore` (one `flutter_secure_storage` key, explicit `AndroidOptions`) is the only place tokens persist.
- Nothing in `api/`, `auth/` or `session.dart` may log.

## Google sign-in (decisions 025 and 026, read them with 020)

- `SessionManager.signInWithGoogle()` gets an ID token from `GoogleIdentity`, posts it once in the body of
  `/v1/auth/google` and drops it. An ID token is never a field, never stored, never kept and never retried: every
  attempt asks Google again (Google may answer with the same token; 026).
- `PluginGoogleIdentity` is the only importer of `package:google_sign_in` and is built only in `main`. It reads only
  the ID token (no email, no scopes, no silent sign-in, no nonce) and initializes the plugin once.
- Screens use `GoogleSignInSection` and may import only `google_identity_exception.dart` from the Google files.
- Logout also clears Google's credential state, best effort, and the next Google sign-in waits for it (call order for
  the plugin, not security; bounded at 10 s).
- No `google-services.json`, no Firebase.

## UI (decision 022)

- `lib/ui/theme.dart` holds `AppTheme` (M3 light/dark) and the `Spacing`/`Radii` constants.
- Widgets in `lib/ui/widgets/` never import `Session`, the router or `AppConfig` and hardcode no user-visible string.
- `GoogleSignInButton` follows Google's branding guidelines: its colors and the official logo in `assets/google/`
  must not be changed.
- Previews in `lib/ui/previews/` use `@VocaPreview` and stay pure UI. Add each new preview function to
  `test/ui/previews_test.dart`.

## Screens (decision 024)

- `StatefulWidget`s holding only ephemeral form state (controllers, focus, `_busy`, errors).
- Every `await` is followed by a `mounted` check, and the submit handler checks `_busy` (the keyboard bypasses the
  button).
- Every failure goes through `presentFailure` (`lib/screens/failure_presentation.dart`), the one mapping from
  `ApiException`s and server codes to localized text and field errors. Screens branch on `FailureKind`, never on code
  strings, and never show server text.
- Client validation is only empty fields and password confirmation. The password policy and normalization stay on
  the server, and values are sent exactly as typed.
- Success text for register, forgot and resend is neutral (no account enumeration).

## Strings

- Add them to `lib/l10n/app_en.arb` with an `@` description. `flutter pub get` (also run by `flutter
  test/run/build`) regenerates the committed `lib/l10n/app_localizations*.dart`. Never edit the generated files.
