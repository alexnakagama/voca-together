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
  - "mobile/lib/screens/profile_screen.dart"
  - "mobile/test/**/*profile*"
---

# Profile rules (`GET`/`PUT /v1/me/profile`, `GET /v1/profiles/{id}`, both sides)

Records: 027 (backend), 028 (client), 031 (the public side, backend; **draft**: the profile picture is approved and
not built). The general rules for a member's own resource and for reading another member are in `backend.md`.

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
  same 404 `profile_not_found`: unknown, malformed, a `users.id`, a member with no profile.
- What is public is exactly `memberProfileResponse` (`id`, `display_name`, `bio`, `has_avatar`, `languages`). Keep
  it a type of its own: never reuse the owner's `profileResponse`, and never add an email, an account id, a
  timestamp or anything about sign-in or sessions. `has_avatar` is always `false` until the picture is built.
- Never log a public id, for the owner's requests or a reader's. A member read logs nothing.
- **Gate:** no feature that lists, suggests or searches members ships before reporting and blocking exist.

## Client

- `/profile` is always the caller's own: the route names nobody.
- `SessionManager.profile()` returns null only for a 404 whose code is `profile_not_found`; any other 404 stays an
  error, so a broken deployment never shows an empty form.
- The only client check is the empty name. No length, character or normalization rule is duplicated, the text is
  sent as typed, and after a save the form shows the text the server stored.
- The form tells the member, before anything is typed, that other members will be able to see the name and the text.
