---
paths:
  - "mobile/**"
---

# Mobile rules (Flutter, Android only)

The layer map is in `docs/architecture.md`. Test fakes and harnesses are in `testing.md`. Rules for one feature
load with that feature's files: `auth.md` (session internals), `google-sign-in.md`, `profile.md`, `languages.md`,
`safety.md` (blocking and reporting).

## Build config

- Build-time config comes only from `--dart-define-from-file` (`mobile/config/<env>.json`). `lib/config.dart`
  validates it at startup and throws if it's missing or invalid. Every value is compiled into the APK and is public:
  never put a secret there.
- `API_BASE_URL` is an http(s) origin with no path, query, fragment or credentials. Release builds require https.
- `GOOGLE_SERVER_CLIENT_ID`: see `google-sign-in.md`.

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
- Signed-in routes are the exact paths of `Routes.signedInRoutes` (`/home`, `/profile`, `/profile/edit`,
  `/profile/languages`, `/blocked`) and the two patterns `Routes.isMember` and `Routes.isMemberReport` accept
  (`/members/<public id>`, decision 032, and `/members/<public id>/report`, decision 034); `authRedirect` stays a
  function of the session status and the path only. The three `/profile` routes and `/blocked` are always the
  caller's own and name nobody (decisions 028, 030, 032, 034).
- A route carries no token, email, name or language, and nothing a member typed or chose. The only thing a
  route may say about anyone is a profile's public identifier, in `/members/<id>` and `/members/<id>/report`
  (`profile.md`, `safety.md`).
- Routes are flat `GoRoute`s and screens are pushed. `go_router` removes a trailing slash before the redirect
  runs, so only `authRedirect`'s own table can show that it refuses one.
- `main.dart` also builds the `PhotoSource` (`lib/media/`); only the profile edit screen receives it
  (`profile.md`).

## Networking and the token boundary (decision 023)

- `ApiClient` holds no auth state, never retries, never follows redirects and never logs. It sends GET, POST, PUT
  and DELETE, with a JSON body or a byte body (`application/octet-stream`), never both. `getImage` and
  `putForImage` return an image answer as bytes: it must be a 200 `image/jpeg` and has its own size cap; error
  answers are still parsed as JSON. Use PUT and DELETE only for writes the backend makes idempotent, because
  `_authorized` resends once after a 401 (028): the profile, the member's languages, the profile picture, a
  block, an unblock and a report.
- **Token boundary:** screens may use only `AccountApi` (register/resend/forgot, token-free) and `SessionManager`'s
  public API, which takes and returns no token (`signIn`/`signInWithGoogle()`/`logout` → `void`, `me()` → `Me`,
  `profile()` → `Profile?`, `saveProfile()` → `Profile`, `languageCatalog()` → `List<Language>`, `languages()` and
  `saveLanguages()` → `UserLanguages`, `avatar()` → `Uint8List?`, `saveAvatar()` → `Uint8List`, `removeAvatar()` →
  `void`, `memberProfile(id)` → `MemberProfile?`, `memberAvatar(id)` → `Uint8List?`, `blockMember(id)` and
  `unblockMember(id)` → `void`, `blockedMembers()` → `List<BlockedMember>`, `reportMember(id, reason, details)` →
  `void`). Only the languages editor calls `saveLanguages()` (`languages.md`); only the member profile screen
  calls the two member reads and `blockMember`; only the report screen calls `reportMember` (`safety.md`).
- Adding a protected route: a path in `ApiPaths`, a call in `AuthApi` (which takes the raw access token and is held
  only by `SessionManager`), and a typed, token-free `SessionManager` method that makes one `_authorized` call.
  `_authorized` stays private. A model screens may import is added to the allowlist in `test/architecture_test.dart`.
- Nothing in `api/`, `auth/` or `session.dart` may log.
- How tokens are stored, refreshed and cleared is in `auth.md`; read decision 023 before changing any of it.

## UI (decision 022)

- `lib/ui/theme.dart` holds `AppTheme` (M3 light/dark) and the `Spacing`/`Radii` constants.
- Widgets in `lib/ui/widgets/` never import the session, the router or `AppConfig` and hardcode no user-visible string.
- Previews in `lib/ui/previews/` use `@VocaPreview` and stay pure UI. Add each new preview function to
  `test/ui/previews_test.dart`.

## Screens (decision 024)

- `StatefulWidget`s holding only ephemeral form state (controllers, focus, `_busy`, errors).
- Every `await` is followed by a `mounted` check, and the submit handler checks `_busy` (the keyboard bypasses the
  button).
- Every failure goes through `presentFailure` (`lib/screens/failure_presentation.dart`), the one mapping from
  `ApiException`s and server codes to localized text and field errors. Screens branch on `FailureKind`, never on code
  strings, and never show server text.
- Client validation is only empty fields and password confirmation. Every policy, limit and normalization rule
  stays on the server, and values are sent exactly as typed.
- Success text for register, forgot and resend is neutral (no account enumeration).

## Strings

- Add them to `lib/l10n/app_en.arb` with an `@` description. `flutter pub get` (also run by `flutter
  test/run/build`) regenerates the committed `lib/l10n/app_localizations*.dart`. Never edit the generated files.
- The message for Google sign-in's 409 `account_exists` never mentions a password (the account may have none) and
  never shows an address.
