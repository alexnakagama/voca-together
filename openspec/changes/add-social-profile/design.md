# Design

## Context

See `proposal.md` for the motivation and the approved product model. What exists and constrains the approach:

- **Profile (decisions 027, 028).** `profiles(user_id PK, display_name, bio, created_at, updated_at)`;
  `GET`/`PUT /v1/me/profile`; the PUT is a full replace, idempotent, one statement. `internal/profile` imports
  neither `auth` nor `server`. `users.id` was kept private on purpose, and a handle, moderation and blocking were
  deferred "until a profile is shown to another member".
- **Languages (decisions 029, 030).** `internal/language`, `GET`/`PUT /v1/me/languages`, always 200. 029 says
  discovery "will read through this package". `Service.Get(ctx, userID)` already takes the user as an argument.
- **A member's own resource (`.claude/rules/backend.md`).** Selected only by the authenticated `UserID`; no id in
  a route, query or body; saves are full replaces and idempotent, because the client resends after a 401 and
  retries after a 503; each write has its own per-user bucket, wired in `serverOptions`.
- **Server.** Every protected route is wrapped individually in `requireAccessToken`, which sets `no-store`. The
  request context ends after 10 s; `http.Server.ReadTimeout` is 10 s. Responses are JSON only today.
  `securityHeaders` already sends `nosniff` and `default-src 'none'`.
- **Client transport (decision 023).** `ApiClient.send` takes a JSON map, accepts GET, POST and PUT, requires a
  JSON 2xx body and caps every response at 64 KiB. Its path pattern, `^/(healthz|v1(/[a-z0-9-]+)+)$`, already
  admits a lowercase UUID segment. Every protected call goes through `SessionManager._authorized`, one call per
  method, nothing cached. `AuthApi.requestTimeout` is 15 s.
- **Client structure (decisions 021, 024, 028, 030).** `main.dart` builds every long-lived object;
  `Routes.signedInRoutes` is a set of exact paths and `authRedirect` a function of status and path; screens are
  `StatefulWidget`s with ephemeral state, a request id for late answers, and every failure through
  `presentFailure`. `ProfileScreen` holds the form and mounts `ProfileLanguagesSection`, whose "Edit languages"
  button pushes the editor and reloads on return. The languages editor leaves with `context.pop()`.
- **Specs.** `language-editor` is the only main spec. Its entry point and "Profile shows the result"
  requirements describe the layout this change replaces.

## Goals / Non-Goals

**Goals:**

- Ownership stays a property of the routes, not a check: every write is under `/v1/me/…`, and the only routes
  that name a member are GETs.
- One definition of "what is public", in one response type, that Friends and Discover can reuse.
- No image a member uploaded is ever stored or served: only pixels the server re-encoded.
- The token boundary is unchanged: no screen or widget sees a token, also for images.
- The existing form, languages editor, models and widgets are moved or reused, not rewritten.

**Non-Goals:**

- An image cache, on disk or in memory, and HTTP caching of pictures.
- A storage abstraction for a future object store. The `avatar` package is the seam; it gets no interface
  until there is a second implementation.
- A read model that joins profiles and languages in SQL. Discover will need one; one profile at a time doesn't.
- A view model or any object that outlives a screen.

## Decisions

Product decisions 1–8 of the proposal are approved. The decisions below are how they are built; the ones that
change an earlier record say so.

### 1. `public_id` is a column of `profiles`

Migration 00007: `ALTER TABLE profiles ADD COLUMN public_id uuid NOT NULL DEFAULT gen_random_uuid()` with
`CONSTRAINT profiles_public_id_key UNIQUE (public_id)`. The default is volatile, so PostgreSQL rewrites the
table and gives every existing row its own value. The owner's `profileResponse` gains `id`.

- *Why on `profiles`:* a member with no profile then has no public identity by construction, which is what
  makes "no such id" and "no profile yet" one 404 without a rule to remember.
- *Why not `users.id`:* 027 kept it private; it is the key the logs carry (`user_id`), so publishing it would
  let anyone with a profile link correlate log lines; and it can never be rotated.
