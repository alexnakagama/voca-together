---
paths:
  - "backend/internal/safety/**"
  - "backend/internal/server/safety.go"
  - "backend/internal/server/safety_test.go"
  - "backend/internal/server/members.go"
  - "backend/internal/db/migrations/00009_blocks.sql"
---

# Safety rules: blocking (`PUT`/`DELETE /v1/me/blocks/{id}`, `GET /v1/me/blocks`, backend)

Record: 033 (draft: blocking is built; reporting is approved and **not implemented**, see the last section). The
general rules for a member's own resource and for reading another member are in `backend.md`. The client side is
not built.

## The package

- `internal/safety` owns blocks and imports none of `auth`, `server`, `profile`, `language` and `avatar`.
- It names members only by internal user id. A public id is resolved in `server` (`profile.Service.Public`) before
  the call and never passed in, so `safety` cannot store, return or log one.
- A block is one row per direction in `blocks`, keyed by `users.id`: it survives the blocked member's profile
  being removed and saved again, and it goes when either account does.

## What a block means

- `safety.Blocked(a, b)` is the one question, with the same answer in both orders: a block made by either member
  hides each from the other.
- **Every route through which one member reads, finds, lists or contacts another asks `Blocked`,** or filters
  by `blocks` in both directions in its query. The answer for a blocked pair is whatever "that member does not
  exist" is on that surface, never a message about a block. 033 says what friend requests, discovery, search and
  chat must do.
- Today that is the two member reads: both go through `memberFor` in `server/members.go`, which returns
  `profile.ErrNotFound` for a blocked pair, so the 404 is the one of an unknown id. Never answer a blocked pair
  from another code path, and never with `avatar_not_found`.
- Nobody is told they were blocked, by a response, a notification or an email.

## Block routes

- The block belongs to the caller: the blocker is always the session, and the path's `{id}` is the *target's*
  public id. Nothing in a query or a body names a blocker; the body is never read.
- **An id that names no profile answers 204, exactly as a stored block does** (unknown, malformed, a `users.id`,
  a member with no profile), on both writes. Never answer 404 there: a blocked member could confirm the block
  with one request. Only the caller's own id is told apart: 422 `member: self`.
- Both writes are idempotent and are one handler (`handleBlockWrite`). A block that exists is not stored again
  and keeps its `created_at`; removing none is not an error.
- `Block` locks the blocker's `users` row first (`FOR NO KEY UPDATE`), then counts and inserts: that is what
  keeps the limit exact. Keep the `users` row the first lock of any transaction here (012, 029).
- At most `safety.MaxBlocks` (200) per member; one more is 422 `blocks: too_many`. A repeat is never refused.
- `GET /v1/me/blocks` is composed in `server`: `ListBlocked` for whom and in what order, and
  `profile.Service.PublicByUsers` for the names. Its entries are `blockedMemberResponse` (`id`, `display_name`), a
  type of its own: no account id, timestamp or picture.
- **The list never names a member who has blocked the caller,** whoever blocked first (`listBlocked`'s
  anti-join), nor one with no profile. To the caller neither exists. In both cases the caller's block is still
  stored, still counts toward the limit and can still be removed by id; never count or filter the limit by
  what is listed.
- The list is written by `writeBlocks`, without `encoding/json`'s HTML escaping, so that 200 entries with the
  longest names stay under the 64 KiB a client reads. Don't switch it to `writeJSON`, and don't add a field to
  an entry without redoing that sum (033).
- `PUT` and `DELETE` share `UserLimits.BlockWrite`. The list is the caller's own data and is not limited.

## Logs

- `block: added` and `block: removed`, with the blocker's `user_id` only and only when a row changed.
- Never log whom: not the blocked `user_id`, a public id or a name. A read hidden by a block logs nothing.

## The gate (restates 031; not lifted)

No feature that lists, suggests or searches members ships, and the app is not released to the public, until all
three hold: blocking and reporting are deployed; `docs/moderation.md` names a reviewer and a review interval
(the document is not written yet); and the feature that lets members meet exposes "Block" and "Report" on other
members' profiles.

## Reporting: approved, not implemented

`PUT /v1/me/reports/{id}`, the `reports` table, `UserLimits.ReportWrite` and `docs/moderation.md` are designed in
`openspec/changes/add-block-and-report/` and do not exist in the code. Nothing above describes them.
