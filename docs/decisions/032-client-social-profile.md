# 032: Social profile in the client (profile page, edit screen, picture, member profile)

> **Status:** in force. Implemented and tested on the host. The OpenSpec change `add-social-profile` was archived
> on 2026-10-08 (its specs are synchronized into `openspec/specs/`). Its integration checks (the run on the
> emulator and the two-account check) were removed from the task list and are left for manual execution; no
> result of them is recorded here.
>
> **Backend side:** 031.
>
> **Changes earlier records:** 021 (a route may name a member, by public identifier; one route takes a
> parameter), 023 (`ApiClient` sends a byte body and DELETE and reads an image answer; five new `SessionManager`
> methods), 028 (the form moved to `/profile/edit` and returns to the page on a save; the discard question 028
> deferred exists; "Profile saved." is gone), 030 (the editor is opened from the edit screen; the summary has no
> button and no longer reloads by itself) and 008 (the client adds `image_picker`).
>
> **Changed later:** 034: the member screen is no longer without actions. On another member's profile its app
> bar has a menu ("Report", "Block"), and the screen blocks, undoes that block and opens the report form. To know
> whether the profile is the caller's own it also reads `SessionManager.profile()`, so "it reads only the member
> routes and the catalog" and "the screen can't tell whose profile it shows" no longer hold; the three loads
> fail whole. `/members/<id>` is no longer the only route that names a member: `/members/<id>/report` does too.
> Nothing of the profile itself can be changed from the screen, as before.
>
> **Current rules:** `.claude/rules/profile.md` ("Client"), `.claude/rules/languages.md`, `.claude/rules/mobile.md`.

- **Scope:** the client side of 031. A member's profile becomes a page they look at, an edit screen, a picture and
  a public view. Nothing lists, suggests or searches members; no username, no friends, no deep link, no sharing.
- **Four routes:**

  | Route | Screen | Whose |
  |---|---|---|
  | `/profile` | `ProfileScreen`, the read-only page | the caller's own |
  | `/profile/edit` | `ProfileEditScreen`: name, text, picture, the way in to the languages editor | the caller's own |
  | `/profile/languages` | `LanguagesScreen`, unchanged (030) | the caller's own |
  | `/members/<id>` | `MemberProfileScreen`, read-only | the member whose profile has that public identifier |

  - `Routes.profileEdit` joined `Routes.signedInRoutes`, which stays a set of exact paths.
  - **A route may now name a member (amends 021 and 028).** `Routes.member(id)` builds `/members/<id>`, and
    `authRedirect` lets a signed-in user stay on a path that `Routes.isMember` accepts: `/members/` and a canonical
    lowercase UUID, nothing before or after. It is still a function of the session status and the path alone. A
    malformed identifier is not a route: `/members`, `/members/abc`, an upper-case id and `/members/<id>/x` go home
    like any unknown path, so `MemberProfileScreen` is only ever built with a well-formed id and a mistyped
    location causes no request.
  - *The identifier is the only thing a route says about anyone:* the public id of 031, never `users.id`, a name,
    a language, an email or a token. It is in the location on purpose; it is what the member route is for.
  - *The pattern is written again in `router.dart`,* beside the one in `ApiPaths` (`checkMemberId`). The router
    doesn't import `ApiPaths`, which belongs to `AuthApi`; the two are the same expression, and
    `SessionManager.memberProfile` refuses a non-canonical id before any request, so a drift fails closed.
  - Routes stay flat `GoRoute`s; `/members/:id` is the first with a parameter. Every screen is pushed, so back
    returns to the opener. `go_router_builder` is still not worth it for one parameter (021).
- **The profile page (`/profile`) is read-only (amends 028).** `profile()` decides it: a labelled spinner, a
  failure with "Try again", the empty state ("create your profile", a button that opens the edit screen), or the
  profile. Loaded, it shows `ProfileHeader` (picture or placeholder, name, text), "Edit Profile", the Friends area
  and the languages. No text field and nothing that changes anything.
  - *Three loads, each on its own.* Once the profile has loaded, the picture (`avatar()`) and
    `ProfileLanguagesSection` load by themselves. A picture that fails is the placeholder, with no message; failed
    languages show the section's own error and retry. A member with no profile, or a failed load, causes neither
    request. The cost: the languages wait for the profile, so a slow connection sees two spinners in turn.
  - *Reload on every return from the edit screen,* saved or not: the page awaits the push, drops what it showed and
    loads again, and the languages section is rebuilt with a key per load. For 030's reason: a save whose answer
    was lost may be stored. A failed reload shows the failure, never the profile shown before.
  - *Friends is a placeholder:* a heading and "Coming later". No number, no control, no request.
  - *"See public profile"* is an icon button in the app bar with that tooltip, present only with a loaded profile.
    It pushes `Routes.member(profile.id)`. **No reload on return:** nothing can change on a read-only screen. An
    icon and not a text button: at twice the text size on 320 dp the title and a text action don't fit.
  - The placeholder's initial is the first grapheme cluster of the name (`characters`, which Flutter exports), so
    an emoji or a combined character is not cut.
