---
paths:
  - "backend/internal/profile/**"
  - "backend/internal/server/profile.go"
  - "backend/internal/server/profile_test.go"
  - "backend/internal/db/migrations/00005_profiles.sql"
  - "mobile/lib/api/profile.dart"
  - "mobile/lib/session.dart"
  - "mobile/lib/screens/profile_screen.dart"
  - "mobile/test/**/*profile*"
---

# Profile rules (`GET`/`PUT /v1/me/profile`, both sides)

Records: 027 (backend), 028 (client). The general rules for a member's own resource are in `backend.md`.

## Backend

- `internal/profile` owns profiles and imports neither `auth` nor `server`.
- `display_name` and `bio` are public by intent; everything else about the account stays private. Never log either.
- No row means no profile yet: `GET` answers 404 `profile_not_found`. `PUT` is a full replace (an absent or `null`
  `bio` is cleared) and always answers 200.
- All normalization and validation live in `validate.go`; normalizing is idempotent, so sending back what the server
  returned stores the same text. The body cap (64 KiB) is far above the field limits on purpose, so a long paste
  gets 422 `too_long`, not 400.
- One write statement, no transaction (`INSERT … ON CONFLICT DO UPDATE … WHERE the text differs`).

## Client

- `/profile` is always the caller's own: the route names nobody.
- `SessionManager.profile()` returns null only for a 404 whose code is `profile_not_found`; any other 404 stays an
  error, so a broken deployment never shows an empty form.
- The only client check is the empty name. No length, character or normalization rule is duplicated, the text is
  sent as typed, and after a save the form shows the text the server stored.
- The form tells the member, before anything is typed, that other members will be able to see the name and the text.
