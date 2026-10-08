# 028: Profile screen (client stage 7)

> **Status:** in force, amended by 032.
>
> **Backend side:** 027. Draft 030 will add a Languages summary to this screen and a second signed-in route.
>
> **Changed later:** 032 split the screen in two. `/profile` is a read-only page; the form described below is
> `ProfileEditScreen` at `/profile/edit`, with the same fields, checks and save. A successful save returns to the
> page instead of showing "Profile saved." and the stored text in the form; leaving with unsaved changes asks
> first (deferred below); the notice names the picture too; and a route (`/members/<id>`) now names a member.
>
> **Current rules:** `.claude/rules/profile.md`, `.claude/rules/mobile.md`.

- **Scope:** the signed-in user creates, reads and edits their own profile, over the backend contract of 027. No
  new package. Nothing about other members.
- **Transport:** `ApiClient.send` now accepts `PUT` as well as GET and POST (023 allowed only those two). It is
  used only for the profile save, which is idempotent on the server. `ApiPaths.profile` is `/v1/me/profile`.
- **Token boundary, unchanged:** `AuthApi` gains `profile(accessToken)` and `saveProfile(accessToken, displayName,
  bio)`; `SessionManager` gains the token-free `profile()` → `Profile?` and `saveProfile(displayName, bio)` →
  `Profile`, each one `_authorized` call (023). `lib/api/profile.dart` (`Profile`, strict `fromJson`, redacted
  `toString`) joins the libraries screens may import; the architecture test's allowlist and its list of public
  `SessionManager` members were extended.
- **"No profile yet" is a value, not an error:** `profile()` returns null only for a 404 whose code is
  `profile_not_found`. Any other 404 (a proxy's page, a backend without the route) stays an `ApiHttpException`,
  so a broken deployment shows an error with a retry and never an empty form that a save would then overwrite
  nothing with.
- **Resending the save after a 401 is safe.** `_authorized` resends a refused request once after a refresh (023).
  For a write that is acceptable only because the save is idempotent (027): the second send stores the same text.
  Nothing else retries; after a timeout or a 503 the user's own retry is safe for the same reason.
- **Navigation:** `Routes.profile` is `/profile`, and `Routes.signedInRoutes = {home, profile}`. `authRedirect`
  keeps a signed-in user inside that set and is still a function of `SessionStatus` and the path alone: there is
  **no gate** forcing a profile before the app can be used (a gate would put profile state into the session and
  the redirect, with a fetch on every start and an offline rule; left until matching needs it). Home pushes the
  profile, so Android back returns to home. The route names nobody: it is always the caller's own profile.
- **One screen for create and edit** (`ProfileScreen(session)`, a plain `StatefulWidget` with ephemeral state as
  in 024). States:
  - *loading:* a labelled spinner;
  - *load failed:* the mapped error and "Try again", with a request id so a late answer is dropped;
  - *no profile:* the empty form under "Create your profile";
  - *loaded:* the form filled in, under "Edit your profile";
  - *saving:* fields disabled and the button busy; `_busy` refuses a second submission;
  - *save failed:* a 422 puts each error on its field and focuses the first, anything else is a banner; the typed
    text is always kept;
  - *saved:* the form shows **the text the server stored** (normalized), with a "Profile saved." notice that goes
    when the user edits again;
  - *session ended* (load or save): nothing is shown, the router is already leaving.
- **What others will see is said before anything is typed:** the form carries a notice that other members will be
  able to see the name and the text, and that the email address stays private (027's public-by-intent fields).
  Nothing is shown to anyone yet.
- **Client validation is only the empty name** (024): no length, character or normalization rule is duplicated,
  and the text is sent exactly as typed. The server's `too_long` and `invalid` come back as field errors whose
  wording carries no numbers. The fields have no length cap either: a long paste is sent whole and the server
  answers `too_long` for its field (027 sizes the body limit for that). Cost, accepted: a user learns that a text
  is too long only on saving; a counter would need the limits in the client.
- **Failure mapping:** `FailurePresentation` gains `displayNameError` and `bioError`, and `presentFailure` maps
  `display_name:required|too_long|invalid` and `bio:too_long|invalid`. `profile_not_found` is listed and maps to
  the generic message; screens never receive it.
- **Widgets:** `AppTextField.text` (one line, name keyboard, capitalized words, no autofill) and
  `AppTextField.multiline` (three lines growing with the text; the keyboard's action key is a line break, so the
  keyboard never submits this form) with previews. The name's action key moves to the bio.
- **Home** shows a "Profile" button above "Log out". It does not load the profile.
- **Tests (host only):** the model and both API calls (strict parsing, the two kinds of 404, exact PUT body); the
  session methods (stale token, 401 → one refresh → one resend with the same body, no retry of other failures,
  signed out); the leak test with marked name and bio (only ever in the body of the save; in no string, exception
  or print); the screen in every state through the real app, including a double tap, a timeout, a session ending
  mid-save and answers arriving after leaving; the redirect table with `/profile`; accessibility (tap targets,
  labels, contrast in light and dark, 2.0 text at 320×480, an open keyboard) and the privacy run for the new
  states.
- **Deferred:** a profile required before using the app; showing the name on home; a confirmation before leaving
  with unsaved edits; character counters; refreshing on resume; anything about other members.
