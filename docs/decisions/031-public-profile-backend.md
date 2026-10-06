# 031: Public profiles (social profile, backend)

> **Status:** in force. Implemented and tested: the public identifier, the member profile read and the profile
> picture (its table, the image pipeline, its four routes and its limit). The record was written in two parts,
> the picture second; the part "The profile picture" below is the second.
>
> **Changes earlier records:** 018 (a protected read is limited; a third per-user write limit; eleven limiters;
> a second bounded queue that answers 503), 027 (the owner's response gains `id`; a profile is readable by other
> members; the public identifier 027 deferred is decided), 029 (a member's languages are read by other members,
> through `language`) and 008 (the backend adds `golang.org/x/image`).
>
> **Client side:** 032 (not written yet).
>
> **Current rules:** `.claude/rules/profile.md`, `.claude/rules/avatar.md`, `.claude/rules/backend.md` ("Reading
> another member").

- **Scope:** a signed-in member reads the public profile of any member who has saved one: name, text, languages
  with levels and picture; and a member sets, replaces and removes their own picture. Nothing lists, suggests or
  searches members: an identifier is learned only from its owner. No username, no friend relationship, no
  visibility switch.
- **The visibility model:** a profile is readable by **signed-in members only**, all of them, and by nobody else.
  027 and 029 stored the name, the text and the languages as "public by intent" and the app said so before the
  first save; this is the read they announced. No per-member switch: one would be a second state to test on every
  later feature, and a member who doesn't want to be seen saves no profile.
- **The public identifier is `profiles.public_id`** (migration 00007): `uuid NOT NULL DEFAULT gen_random_uuid()`
  with `profiles_public_id_key UNIQUE`. It is assigned by the database on the first save, never written again
  (`upsertProfile` doesn't name it) and returned to its owner as `id` by `GET` and `PUT /v1/me/profile`.
  - *On `profiles`, not `users`:* a member with no profile then has no public identity by construction, which is
    what makes "no such id" and "no profile yet" one answer without a rule to remember.
  - *Not `users.id`:* 027 kept it private. It is the key the logs carry (`user_id`), so publishing it would let
    anyone holding a profile's id correlate log lines, and it can never be rotated.
  - *A UUID, not a shorter token:* `gen_random_uuid()` is already used, has 122 random bits, needs no generator
    in Go, and its canonical form is a valid path segment for the client as it is.
  - *Not a username:* deferred with discovery, as 027 said. It can be added beside `public_id`.
  - The default is volatile, so the migration rewrites the table and every existing profile gets its own value.
    `updated_at` is not touched: no profile's text changed.
- **An identifier is not text.** `profile.ParsePublicID` accepts only the canonical form PostgreSQL prints: 36
  characters, lowercase hexadecimal, hyphens in place. Nothing is trimmed or lower-cased, as with language codes
  (029), so a profile has exactly one spelling. `profile.Service.Public` parses its argument itself and answers
  `ErrNotFound` for anything else **without a query**: it is the one place that decides what an id names, and no
  caller can hand PostgreSQL a value that isn't a UUID (which would be a 500, and a way to tell "malformed" from
  "unknown").
- **`GET /v1/profiles/{id}`** → 200 `{"id","display_name","bio","has_avatar","languages":{"spoken":[…],
  "learning":[…]}}`, behind `requireAccessToken` and the member-read limit.
  - *Composed in `server`* (`members.go`): `profile.Public` returns the public fields and the owner's internal
    `UserID`, then `language.Service.Get(ctx, userID)` and `avatar.Service.Exists(ctx, userID)` for `has_avatar`.
    `profile`, `language` and `avatar` must not import each other, and three primary-key reads need no new package.
    The `UserID` never leaves the handler. `language.Service.Get`'s contract changed from "the authenticated user"
    to "the user whose languages are read; the caller decides who may read them".
  - *Its own response type,* `memberProfileResponse`, not the owner's `profileResponse` with fields left out: a
    shared struct with `omitempty` is how a timestamp leaks later. This type **is** the definition of "public",
    and a test fixes its keys exactly. The owner's timestamps stay the owner's.
  - *`languages` has the shape of `GET /v1/me/languages`,* so a client parses and shows it with what it has. A
    member with no languages gets two empty arrays.
  - *Not one transaction:* the three reads can straddle a save, so one part can be a few milliseconds older than
    the other until the next load. A snapshot would cost a transaction per view.
  - *A reader needs no profile of their own,* and reading one's own id answers what anyone else gets.
- **One 404.** An id no profile has, a malformed one, an upper-case one, a `users.id` (the reader's, the
  owner's, anybody's) and the id of a profile whose account was deleted all answer 404 `profile_not_found` with
  identical bodies and headers. Without a valid token every one of them, and an existing id, answers the same
  401: the route reveals nothing to someone who is not signed in.
- **A route that names a member is a GET and nothing else.** Ownership stays a property of the routes, not a
  check: every write is under `/v1/me/…` and is selected by the session. Other methods on the member route answer
  405 from the mux, and an identifier in the query or body of an owner route still names nobody (a body carrying
  `id` is an unknown field, 400).
- **Member reads are limited per reader:** `UserLimits.MemberRead`, `user_member_read`, burst 60 then 1 per
  second, keyed by the reader's `UserID` and shared by their sessions. It is the first limit on a protected
  read: 018 and 027 left reads unlimited when a read could only return the caller's own data. Here an account
  can walk other members' data, so the allowance bounds scraping per account while staying far above looking at
  profiles by hand. A miss costs a token too, since walking ids is what it bounds. A member's own GETs stay
  unlimited, and unauthenticated requests spend nothing (the limit runs inside `authn`). `serverOptions` wires
  it and `TestServerOptionsWireEveryRateLimit` covers it. With the picture's write limit there are eleven limiters.
- **The profile picture** (the second part of this record).
  - **`internal/avatar`,** a fourth domain package: `Service` (`Get`, `Exists`, `Save`, `Delete`), the image
    pipeline, its SQL and its typed errors. It imports none of `auth`, `server`, `profile` and `language`, and it
    gets no storage interface until there is a second implementation: the package is the seam.
  - **Stored in PostgreSQL** (migration 00008): `avatars(user_id PK → users ON DELETE CASCADE, image bytea,
    source_sha256 bytea, created_at, updated_at)`, with `avatars_image_size` (1 to 524288 bytes) and
    `avatars_source_sha256_length` (32). Replace and delete are one statement with nothing left behind, the user
    cascade is free, backups already cover it, and it adds no service, secret or SDK; a stored picture is tens of
    kilobytes. *Turned down:* local disk (breaks backups and a second instance) and an object store (right at
    scale; a new secret, orphan cleanup and a non-transactional write now). Moving later changes only this package.
    - *Its own table,* so `profiles` rows stay small for every read that doesn't want the image.
    - *Keyed by `user_id`, not `public_id`:* it is a member's own resource like the others, and the foreign key
      to `users` gives the write the dead-credential 401 the other writes have (`ErrUserGone`).
    - *It references `users`, not `profiles`:* **a picture does not need a profile.** The client can offer the
      picture before the first save and nothing has to be ordered. Visibility is decided at the one place that
      serves other members: no profile, no public id, no public picture.
  - **What is accepted** (`inspect`, before any pixel buffer exists): not empty (`required`); at most 5 MiB
    (`too_large`); a JPEG or a PNG **by its first bytes** (`unsupported_type`): the `Content-Type` header and any
    file name are never read, and each format is decoded by its own standard-library decoder, never through the
    `image` package's registry, so no import elsewhere can widen what is accepted; a header that decodes
    (`invalid_image`); at most 4096 pixels on either side (`dimensions_too_large`). The dimensions come from
    `DecodeConfig` alone: that is the decompression-bomb guard, since a 33-byte PNG can declare any size. A header
    the decoder itself refuses (a size that overflows) is `invalid_image`.
  - **What is stored is always re-encoded** (`render`): decode, keep the centre square, scale to 512×512 with
    `golang.org/x/image/draw` (`CatmullRom`) over white, apply the JPEG's EXIF orientation, encode as JPEG at
    quality 85. Nothing a member uploaded is ever stored or served. Re-encoding is what removes EXIF (GPS above
    all), comments, colour profiles, embedded thumbnails and bytes after the end of the image, and it is the
    defence against a file that is an image and something else.
    - *Orientation is applied on the server.* Re-encoding drops the tag, so a photo whose pixels are sideways
      would be stored sideways. The reader (`exif.go`) is a small hand-written parser of the APP1 segment: the tag
      is one 16-bit value, every offset is bounded, and anything odd is "upright". No EXIF library.
    - *`x/image` is the one new dependency:* the standard library has no quality scaler, and it is of the family
      of `x/crypto` and `x/text`.
    - *No minimum size:* a small image is scaled up and looks soft, which is the member's choice.
    - *Deterministic:* fixed quality, no timestamp, so the same upload always gives the same bytes.
  - **Decode slots:** at most 2 uploads are decoded at once; a request waits up to 5 s for a slot and then gets
    `avatar.ErrOverloaded` → 503 `service_unavailable` with `Retry-After`, the shape of the argon2 queue (018). A
    4096×4096 image takes tens of megabytes while it is processed, so the cap makes the worst case a constant.
    Refused uploads never wait for a slot.
  - **An upload is idempotent by the hash of what was uploaded.** `Save` runs the checks above, then takes the
    SHA-256 of the upload; if the member's row already has that `source_sha256` it returns the stored picture
    without decoding, writing or logging. Otherwise it produces the picture and writes it with one statement,
    `INSERT … ON CONFLICT (user_id) DO UPDATE … WHERE the hash differs`. The client resends a refused request
    once after a 401 and a member retries after a 503 or a lost answer; `backend.md`'s rule is that such a repeat
    changes nothing, and the hash also makes it cost nothing.
    - *Concurrent uploads* by one member take the row lock in turn and the last one wins whole: the picture and
      its hash are written together. Identical concurrent first uploads write once; the others find the hash
      unchanged and answer the picture they produced, which is the stored one because the pipeline is
      deterministic.
    - *The checks run before the hash and the lookup,* so a refused upload costs no query.
    - The hash is of the member's own upload and is never returned or logged.
  - **The routes:**

    | Route | Success | Notes |
    |---|---|---|
    | `PUT /v1/me/avatar` | 200, `image/jpeg`, the stored picture | the body is the raw image, not multipart |
    | `GET /v1/me/avatar` | 200, `image/jpeg` | 404 `avatar_not_found` |
    | `DELETE /v1/me/avatar` | 204 | also when there was none |
    | `GET /v1/profiles/{id}/avatar` | 200, `image/jpeg` | 404 `avatar_not_found`, or `profile_not_found` |

    - *A raw body, not multipart:* one file and no other field; a raw body is bounded by a byte count and needs
      no parser. The handler reads at most the limit plus one byte (`http.MaxBytesReader`), so a longer body is
      recognised without being buffered. A body that can't be read whole is 400 `invalid_request`: the client
      went away, or the upload was too slow and `ReadTimeout` cut it. That 400 says nothing about the photo, so
      the client must offer the same photo again and never show it as a refusal of the picture.
    - *200 with the picture, not 204:* the other saves answer with the resource as stored, and the client then
      shows exactly what the server kept, in one round trip.
    - *Refusals are 422 `validation_failed` on the field `avatar`,* so the app can say what is wrong with the
      photo. **A departure from the convention, on purpose:** an oversized JSON body is 400 `invalid_request`; an
      oversized photo is something a member fixes by choosing another, so it gets a field code, as 027 arranged
      for a long bio.
    - *The member picture route resolves `id` with `profile.Service.Public` first* and then reads by `UserID`, so
      it misses exactly as the member profile route does, and a picture whose owner has no profile is unreachable.
    - *One new error code,* `avatar_not_found`. A user deleted between authentication and an upload gets the 401
      of a dead credential, as on the other writes; a removal for one answers 204, since nothing is left.
    - Every image response is `image/jpeg` with `Content-Length`, `Cache-Control: no-store` (from
      `requireAccessToken`) and `nosniff`. The type is a constant, never taken from a request.
  - **Pictures are served behind the access token,** not by an unguessable link: it follows "signed-in members
    only". The cost, accepted: no HTTP cache, so a picture is fetched each time a screen loads it. A cache keyed
    by a version is the first thing to add when a screen lists many members.
  - **Limits:** `PUT` and `DELETE /v1/me/avatar` share `UserLimits.AvatarWrite`, `user_avatar_write`: burst 5,
    then 1 per minute, lower than the other writes because each accepted upload costs a decode. Refused uploads
    and removals of nothing spend it too. `GET /v1/me/avatar` is unlimited, like a member's other own reads. The
    member picture route shares `MemberRead` with the member profile route, so one view of a member spends two.
    Eleven limiters.
  - **The server's timeouts are unchanged.** `ReadTimeout` and the request deadline stay at 10 s: they are the
    slow-request defence. The app asks the system picker for a downscaled copy, a few hundred kilobytes; the
    5 MiB limit is what the server tolerates, not what the app sends. A full 5 MiB upload needs about 4 Mbit/s to
    finish in time, and a slower one ends as a timeout the member can retry.
  - **Logs:** `avatar: saved` and `avatar: removed` with the `user_id` and nothing else, and only when something
    changed. Never bytes, sizes, dimensions, formats or hashes; validation errors name the field and the code,
    and a decoder's own message is dropped, since it may quote the input.
- **Logs of the member reads:** a member read logs nothing, found or not: no identifier, name, text or language. A
  failure inside is the usual opaque 500, logged with the route's pattern (`GET /v1/profiles/{id}`), never the path. The
  public identifier joins the list of things never logged, for the owner's requests too.
- **Accepted risks:**
  - **Profiles are readable before reporting, blocking or moderation exist.** Accepted because, until a feature
    lists members, an identifier can be learned only from its owner. This is a **hard gate**: no feature that
    lists, suggests or searches members (Friends, Discover, search) ships before reporting and blocking do.
    Meanwhile, taking a profile down is a manual `DELETE FROM profiles`, and a picture a manual
    `DELETE FROM avatars`.
  - *A member with a valid account can read every profile whose id they hold,* at 60 a minute. Ids are
    unguessable and nothing lists them; this becomes real with Discover, which must bring its own limits.
  - *A public id never changes while the profile exists,* so a member cannot shed an id they shared. Rotating it
    would be an `UPDATE` and a new feature; deleting the account removes it.
  - *The 60 and the 1 per second are product guesses:* one constant.
  - *The limit is per process,* like the others (018), and so are the decode slots: each instance decodes two.
  - *An image decoder bug or a hostile file.* Only JPEG and PNG, both decoded by the Go standard library;
    dimensions checked before decoding; two slots; a byte limit; fuzz targets over the pipeline and the EXIF
    reader; the stored output made only from decoded pixels.
  - *A decode is not interrupted by the request deadline:* it is bounded by the size of the image instead. A
    request that times out while decoding has written nothing, and its retry is safe.
  - *No picture cache,* so each screen load fetches the image again: accepted for single-profile screens.
  - *A slow connection cannot upload 5 MiB within the read timeout* (above).
  - *Database size:* at most one 512 KiB row per member, typically a tenth of that.
  - *Quality 85, the two slots and their 5 s, and the avatar allowance are product guesses:* constants.
- **Deployment:** migration 00007 adds one column with a default and rewrites `profiles`, which is small, and
  00008 adds a table; the previous binary keeps working against both, so a rollback is a redeploy of the previous
  binary. The owner's responses gain a key and no request changes, so an older client keeps working. The down
  migrations drop every public id and every picture and are for development.
- **Tests:** the column by constraint name, distinct ids for new and for existing rows, `updated_at` kept, and
  00007 applied and rolled back with profiles present; `ParsePublicID` as a table (upper case, braces, no
  hyphens, 35 and 37 characters, surrounding spaces, moved hyphens); the service on the database (the id assigned
  once and stable across an edit and an unchanged save, `Public` finding a profile, missing an unknown id, a
  `users.id` and a deleted user, no query for a malformed id, nothing logged); over HTTP the exact response and
  its exact keys, a profile with and without languages, one's own id, the latest save showing, every miss
  answering identically, the handler reaching no database for a malformed id (services with no pool), no email
  or account id in any answer, the 401 matrix identical for an existing and an unknown id, 405 for PUT, POST,
  PATCH and DELETE with nothing changed, the handler failing closed without the middleware, the limit (per
  reader, shared by sessions, spent by misses, not by unauthenticated requests, separate from the write buckets,
  leaving the reader's own routes working), no log line for any read, and an opaque 500 logged without the id;
  and that `main` wires the limit.
- **Tests of the picture:** the table by constraint name, one row per user, the cascade, and 00008 applied and
  rolled back with rows present; each refusal as a table over generated fixtures (no image file is checked in),
  the limits at and over their edge, and no error message holding input bytes; the EXIF reader over the eight
  orientations, both byte orders, truncations and self-referencing segments, with a fuzz target; the pipeline
  (the centre square of a landscape, a portrait, a 100×100 and a 4096×4096 input, the eight orientations giving
  one upright picture, transparency turned white, no APP1, COM or ICC segment and no planted marker in the
  output, the same input giving the same bytes), with a fuzz target; the slots (overload after the timeout, a
  context that ends, refusals that never queue, never more decodes than slots); the service on the database (a
  first save, a replace, a replay that decodes nothing and writes no row version, a removal with and without a
  row, a deleted user, 16 concurrent different uploads leaving one upload's picture beside its own hash, 16
  identical ones writing once, a failed save leaving the earlier picture, a refusal reaching no database,
  `ErrOverloaded` with every slot held and nothing written, logs holding the `user_id` only); over HTTP each
  refusal and the declared type ignored, a body over the limit not read past it, identical answers to a repeated
  upload, the removal, the member picture and its misses identical to the profile route's, a picture without a
  profile unreachable until a profile is saved, `has_avatar` following an upload and a removal, 405 on the member
  picture route, the 401 matrix on the four routes with nothing changed, another member's ids in the query
  affecting only the caller, a member's second session, the shared and separate buckets, the overload mapped to
  503 with `Retry-After`, a user deleted before the write, the handlers failing closed, an opaque 500, and the
  exact log lines; and that `main` wires the limit. The decode slots are held in the `avatar` package's tests;
  from `server` they can't be reached, so the 503 is verified there by its mapping.
- **Deferred:** reporting, blocking and moderation tools (the gate above); a username; a "hide my profile"
  switch; rotating a public id; listing, suggesting or searching members and the read model that joins profiles
  and languages in SQL for it; showing "member since"; limiting a member's own reads. For the picture: a cache
  and a version to key it by (before any list of members); thumbnails and other sizes; an object store or a CDN;
  interactive cropping; a minimum size; more formats (WebP, HEIC); a tool to take a picture down.
