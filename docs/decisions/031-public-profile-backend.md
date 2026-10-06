# 031: Public profiles (social profile, backend)

> **Status:** draft. Implemented and tested: the public identifier and the member profile read. Approved and not
> built: the profile picture (its table, the image pipeline, its four routes and its limit). This record does
> not describe the picture yet; `has_avatar` is in the response and is always `false` until it is built.
>
> **Changes earlier records:** 018 (a protected read is limited; ten limiters), 027 (the owner's response gains
> `id`; a profile is readable by other members; the public identifier 027 deferred is decided) and 029 (a
> member's languages are read by other members, through `language`).
>
> **Client side:** 032 (not written yet).
>
> **Current rules:** `.claude/rules/profile.md`, `.claude/rules/backend.md` ("Reading another member").

- **Scope:** a signed-in member reads the public profile of any member who has saved one: name, text and
  languages with levels. Nothing lists, suggests or searches members: an identifier is learned only from its
  owner. No username, no friend relationship, no visibility switch.
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
    `UserID`, then `language.Service.Get(ctx, userID)`. `profile` and `language` must not import each other, and
    two primary-key reads need no new package. The `UserID` never leaves the handler. `language.Service.Get`'s
    contract changed from "the authenticated user" to "the user whose languages are read; the caller decides who
    may read them".
  - *Its own response type,* `memberProfileResponse`, not the owner's `profileResponse` with fields left out: a
    shared struct with `omitempty` is how a timestamp leaks later. This type **is** the definition of "public",
    and a test fixes its keys exactly. The owner's timestamps stay the owner's.
  - *`languages` has the shape of `GET /v1/me/languages`,* so a client parses and shows it with what it has. A
    member with no languages gets two empty arrays.
  - *Not one transaction:* the two reads can straddle a save, so one part can be a few milliseconds older than
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
  it and `TestServerOptionsWireEveryRateLimit` covers it. There are now ten limiters.
- **Logs:** a member read logs nothing, found or not: no identifier, name, text or language. A failure inside
  is the usual opaque 500, logged with the route's pattern (`GET /v1/profiles/{id}`), never the path. The
  public identifier joins the list of things never logged, for the owner's requests too.
- **Accepted risks:**
  - **Profiles are readable before reporting, blocking or moderation exist.** Accepted because, until a feature
    lists members, an identifier can be learned only from its owner. This is a **hard gate**: no feature that
    lists, suggests or searches members (Friends, Discover, search) ships before reporting and blocking do.
    Meanwhile, taking a profile down is a manual `DELETE FROM profiles`.
  - *A member with a valid account can read every profile whose id they hold,* at 60 a minute. Ids are
    unguessable and nothing lists them; this becomes real with Discover, which must bring its own limits.
  - *A public id never changes while the profile exists,* so a member cannot shed an id they shared. Rotating it
    would be an `UPDATE` and a new feature; deleting the account removes it.
  - *The 60 and the 1 per second are product guesses:* one constant.
  - *The limit is per process,* like the others (018).
- **Deployment:** migration 00007 adds one column with a default and rewrites `profiles`, which is small; the
  previous binary keeps working against it, so a rollback is a redeploy of the previous binary. The owner's
  responses gain a key and no request changes, so an older client keeps working. The down migration drops every
  public id and is for development.
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
- **Deferred:** the profile picture, in this record when it is built; reporting, blocking and moderation tools
  (the gate above); a username; a "hide my profile" switch; rotating a public id; listing, suggesting or
  searching members and the read model that joins profiles and languages in SQL for it; showing "member since";
  limiting a member's own reads.