- **The edit screen (`/profile/edit`, amends 028).** The form of 028 moved as it was: fields, the empty-name
  check, the failure handling, the strings. What changed around it:
  - *A successful save returns to the page* with `context.pop()`. The "Profile saved." notice and the "form shows
    the stored text" state are gone: the page shows what is stored.
  - *Cancel and the discard question,* which 028 deferred. Cancel, the app bar's back and the system's back go
    through one `_requestLeave` with `PopScope`, as in `LanguagesScreen`. "Changed" compares the two fields with
    the loaded text, never a flag. The dialog's two answers are in its scrolling content, not `actions`, which
    overflowed at twice the text size on 320 by 480 dp with the keyboard open.
  - *Save stays available whenever the screen isn't busy.* The languages editor disables Save until something
    changed; copying that here would be a redesign of the form.
  - *A "Languages" row* pushes `Routes.languages`. It awaits the push only to ignore a second tap; nothing is
    loaded on return, since the edit screen holds no language data, and typed text is kept under the editor.
  - *The notice names the picture too.* Both screens keep the app bar title "Your profile".
- **The picture control applies at once.** `ProfileAvatarEditor` (`lib/screens/profile_avatar_editor.dart`) loads
  `avatar()` when the form shows. "Add photo" or "Change photo" calls `PhotoSource.pick()`, then `saveAvatar`, and
  shows the bytes the server returned; "Remove photo" asks first, then `removeAvatar`.
  - *Not with Save:* the picture and the text are two resources and two requests (031). Staging both behind one
    button creates "the name was saved and the picture was not", with no honest way to show it. A picture can be
    set before the first profile save (031), so the create flow offers it too.
  - *Busy starts when the chooser opens,* not when the upload is sent, and the control reports it to the screen,
    which disables everything and holds back leaving. The form can't be saved or left under an open chooser, and
    a chosen photo is never dropped.
  - While the picture loads the control shows a labelled spinner and no button: whether there is a picture decides
    what is offered, so no action can race the load. A picture that fails to load is the placeholder and "Add
    photo", with no message. The placeholder's initial comes from the stored name, not from what is being typed.
  - Failures show under the control through `presentFailure`: `avatarError` for the five `avatar` codes of 031,
    and a new `FailureKind.photoUnusable` for a photo the device couldn't give (`PhotoSourceException`), so the
    control has one path for every message. The server's 400 for an upload cut short is shown as something to
    try again, not as a refusal of the photo. In every case the earlier picture stays.
