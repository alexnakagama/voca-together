# 021: Flutter app shell: session state and routing (client stage 2)

> **Status:** in force for routing and ownership; the session class it describes was replaced.
>
> **Changed later:** 023 replaced `Session` and its `markSignedIn`/`markSignedOut` with `SessionManager`
> (`SessionStatus` and the redirect contract stayed). 028 lets a signed-in user be on any route of
> `Routes.signedInRoutes`, not only `/home`. 032: routes are no longer all parameterless and the caller's own:
> `/members/<id>` names a member by public identifier, and `authRedirect` also accepts that pattern (still a
> function of the status and the path alone). The deferred work was done in 023 (session restore), 024
> (screens, logout) and 025 (Google sign-in); deep links are still off.
>
> **Current rules:** `.claude/rules/mobile.md`.

- **`go_router`** (flutter.dev, BSD-3) is added to the Flutter libraries of 008. Routes are flat, top-level and
  parameterless, defined as constants in `Routes` (`lib/router.dart`); `go_router_builder` and code generation are not
  worth it until routes take parameters.
- **Session state:** `Session` (`lib/session.dart`) is a `ChangeNotifier` holding a `SessionStatus`: `unknown` (startup
  hasn't decided yet), `signedOut` or `signedIn`. It stores no tokens and does no I/O; whatever learns the status
  reports it through `markSignedIn` / `markSignedOut`, and listeners are notified only on an actual change. There is no
  way back to `unknown`. Until the token manager exists (Stage 3), `main` resolves startup to `signedOut` at once.
- **One navigation policy:** `authRedirect(status, uri)` is a pure function and the router's only redirect:
  `unknown` → `/splash`; `signedOut` → stays on `/login`, `/register` or `/forgot-password`, otherwise `/login`;
  `signedIn` → `/home`. Paths are matched exactly (query ignored), so unmatched or unexpected locations go to the
  status's default route instead of an error page, and every destination is final (no redirect loop). Screens may
  `go` to a route to express intent, but never decide access; the redirect runs on every navigation and, via
  `refreshListenable`, on every session change.
- **Ownership:** `main` is the composition root and owns the `Session` for the life of the process. The router is
  created and disposed by `VocaTogetherApp`'s `State` (recreated if the session instance changes); disposing it
  removes its listener from the session. No top-level mutable state, no service locator or state-management package.
- **Android deep links are off** (`flutter_deeplinking_enabled=false`): `MainActivity` is exported, so any app could
  otherwise choose our initial route. The redirect would contain it, but deep links (e.g. the email links of 006)
  need their own design and decision.
- Routes never carry tokens, email addresses or other personal data, and router diagnostics logging stays off.
- **Deferred:** token manager and session restore from `flutter_secure_storage` (014/015 client contracts), real
  screens, logout, Google sign-in, deep links and a return-to location after login, state restoration.