- *Why a UUID and not a shorter token:* `gen_random_uuid()` is already used, has 122 random bits, needs no
  generator in Go, and its canonical form passes the client's path pattern unchanged.
- *Alternative, a username:* deferred by decision (proposal). It can be added beside `public_id` later.
- `updated_at` is not touched by the migration: the profile's text did not change.

### 2. The member read is composed in `server`

`GET /v1/profiles/{id}` calls `profile.Service.Public(ctx, publicID)`, which returns the public fields and the
owner's internal `UserID`, then `language.Service.Get(ctx, userID)` and `avatar.Service.Exists(ctx, userID)`.
The handler builds `memberProfileResponse`, a type of its own that lists `id`, `display_name`, `bio`,
`has_avatar` and `languages`. The internal `UserID` never leaves the handler.

- *Why in `server`:* `profile`, `language` and `avatar` must not import each other (`backend.md`), and three
  primary-key reads need no new package. The doc comment on `language.Service.Get` ("must be the authenticated
  user") is relaxed to "the user whose languages are read; the caller decides who may read them".
- *Why a separate response type and not `profileResponse` minus fields:* a shared struct with `omitempty` is
  how a timestamp leaks later. Two explicit types make the public contract greppable and testable by exact keys.
- *Not one transaction:* the three reads can straddle a save. The result is a profile that is a few
  milliseconds stale in one part, which the next load corrects; a snapshot would cost a transaction per view.
- *`languages` reuses the JSON shape of `GET /v1/me/languages`,* so the client parses it with
  `UserLanguages.fromJson` and shows it with `LanguageChip`. No language rule or type is duplicated.

### 3. A member identifier is an identifier, not text

The path value is accepted only as a canonical lowercase UUID (36 characters, hyphens in place). Anything else
answers 404 `profile_not_found` without a query, exactly as an unknown id does. Nothing is trimmed or
lower-cased, as with language codes (029). The parse lives in `profile` (`ParsePublicID`).

### 4. Member reads get a per-user limit (amends 018, 027)

`UserLimits.MemberRead`, `user_member_read`: burst 60, then 1 per second, keyed by the reader, shared by the
profile and the picture route of a member (one view spends two). It is the first limit on a protected read:
018 and 027 left reads unlimited when they could only return the caller's own data. Here a reader can walk
other members' data, so the allowance bounds scraping per account. The owner's own GETs stay unlimited.
`serverOptions` wires it and `TestServerOptionsWireEveryRateLimit` covers it.

### 5. `internal/avatar` and the `avatars` table

A fourth domain package, `internal/avatar` (`Service.Get`, `Exists`, `Save`, `Delete`; the image pipeline; SQL;
typed errors), importing none of `auth`, `server`, `profile`, `language`. Migration 00008:

```
avatars(
  user_id       uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
  image         bytea NOT NULL,        -- the normalized JPEG
  source_sha256 bytea NOT NULL,        -- of the bytes uploaded; decision 7
  created_at, updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (octet_length(image) BETWEEN 1 AND 524288),
  CHECK (octet_length(source_sha256) = 32)
)
```

- *Why PostgreSQL:* replace and delete are one statement with nothing left behind, the user cascade is free,
  backups already cover it, and it adds no service, secret or SDK. A 512×512 JPEG is tens of kilobytes; ten
  thousand members are well under a gigabyte. *Alternatives:* local disk (breaks backups and a second
  instance); an object store (right at scale, but a new secret, orphan cleanup and a non-transactional write
  now). Moving later changes only this package.
- *Why its own table:* `profiles` rows stay small for every read that doesn't want the image, and a picture
  can exist without a profile (decision 10).
- *Why keyed by `user_id`, not `public_id`:* it is a member's own resource like the others, and the foreign key
  to `users` gives the dead-credential 401 the other writes have (`ErrUserGone`).
- `testutil.DB` adds `avatars` to its `TRUNCATE`.

### 6. The image pipeline

`Save(ctx, userID, raw []byte)`:

1. Empty → `required`. Longer than 5 MiB → `too_large` (the handler reads at most the limit plus one byte, so
   an oversized body is detected without being buffered whole).
2. Sniff the first bytes: JPEG (`FF D8 FF`) or PNG (the 8-byte signature), else `unsupported_type`. The
   `Content-Type` header is never read.
3. `image.DecodeConfig`: either side over 4096 → `dimensions_too_large`; a failure → `invalid_image`. This is
   the decompression-bomb guard: it runs before any pixel buffer is allocated.
4. Take a decode slot (below), then `image.Decode`; a failure → `invalid_image`.
5. For a JPEG, read the EXIF orientation (values 1–8) and apply it. A missing or unreadable tag is "upright".
6. Crop the centre square, scale to 512×512, draw over white, encode as JPEG at quality 85.

- *Always re-encode:* it is what removes EXIF (GPS above all), comments, colour profiles, embedded thumbnails
  and any bytes after the end of the image, and it is the defence against a file that is both an image and
  something else. The result is produced only from decoded pixels.
- *Orientation is applied on the server,* not assumed from the client. Re-encoding drops the tag, so a photo
  whose pixels are sideways would otherwise be stored sideways; Android's picker keeps the tag when it
  resizes. The reader is a small hand-written parser of the JPEG APP1 segment (the tag is one 16-bit value);
  it bounds every offset and treats anything odd as "upright". No EXIF library is added.
- *Scaling uses `golang.org/x/image/draw`* (`CatmullRom`), the approved dependency, from the same family as
  `x/crypto` and `x/text`. The standard library has no quality scaler. *No minimum size:* a small image is
  scaled up and looks soft, which is the member's choice; a minimum would be one more rule and code.
- *Decode slots:* at most 2 images are decoded at once, and a request waits up to 5 s for a slot, then
  `avatar.ErrOverloaded` → 503 with `Retry-After`, the shape of the argon2 queue (018). A 4096×4096 image
  takes tens of megabytes while it is processed; the cap makes the worst case a constant.
- *Deterministic:* the same upload always yields the same stored bytes (fixed quality, no timestamp), which
  the tests rely on.

### 7. Uploads are idempotent by the hash of what was uploaded

`Save` computes the SHA-256 of the upload first. If the member's row already has that `source_sha256`, it
returns the stored image without decoding or writing, and logs nothing. Otherwise it processes and writes with
one statement: `INSERT … ON CONFLICT (user_id) DO UPDATE …`. Concurrent uploads by one member are applied in
turn by the row lock and the last one wins whole, as in 027.

- *Why:* `_authorized` resends a refused request once after a 401, and a member retries after a 503 or a lost
  answer. The rule in `backend.md` is that such a repeat changes nothing; the hash also makes it cost nothing.
- The hash is of the member's own upload and is never returned or logged.
- *As built:* the checks of decision 6 that need no pixel buffer (steps 1 to 3) run before the hash and the
  lookup, so a refused upload costs no query. The hash is still computed before anything is decoded.

### 8. The avatar routes

| Route | Success | Notes |
|---|---|---|
| `PUT /v1/me/avatar` | 200, `image/jpeg`, the stored picture | the body is the raw image, not multipart |
| `GET /v1/me/avatar` | 200, `image/jpeg` | 404 `avatar_not_found` |
| `DELETE /v1/me/avatar` | 204 | also when there was none |
| `GET /v1/profiles/{id}/avatar` | 200, `image/jpeg` | 404 `avatar_not_found` or `profile_not_found` |

- *Raw body, not multipart:* one file and no other field; a raw body is bounded by a byte count and needs no
  parser. *200 with the image, not 204:* the other saves answer with the resource as stored, and the client
  then shows exactly what the server kept, in one round trip.
- *Refusals are 422 `validation_failed` on the field `avatar`* (`required`, `too_large`, `unsupported_type`,
  `invalid_image`, `dimensions_too_large`), so the app can say what is wrong with the photo.
  `writeServiceError` maps `avatar.ValidationError` through the existing conversion. *Departure from the
  convention, on purpose:* an oversized JSON body is 400 `invalid_request`; an oversized photo is something a
  member can fix by choosing another, so it gets a field code, as 027 arranged for a long bio.
- `PUT` and `DELETE` share `UserLimits.AvatarWrite`, `user_avatar_write`: burst 5, then 1 per minute. Lower
  than the other writes because each accepted upload costs a decode.
- The member picture route resolves `id` with `profile.Service.Public` first, so a picture whose owner has no
  profile is unreachable, and then reads by `UserID`.
- Every response keeps `Cache-Control: no-store` (set by `requireAccessToken`) and `nosniff`.
- One new error code, `avatar_not_found`. A user deleted between authentication and an upload gets the 401 of
  a dead credential, as on the other writes. A removal for such a user answers 204: nothing is left to remove.
- A body that can't be read whole (the client went away, or `ReadTimeout` cut a slow upload) answers 400
  `invalid_request`. It says nothing about the photo, so the client treats it as something to try again.
- Logs: `avatar: saved` and `avatar: removed` with `user_id` only, and only when something changed. Never
  bytes, sizes, dimensions, formats or hashes.

### 9. Pictures are served behind the access token (approved)

It follows "signed-in members only". The cost, accepted: no HTTP cache, so a picture is fetched each time a
screen loads it. A cache keyed by a version is the first thing to add when a screen lists many members; it is
not built now.

### 10. A picture does not need a profile (approved)

`avatars` references `users`, not `profiles`, like `user_languages`. The create flow can therefore offer the
picture control before the first save, and nothing has to be ordered. Visibility is decided at the one place
that serves other members: no profile, no public picture.

### 11. Server timeouts are not changed

`ReadTimeout` (10 s) and the request deadline (10 s) stay; they are the slow-request defence. The app always
asks the system picker for a downscaled copy (decision 16), typically a few hundred kilobytes, which fits
easily. The 5 MiB limit is what the server tolerates, not what the app sends. A full 5 MiB upload needs about
4 Mbit/s to finish in time; a slower one ends as a timeout, or as the 400 of decision 8 if the server cut the
body first, and the member can retry either. Accepted, and recorded.

### 12. `ApiClient` gains bytes in, bytes out and DELETE (amends 023)

- `send` accepts `DELETE`, and an optional `bytes` body (mutually exclusive with `json`) sent as
  `application/octet-stream`. PUT and DELETE remain for idempotent writes only.
- A second method, `getImage(path, bearer, timeout)`, and an `expectImage` variant for the PUT's answer: a 2xx
  must be `image/jpeg`, is capped at 1 MiB (twice the stored maximum) and is returned as bytes; anything else
  is the existing `ApiProtocolException`. Error responses are parsed as today (JSON, 64 KiB).
- The JSON path is untouched: same cap, same checks.
- *As built:* the two image methods are `getImage` and `putForImage`. An image answer must be exactly 200, not
  any 2xx (the only success the avatar routes have), so they return plain bytes; the type and the status are
  decided from the headers, before anything is buffered up to the image cap.
- *Why not `Image.network`:* it would need the access token in a widget, and it uses `dart:io`'s client, which
  bypasses `ApiClient`'s origin, redirect and size rules.

### 13. Models and the session layer

- `Profile` gains `id` (required, strict). `lib/api/member_profile.dart`: `MemberProfile(id, displayName, bio,
  hasAvatar, languages)`, strict `fromJson`, `languages` parsed by `UserLanguages.fromJson`, redacted
  `toString`. Image data crosses the boundary as `Uint8List`.
- `ApiPaths`: `myAvatar`, and two functions, `memberProfile(id)` and `memberAvatar(id)`, which refuse an id
  that is not a canonical UUID.
- `AuthApi` and `SessionManager` gain, each one `_authorized` call and nothing cached:
  `avatar()` → `Uint8List?` (null only for 404 `avatar_not_found`), `saveAvatar(Uint8List)` → `Uint8List`,
  `removeAvatar()` → `void`, `memberProfile(id)` → `MemberProfile?` (null only for 404 `profile_not_found`),
  `memberAvatar(id)` → `Uint8List?`. Any other 404 stays an error, as 028 decided for the profile.
  *As built:* `SessionManager` refuses a non-canonical id before `_authorized`, so a caller's mistake causes
  no request and no refresh; `AuthApi` keeps its own check.
- `presentFailure` gains `avatarError` and the five `avatar` codes.
- `test/architecture_test.dart`: the new model on the screens' allowlist and the new public members listed.
  `test/leak_test.dart`: image bytes only in the body of the upload; the bearer on the five new paths.

### 14. The photo chooser is an interface built in `main`

`lib/media/photo_source.dart`: `abstract interface class PhotoSource { Future<Uint8List?> pick(); }` (null when
the member backs out; a `PhotoSourceException` when the photo can't be read).
`lib/media/photo_source_plugin.dart` implements it with `image_picker` (gallery, `maxWidth` and `maxHeight`
1024, `imageQuality` 85). `main.dart` builds it and it is passed by constructor to the edit screen only, as
`GoogleIdentity` is passed to the session. Tests use a fake. The plugin's file is the only one that imports
`image_picker`, enforced by the architecture test.

- On Android 13 and later this is the system photo picker; before that, the system document chooser. Neither
  needs a manifest permission.
- The downscale is a transfer optimisation, not a rule: the server still crops, scales and validates.
- *As built:* with `image_picker_android` 0.8.13 as it comes, the system photo picker is used on Android 16
  and later, and the system document chooser (`ACTION_GET_CONTENT`) on every earlier version, 13 to 15
  included. Using the photo picker there means setting `ImagePickerAndroid.useAndroidPhotoPicker`, which needs
  `image_picker_android` and `image_picker_platform_interface` as direct dependencies; they were not approved,
  so the default stays. Neither chooser needs a permission: the merged manifest of a debug build declares no
  storage, media or camera permission.
- *As built:* `VocaTogetherApp` and `createRouter` take the source, so the harness can pass a fake; no screen
  receives it until the edit screen has its picture control. `PluginPhotoSource` takes an optional
  `ImagePicker`, which is how its test scripts the plugin without a platform-interface dependency.

### 15. Routes and `authRedirect`

`Routes.profileEdit = '/profile/edit'` joins `signedInRoutes`. `Routes.member(id)` builds `/members/<id>`, and
`authRedirect` accepts, for a signed-in user, a path that matches `^/members/<canonical uuid>$`. It stays a
function of the status and the path alone; a malformed member path goes home like any unknown path. The
comment "no route names another member" is replaced. Routes stay flat `GoRoute`s; screens are pushed, so back
returns to the opener.

### 16. Screens and files

| File | Becomes |
|---|---|
| `lib/screens/profile_screen.dart` | `ProfileScreen`, the read-only page at `/profile` |
| `lib/screens/profile_edit_screen.dart` (new) | `ProfileEditScreen`: today's form, moved, plus what decision 18 adds |
| `lib/screens/profile_avatar_editor.dart` (new) | the picture control of the edit screen, with its own state |
| `lib/screens/member_profile_screen.dart` (new) | `MemberProfileScreen` at `/members/:id` |
| `lib/screens/profile_languages_section.dart` | loses its button and its reload-on-return; otherwise as it is |
| `lib/screens/profile_language_lists.dart` (new) | the two chip lists, taken out of the section so the member screen shows the same |
| `lib/ui/widgets/profile_avatar.dart` (new) | picture from bytes, or the initial; pure UI, with previews |
| `lib/ui/widgets/profile_header.dart` (new) | avatar, name, text; pure UI, with previews |

The form's fields, validation, failure handling and strings move as they are. `ProfileHeader` and
`ProfileLanguageLists` are what `/profile` and `/members/:id` share; the page adds the Friends area, "Edit
Profile" and the app bar action around them.

*As built:* `ProfileLanguageLists` takes a loaded selection and the catalog's names and also shows the "none
chosen" text, so the member screen gets that state from the same place. `ProfileAvatar` and `ProfileHeader`
take the screen reader's label as a string, like every other text of a widget; with an empty name (the edit
screen before a profile exists) the placeholder is an icon.

### 17. The profile page loads three things, each on its own

`profile()` decides the page: loading, failed with retry, no profile (the empty state), or loaded. Once loaded,
the picture (`avatar()`) and `ProfileLanguagesSection` load by themselves. A failed picture shows the
placeholder with no message; failed languages show the section's own error and retry, as today.

- *Reload on return:* the page awaits `context.push(Routes.profileEdit)`, then drops what it showed, bumps a
  generation and loads again. The languages section is rebuilt with `ValueKey(generation)`, which replaces its
  private reload-on-return. Always, saved or not, for 030's reason: a save whose answer was lost may be stored.
- *"See public profile"* is an app bar action, present only with a loaded profile, and pushes
  `Routes.member(profile.id)`. No reload on return from it: nothing can have changed there.
- The initial is the first grapheme cluster of the name (`characters`, which Flutter already exports), so an
  emoji or a combined character is not cut in half.
- *As built (group 7):* "Edit Profile" sits directly under the header, above the Friends area. The languages
  section is mounted only once the profile has loaded, so a member with no profile, or a failed load, causes
  no languages request; the cost is that the languages wait for the profile, so a slow connection sees the
  two spinners one after the other. While the page loads it has no control, so a newer load can never start while one is
  pending: the request id only guards the answer that arrives after the page was left.

### 18. The edit screen: what changes around the moved form (amends 028)

- *Success returns to `/profile`* with `context.pop()`. The "Profile saved." notice and the "form shows the
  stored text" state of 028 are removed: the page shows the stored profile instead.
- *Cancel and the discard question:* Cancel, the app bar back and the system back go through one
  `_requestLeave`, with `PopScope`, as in `LanguagesScreen`. "Changed" is a comparison of the two fields with
  the loaded text, never a flag. 028 had deferred this question.
- *Save stays always available* when not busy, as today. The languages editor disables Save until something
  changed; copying that here would be a redesign of the form, which is out of scope.
- *"Languages"* is a row below the fields that pushes `Routes.languages` and awaits nothing: the edit screen
  holds no language data, so there is nothing to refresh on return.
- *The visibility notice* gains the picture ("your name, your picture and this text").
- *As built:* the "Languages" row does await the push, only to ignore a second tap until the editor is left
  (the guard the summary's button had); nothing is loaded on return. The discard dialog puts its two answers
  inside the scrolling content instead of `actions`: at twice the text size on 320 by 480 dp with the keyboard
  open, `actions` overflowed. The languages editor's dialog is unchanged. Both screens keep the app bar title
  "Your profile".

### 19. The picture control applies at once (approved)

`ProfileAvatarEditor` loads `avatar()` when the edit screen shows its form. "Add photo" or "Change photo" calls
`PhotoSource.pick()`, then `saveAvatar`, then shows the bytes the server returned. "Remove photo" asks first,
then `removeAvatar`. Failures show under the control through `presentFailure`. While an action runs, the
editor reports "busy" to the screen, which disables everything and holds back leaving, like a save.

- *Why not with Save:* the picture and the text are two resources and two requests. Staging both behind one
  button creates the state "the name was saved and the picture was not", with no honest way to show it.

### 20. The member screen

`MemberProfileScreen(session, id)` requests `memberProfile(id)` and `languageCatalog()` together and fails
whole, as the languages editor's load does. `null` is the "isn't available" state, with no retry. When
`hasAvatar`, `memberAvatar(id)` loads on its own and falls back to the placeholder. No Friends area, no edit
control, also when the id is the member's own.

### 21. Strings

New, each with an `@` description: the page's empty state and its button; "Edit Profile"; "See public profile";
"Friends" and "Coming later"; "Languages" (the row); "Add photo", "Change photo", "Remove photo", the removal
question and its two answers; the five photo messages and "couldn't use that photo"; the discard question for
the profile (reusing the editor's "Keep editing" and "Discard"); "This profile isn't available"; the semantic
labels of the picture and the placeholder. Removed: "Profile saved." and "Edit languages" on Profile. The
notice is reworded (decision 18). Messages carry no number, as 028 decided.

### 22. Documents

- **Decision 031**, backend: the public id, the member read, the visibility model and its accepted risks, the
  avatar pipeline, storage and limits. Header notes on 018 (a limited read; eleven limiters), 027 (`id` in the
  owner's response; a profile is now readable by members; the deferred identifier decided) and 029 (a member's
  languages are read by others through `language`).
- **Decision 032**, client: the four routes, the page and edit split, the picture control, the member screen,
  the transport change. Header notes on 021 (a route may name a member by public id), 023 (bytes, DELETE, the
  new session methods), 028 (the form moved; return on save; the discard question) and 030 (the editor's entry
  point; the summary no longer reloads by itself).
- **Rules:** `profile.md` (both sides, with the new files in `paths`), a new `avatar.md`, `languages.md` (the
  summary and the entry point), `mobile.md` (routes, transport, `PhotoSource`), `backend.md` (the member-read
  pattern beside "a member's own resource"), `testing.md` (the `TRUNCATE` list).
- **Map and index:** `docs/architecture.md`, `docs/decisions.md`, and the table and boundaries in `CLAUDE.md`.

## Risks / Trade-offs

- [Profiles and pictures become readable before reporting, blocking or moderation exist] → Approved. Until a
  feature lists members, an id can only be learned by its owner. Decision 031 records it as a hard gate: no
  Friends, Discover or search ships before reporting and blocking do. Removing a picture meanwhile is a manual
  `DELETE FROM avatars`.
- [An image decoder bug or a hostile file] → Only JPEG and PNG, both decoded by the Go standard library;
  dimensions checked before decoding; two decode slots; a byte limit; a fuzz test over the pipeline and the
  EXIF reader; the stored output comes only from decoded pixels.
- [Memory under load] → Bounded by the slots: two decodes of 4096×4096 at once is the worst case, and the
  third waits or gets a 503.
- [A member with a valid account scrapes profiles] → Ids are unguessable and nothing lists them; reads are
  limited per account. Real once Discover exists, which must bring its own limits.
- [No picture cache: each screen load fetches the image again] → Accepted for single-profile screens (decision
  9). Revisit before any list of members.
- [The reload on every return costs four small requests and a brief spinner] → Accepted, as 030 accepted it
  for the summary; it is what guarantees the page shows what is stored.
- [`ProfileScreen` changes meaning: tests and documents that say "the profile form" must follow it] → The
  screen tests are split with the screens, and a search for the old wording is a task.
- [The photo chooser returns after Android killed the activity] → The pick is lost and the member chooses
  again; nothing was sent. `image_picker`'s lost-data recovery is not adopted.
- [A sheet or dialog open when the session ends] → Same handling and tests as the languages editor's.
- [An older app build against the new backend] → Works: the new `id` is an extra key that its parser ignores,
  and no request changed.
- [Database size] → Bounded by one 512 KiB row per member at most, typically a tenth of that.

## Migration Plan

1. Deploy the backend. Migrations 00007 and 00008 are additive: a column with a default, and a new table. The
   previous binary keeps working against them, so a rollback is a redeploy of the previous binary; goose
   leaves the new objects in place and nothing reads them.
2. Release the app. It requires `id` in the profile response, so it must not ship before step 1.
3. The down migrations drop every picture and every public id; they are for development only.

## Open Questions

None that change the specs or the tasks. Constants that are product guesses and can be tuned without a
contract change: JPEG quality 85, the two decode slots and their 5 s wait, the `MemberRead` and `AvatarWrite`
allowances, the picker's 1024 px downscale.
