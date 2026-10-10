---
paths:
  - "backend/internal/profile/**"
  - "backend/internal/server/profile.go"
  - "backend/internal/server/profile_test.go"
  - "backend/internal/server/members.go"
  - "backend/internal/server/members_test.go"
  - "backend/internal/db/migrations/00005_profiles.sql"
  - "backend/internal/db/migrations/00007_profile_public_id.sql"
  - "mobile/lib/api/profile.dart"
  - "mobile/lib/session.dart"
  - "mobile/lib/api/member_profile.dart"
  - "mobile/lib/screens/profile_screen.dart"
  - "mobile/lib/screens/profile_edit_screen.dart"
  - "mobile/lib/screens/profile_avatar_editor.dart"
  - "mobile/lib/screens/member_profile_screen.dart"
  - "mobile/lib/ui/widgets/profile_*.dart"
  - "mobile/lib/media/**"
  - "mobile/test/**/*member*"
  - "mobile/test/media/**"
  - "mobile/test/**/*profile*"
---

# Profile rules (`GET`/`PUT /v1/me/profile`, `GET /v1/profiles/{id}`, both sides)

Records: 027 (backend), 028 (client, the form), 031 (the public side, backend), 032 (client: the page, the edit
screen, the picture, the member screen). The general rules for a member's own resource and for reading another
member are in `backend.md`; the backend rules of the profile picture are in `avatar.md`.

## Backend

- `internal/profile` owns profiles and imports neither `auth` nor `server`.
- `display_name` and `bio` are public by intent; everything else about the account stays private. Never log either.
- No row means no profile yet: `GET` answers 404 `profile_not_found`. `PUT` is a full replace (an absent or `null`
  `bio` is cleared) and always answers 200.
- All normalization and validation live in `validate.go`; normalizing is idempotent, so sending back what the server
  returned stores the same text. The body cap (64 KiB) is far above the field limits on purpose, so a long paste
  gets 422 `too_long`, not 400.
- One write statement, no transaction (`INSERT … ON CONFLICT DO UPDATE … WHERE the text differs`).

## Backend: the public side (031)

- A profile is named to other members only by `profiles.public_id`, returned to its owner as `id`. The database
  assigns it on the first save; nothing ever writes it again. `users.id` stays private: never return it, and never
  accept it where a public id is expected.
- A public id is an identifier, not text: only the canonical lowercase UUID is one (`profile.ParsePublicID`), never
  trimmed or lower-cased. `profile.Service.Public` decides what an id names and makes no query for a malformed
  one; handlers pass the path value as it came and don't parse it themselves.
- `GET /v1/profiles/{id}` is for signed-in members only. Everything that is not the id of a saved profile is the
  same 404 `profile_not_found`: unknown, malformed, a `users.id`, a member with no profile. So is a profile with
  a block between its owner and the reader, in either direction (033): the handler resolves the id through
  `memberFor`, never `Public` alone, and a member reading their own id is never hidden.
- `profile.Service.PublicByUsers` returns the public profiles of several users by **internal** user id, for
  `server` to compose a list with (the blocked members). Its ids come from the server's own data, never from a
  request, and it returns nothing for a user without a profile.
- What is public is exactly `memberProfileResponse` (`id`, `display_name`, `bio`, `has_avatar`, `languages`). Keep
  it a type of its own: never reuse the owner's `profileResponse`, and never add an email, an account id, a
  timestamp or anything about sign-in or sessions. `has_avatar` comes from `avatar.Service.Exists`, read with the
  owner's `UserID` like the languages.
- Never log a public id, for the owner's requests or a reader's. A member read logs nothing.
- **Gate:** no feature that lists, suggests or searches members ships, and no public release, until the three
  conditions of 033 hold (`safety.md`, "The gate"). Blocking alone does not lift it.

## Client: the page and the edit screen (032, 028)

- `/profile` (`ProfileScreen`) is the caller's own, read-only page: no text field and nothing that changes the
  profile, the picture or the languages. Everything is edited at `/profile/edit` (`ProfileEditScreen`). Neither
  route names anybody.
- `SessionManager.profile()` returns null only for a 404 whose code is `profile_not_found`; any other 404 stays an
  error, so a broken deployment never shows the empty state or an empty form.
- The page shows only what the server returned. The picture and the languages are asked for once the profile has
  loaded and each loads by itself: a failed picture is the placeholder with no message, failed languages show the
  section's own error. A member with no profile, or a failed load, causes neither request.
- Each time the member comes back from the edit screen, saved or not, the page drops what it showed and loads
  everything again (a save whose answer was lost may be stored). A failed reload shows the failure, never the
  earlier profile.
- Friends is a heading and one line: no number, no control, no request.
- "See public profile" is shown only with a loaded profile and pushes `Routes.member(profile.id)`. Coming back
  from it reloads nothing.
- The form: the only client check is the empty name. No length, character or normalization rule is duplicated
  and the text is sent as typed. It tells the member, before anything is typed, that other members will see the
  name, the picture and the text. A successful save leaves with `context.pop()`; there is no "saved" state.
- Cancel, the app bar's back and the system's back all go through `_requestLeave`: at once when nothing changed,
  after the discard question otherwise, not at all while the screen is busy. "Changed" is the two fields
  differing from the loaded text, never a flag.
- The edit screen holds no language data: its "Languages" row only pushes `Routes.languages`.

## Client: the picture (032)

- The picture is its own resource: `ProfileAvatarEditor` applies a chosen photo at once (`saveAvatar`) and never
  through the form's Save, and no picture action sends a profile PUT. It shows the bytes the server returned.
- The control is busy from the moment the chooser opens until the answer, and reports it to the screen, which
  disables everything and holds back leaving. While the picture loads it offers no button.
- Every failure goes through `presentFailure` (`avatarError`, `FailureKind.photoUnusable`) and leaves the earlier
  picture. A picture that fails to load is the placeholder, with no message.
- A photo comes only from `PhotoSource` (`lib/media/`), built in `main` and passed to the edit screen only.
  `photo_source_plugin.dart` is the only file that imports `image_picker` (the architecture test enforces it):
  gallery only, never the camera, and no Android permission. Its downscale is a transfer optimisation; no rule
  of the server about a picture is repeated in the client.
- Pictures cross the token boundary as `Uint8List` from `SessionManager`. Never `Image.network`: it would need
  the token in a widget and bypasses `ApiClient`. Nothing caches a picture.
- Never print or show as text anything of a photo, and send its bytes only in the body of the upload.

## Client: a member's profile (032)

- `/members/<id>` (`MemberProfileScreen`) is the only route that names a member, and the public identifier is
  the only thing it carries: never `users.id`, a name, a language, an email or a token. `Routes.isMember` accepts
  only the canonical lowercase UUID; anything else goes home through `authRedirect`, with no request.
- The screen is read-only for everyone, the member looking at their own profile included: no edit control, no
  Friends area, no app bar action. It reads only the member routes (`memberProfile`, `memberAvatar`) and the
  catalog, never the caller's own profile, languages or picture.
- `memberProfile()` returns null only for a 404 `profile_not_found`, shown as "isn't available" with **no
  retry**. Every other failure is the mapped message with "Try again".
- The profile and the catalog load together and fail whole. The picture is asked for only when `hasAvatar`, by
  itself, and a failure is the placeholder with no message.
- `MemberProfile` is everything one member may read about another; `toString` is redacted. Don't add a field
  the backend's `memberProfileResponse` doesn't have.
- `ProfileHeader` and `ProfileLanguageLists` are shared with the page. Texts that speak to "you" are not: the
  member screen has its own picture labels (with the name) and its own "no languages" text.
