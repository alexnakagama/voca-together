---
paths:
  - "backend/internal/avatar/**"
  - "backend/internal/server/avatar.go"
  - "backend/internal/server/avatar_test.go"
  - "backend/internal/server/members.go"
  - "backend/internal/db/migrations/00008_avatars.sql"
---

# Profile picture rules (`PUT`/`GET`/`DELETE /v1/me/avatar`, `GET /v1/profiles/{id}/avatar`, backend)

Records: 031 ("The profile picture"), 033 (a picture across a block). The general rules for a member's own
resource and for reading another member are in `backend.md`; the member profile's `has_avatar` is in
`profile.md`. The client side is not built.

## What is stored

- `internal/avatar` owns pictures and imports none of `auth`, `server`, `profile` and `language`.
- **Never store or serve bytes a member uploaded.** The stored picture is produced only from the decoded pixels:
  the centre square, 512×512, over white, JPEG at quality 85, the JPEG's EXIF orientation applied. Re-encoding is
  what removes location, comments, profiles and hidden data: no shortcut may pass an upload through.
- The same upload must always give the same bytes (fixed quality, no timestamp): idempotence and the tests rely on
  it.
- One row per user in `avatars`, keyed by `user_id` and referencing `users`, not `profiles`: a picture can exist
  before a profile. No row means no picture.

## What is accepted

- JPEG and PNG only, recognised by the first bytes. Never read the `Content-Type` header or a file name, and
  decode each format with its own decoder (`image/jpeg`, `image/png`), never through `image.Decode`'s registry.
- Every check that needs no pixel buffer runs first (`inspect`): empty, over 5 MiB, the signature, the header, and
  a side over 4096 px from `DecodeConfig`. Nothing is decoded, queued or queried for an upload these refuse.
- Decoding takes a slot (2 at once, 5 s wait, then `ErrOverloaded` → 503). Never decode outside `withSlot`.
- Errors name the field `avatar` and a code. Never include input bytes or a decoder's message in an error.
- The EXIF reader bounds every offset and treats anything odd as "upright"; it must never panic. Keep the two
  fuzz targets passing when touching `exif.go` or `normalize.go`.

## Saving and removing

- `Save` is idempotent by the SHA-256 of the upload: the same bytes again return the stored picture without
  decoding, writing or logging. The write is one statement that sets the picture and its hash together.
- `Delete` of nothing is not an error. A user that no longer exists is `ErrUserGone` on a save (401).
- Log `avatar: saved` and `avatar: removed` with the `user_id` only, and only when something changed. Never log
  bytes, sizes, dimensions, formats or hashes; the hash is never returned either.

## Routes

- The body of the `PUT` is the raw image, read up to the limit plus one byte. A refused photo is 422
  `validation_failed` on `avatar` (`required`, `too_large`, `unsupported_type`, `invalid_image`,
  `dimensions_too_large`), also when it is too large: not 400.
- A successful `PUT` and both `GET`s answer `image/jpeg`, always that constant, with `Content-Length`; `DELETE`
  answers 204, also when there was no picture; no picture is 404 `avatar_not_found`.
- `PUT` and `DELETE` share `UserLimits.AvatarWrite`. The member picture route is behind `MemberRead`, shared with
  the member profile route.
- The member picture route resolves the id through `memberFor` first (`profile.Service.Public`, then
  `safety.Blocked`): **no profile, no public picture**, and none across a block either. Never look a picture up
  by a public id directly, and never serve one by an unauthenticated link.
- With a block between the reader and the member, in either direction, the answer is 404 `profile_not_found`
  whether or not there is a picture, never `avatar_not_found`: that would say the profile exists (033).