- **The photo chooser is an interface built in `main`.** `lib/media/photo_source.dart`: `PhotoSource.pick()` →
  `Uint8List?` (null when the member backs out, `PhotoSourceException` when the photo can't be read).
  `lib/media/photo_source_plugin.dart` implements it with `image_picker`: gallery only, `maxWidth` and `maxHeight`
  1024, `imageQuality` 85. `main.dart` builds it and `createRouter` passes it to the edit screen only. Its file is
  the only one that imports `image_picker`; the architecture test enforces it.
  - *The downscale is a transfer optimisation, not a rule:* the server still validates, crops and scales (031).
  - **As built: the system photo picker only on Android 16 and later.** With `image_picker_android` 0.8.13 as it
    comes, earlier versions (13 to 15 included) get the system document chooser (`ACTION_GET_CONTENT`). Using the
    photo picker there means setting `ImagePickerAndroid.useAndroidPhotoPicker`, which needs
    `image_picker_android` and `image_picker_platform_interface` as direct dependencies; they were not approved.
    Neither chooser needs a permission: the merged manifest of a debug build declares no storage, media or camera
    permission.
  - *Lost-data recovery is not adopted:* if Android kills the activity under the chooser, the pick is lost and
    the member chooses again. Nothing was sent.
- **The member screen (`/members/<id>`).** `MemberProfileScreen(session, id)` asks for `memberProfile(id)` and
  `languageCatalog()` together and fails whole, as the languages editor's load does: nothing of the profile is
  shown unless both answered.
  - *`null` is "This profile isn't available",* with no retry: unknown, removed and never saved are one answer
    (031), and asking again wouldn't change it. Every other failure is the mapped message and "Try again".
  - *The picture* is asked for only when the profile says `has_avatar`, by itself; one that fails, also because
    it was removed between the two reads, is the placeholder with no message.
  - *Read-only for everyone,* the member looking at their own included: no edit control, no Friends area, no app
    bar action. The screen can't tell whose profile it shows and doesn't try.
  - *It shares the view, not the data path.* `ProfileHeader` and `ProfileLanguageLists` are what `/profile` and
    `/members/<id>` have in common; the member screen reads everything from the member routes, also for one's own
    id, so "See public profile" shows what the server gives other members and exercises 031's read.
  - The texts that speak to "you" are not reused: the picture is announced as "<name>'s profile picture", and a
    member with no language gets "No languages added yet." (`ProfileLanguageLists` takes that text from its
    caller). The list headings stay "I speak" and "I'm learning": they are the member's voice on their profile.
  - A language code the catalog doesn't name is shown as the code, as on the summary (030).
  - Reached for now only from "See public profile". Each open spends two of the reader's 60 member reads (031).
- **Transport (amends 023).** `ApiClient.send` accepts `DELETE` and an optional byte body, sent as
  `application/octet-stream` and mutually exclusive with `json`. `getImage` and `putForImage` return an image
  answer as bytes: it must be exactly 200 and `image/jpeg`, decided from the headers before anything is buffered,
  and is capped at 1 MiB (twice the stored maximum of 031). Error answers are parsed as before (JSON, 64 KiB), and
  the JSON path is untouched. PUT and DELETE remain for writes the backend makes idempotent, because `_authorized`
  resends once after a 401: the same upload stores nothing twice (031's hash), and a second removal is a 204.
  - *Not `Image.network`:* it would need the access token in a widget, and it uses `dart:io`'s client, which
    bypasses `ApiClient`'s origin, redirect and size rules. Pictures cross the token boundary as `Uint8List`.
  - *No picture cache* (031): each screen load fetches the image again. Accepted for single-profile screens;
    revisit before any list of members.
- **Models and the session layer.** `Profile` gained a required `id`. `lib/api/member_profile.dart`:
  `MemberProfile(id, displayName, bio, hasAvatar, languages)`, strict, its languages parsed by
  `UserLanguages.fromJson` so no language rule is duplicated, `toString` redacted. `ApiPaths.myAvatar`,
  `memberProfile(id)` and `memberAvatar(id)`; the two functions refuse an id that isn't canonical. `AuthApi` and
  `SessionManager` gained `avatar()` → `Uint8List?` (null only for 404 `avatar_not_found`), `saveAvatar(bytes)` →
  `Uint8List`, `removeAvatar()`, `memberProfile(id)` → `MemberProfile?` (null only for 404 `profile_not_found`)
  and `memberAvatar(id)` → `Uint8List?`. Each is one `_authorized` call and caches nothing; any other 404 stays
  an error, as 028 decided. `SessionManager` checks a member id before `_authorized`, so a caller's mistake
  causes no request and no refresh.
- **Privacy.** Nothing about a profile is printed. A screen shows only the profile's own fields and catalog
  names, never text from an error body. The photo's bytes travel only in the body of the member's own upload,
  and a member id only in the path of the two member reads and in the location of the member screen.
- **Accepted risks:**
  - *`image_picker` leaves a copy of the chosen photo in the app's cache directory.* The plugin copies the pick
    there (downscaled) and hands the app a path; the app reads the bytes and doesn't delete the file. It is the
    member's own photo, in the app's private storage, removed by the system under storage pressure or with the
    app's data, and app backup is off. Deleting it would mean `dart:io` file handling in the adapter for a file
    the plugin owns. Revisit if the cache is ever exposed or backed up.
  - *The public profile ships before reporting and blocking* (031's gate). In the app an identifier still
    reaches nobody but its owner.
  - *The reload on every return from the edit screen* costs four small requests and a brief spinner.
  - *An unavailable profile whose catalog request failed* shows the failure and a retry first; the retry then
    says it isn't available. The two loads fail whole on purpose.
- **Tests (host only):** the redirect table with the member pattern for the three statuses (canonical, with a
  query, and every malformed shape) and no redirect loop; the member screen through the real app: every loaded
  shape, the unavailable state, each failure and its retry, a timeout, the session ending during either load,
  answers arriving after leaving, and the route (signed out, malformed ids causing no request, back, logout);
  the page's action (absent without a profile and while loading or failed, the same name, text, picture and
  languages, no reload on return, a double tap); the page, the edit screen and the picture control in every
  state; the accessibility run (tap targets, labels, contrast in both themes, twice the text size at 320 by 480
  dp, an open keyboard) and the privacy run for every new state, including the location of each screen; the
  transport, the models, the five `AuthApi` calls and the five session methods; the architecture test (the new
  model, the new public members, `image_picker` in one file) and the leak test (the bearer on the five new
  paths, image bytes only in the upload, a member id only in a path).
- **Deferred:** opening a member from anywhere but one's own page (Friends, Discover, search), with reporting and
  blocking first (031); deep links to a profile and sharing one; a picture cache and thumbnails; the system photo
  picker on Android 13 to 15; the camera as a source; cropping in the app; deleting the chooser's cached copy;
  showing "member since"; refreshing a screen on resume.
