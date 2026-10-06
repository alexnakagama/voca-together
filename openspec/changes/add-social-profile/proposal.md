# Proposal

## Why

VocaTogether is meant to feel like a social network for language exchange, but a member's profile is a form only
its owner can open: no member can see another, there is no picture, and nothing identifies a member publicly.
Decisions 027 and 029 stored the name, the text and the languages as "public by intent" and deferred the public
side to this moment. This change builds that side, and it is the base Friends and Discover will stand on.

## What Changes

**Product model (approved)**

```
/profile            the member's social identity page, read-only
/profile/edit       edit name, text and picture; the way in to the languages editor
/profile/languages  the existing languages editor, unchanged
/members/:id        another member's profile, read-only
```

**In scope**

- **A public identifier.** Every saved profile gets an opaque, random `public_id`. It is the only thing that
  names a member outside their own session. No username.
- **A member's public profile.** `GET /v1/profiles/{id}`, for signed-in members only: name, text, languages with
  levels, and whether there is a picture. One 404 for "no such id" and "no profile yet". Limited per user.
- **A profile picture.** `PUT`, `GET` and `DELETE /v1/me/avatar`, and `GET /v1/profiles/{id}/avatar`. JPEG or PNG
  by content, up to 5 MiB and 4096 px per side. The server always re-encodes: orientation applied, centre-cropped
  to a square, scaled to 512×512, saved as JPEG with every piece of metadata gone. Stored in PostgreSQL.
- **`/profile` becomes a read-only page**: picture (or initials), name, text, a Friends area, languages, an "Edit
  Profile" button and a "See public profile" action. No editable field.
- **`/profile/edit` holds the existing form**, moved and not redesigned, with the same save contract. New on it:
  the picture control (applied at once, not with Save), the row that opens the languages editor, a Cancel
  button, the "Discard changes?" question, and a return to `/profile` after a successful save.
- **`/members/:id`**, built from the same view as `/profile`, reached for now only from "See public profile"
  with the member's own id, so the public read has a real path in the app and on the emulator.
- **Friends is a placeholder on the member's own page**: a heading and one "Coming later" line. No number, no
  API field, no interaction.
- **Client transport:** `ApiClient` sends a byte body and `DELETE`, and reads an image response.
- Decision records, rules, the architecture map, tests on both sides.

**Out of scope**

- A username or handle; search; friend relationships, requests or counts; Discover, matching, chat,
  notifications.
- Reporting, blocking and moderation tools; a "hide my profile" switch; showing "member since".
- Interactive cropping, filters, several pictures, thumbnails, the camera as a source, object storage, a CDN.
- Deep links to a profile; sharing a profile outside the app.
- Any change to the languages API, its models, or the behavior of the languages editor beyond where it is
  opened from.
- Deleting or exporting an account; a profile required before using the app.
- Committing or pushing.

**Decisions already approved** (reasoning in `design.md`)

1. Opaque `public_id`, not `users.id` and not a username.
2. Public profiles and pictures are readable by signed-in members only; no visibility switch.
3. Public reads ship before reporting and blocking exist. The risk is recorded and is a hard gate on any
   feature that lists or suggests members.
4. Pictures are served through the API behind the access token, not by an unguessable link.
5. A picture is applied when chosen, as its own request; it can be set before a profile is saved, and is shown
   to others only once a profile exists.
6. Gallery only, through the system photo picker.
7. Limits: 5 MiB and 4096 px per side accepted; 512×512 JPEG stored.
8. Two new dependencies: `image_picker` (mobile) and `golang.org/x/image` (backend, for scaling).

## Capabilities

### New Capabilities

- `member-profile`: the public side of a profile: the public identifier, what one signed-in member can read
  about another and what they can never read, how an unknown or unavailable profile answers, and the member
  profile screen at `/members/:id`.
- `profile-avatar`: a member's profile picture: uploading, replacing and removing their own, what input is
  accepted, how it is normalized and stored, and who can read it.
- `profile-page`: the member's own read-only profile page at `/profile`: what it shows, its empty and failed
  states, the Friends placeholder, and how it leads to editing and to the public view.
- `profile-editing`: editing one's own profile at `/profile/edit`: the name and text form and its save, leaving
  with unsaved changes, the picture control, and the way in to the languages editor.

### Modified Capabilities

- `language-editor`: the editor is opened from the edit screen instead of the Languages section of Profile, it
  returns to the screen that opened it, and the profile page shows the result when the member comes back to it.
  The editor's own behavior, its route and its requests do not change.

## Impact

- **API (additive):** five new routes; the owner's `GET`/`PUT /v1/me/profile` responses gain `id`. No existing
  request changes. One new error code, `avatar_not_found`.
- **Database:** migration 00007 adds `profiles.public_id`; migration 00008 adds the `avatars` table.
- **Backend code:** new `internal/avatar`; `internal/profile` (public id, lookup by it); `internal/server` (the
  member and avatar routes, two new per-user limits); `cmd/api/main.go` (wiring); `internal/testutil`.
- **Mobile code:** `lib/api/` (client, paths, models), `lib/session.dart`, `lib/router.dart`, `lib/main.dart`, a
  new picture-source layer, new and moved screens under `lib/screens/`, new widgets and previews under
  `lib/ui/`, strings.
- **Dependencies:** `golang.org/x/image`; `image_picker`. No Android permission is added.
- **Documents:** two new decision records with header notes on 018, 023, 027, 028, 029 and 030;
  `.claude/rules/` (`profile.md`, `languages.md`, `mobile.md`, `backend.md`, `testing.md`, a new `avatar.md`);
  `docs/architecture.md`; `docs/decisions.md`; `CLAUDE.md`.
- **Deployment:** both migrations are additive, so the backend can ship before the client. A client older than
  this change keeps working against the new backend.
