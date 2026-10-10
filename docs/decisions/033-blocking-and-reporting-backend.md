# 033: Blocking and reporting (backend)

> **Status:** draft. **Blocking is implemented and tested:** the `blocks` table, `internal/safety`, the three
> block routes, their limit and the check on the two member reads. **Reporting is approved and not built:** its
> table, its route, its limit and `docs/moderation.md` are not in the repository, and this record does not
> describe them yet. It is completed when they are.
>
> **Changes earlier records:** 031 (its "one 404" has one more cause, a block; a route under `/v1/me` may carry
> another member's public identifier, as the target of the caller's own block; its gate is restated below and
> **not** lifted) and 018 (a fourth per-user write limit, `user_block_write`: twelve limiters).
>
> **Client side:** 034, not written.
>
> **Current rules:** `.claude/rules/safety.md`, `.claude/rules/backend.md`, and for the two member reads
> `.claude/rules/profile.md` and `.claude/rules/avatar.md`.

- **Scope:** a signed-in member blocks and unblocks another member, named by the profile's public identifier,
  and reads the list of the members they blocked. While a block exists between two members, in either
  direction, neither can read the other's profile or picture. Nobody is told that they were blocked. Nothing
  here lists, suggests or searches members, and nothing is built for features that don't exist (friend
  requests, discovery, chat): only the rule they must follow is written down.
- **One domain package, `internal/safety`:** "what one member does about another". `Service` has `Block`,
  `Unblock`, `Blocked` and `ListBlocked`, takes and returns internal user ids only, imports none of `auth`,
  `server`, `profile`, `language` and `avatar`, never reads the request context, and has the typed errors of the
  other domains (`ValidationError` with `FieldError`, `ErrUserGone`), mapped in `writeServiceError`. `main` builds
  it and `server.New` takes it as its sixth argument.
  - *One package for blocks and reports, not two:* they share the target rule (not oneself), the error type and
    the never-log rules, and are wired and documented together.
  - *Not inside `profile`:* a block references users, not profiles (below), and later features will ask
    `Blocked` without wanting anything of a profile.
  - *The public identifier is resolved in `server`,* with `profile.Service.Public`, exactly as the member reads
    do. `safety` never sees a public identifier, so it cannot store, return or log one.
- **The `blocks` table** (migration 00009): `blocks(blocker_id → users ON DELETE CASCADE, blocked_id → users ON
  DELETE CASCADE, created_at, PRIMARY KEY (blocker_id, blocked_id))`, with `blocks_not_self CHECK (blocker_id <>
  blocked_id)` and the index `blocks_blocked_id_idx (blocked_id)`.
  - *Keyed by `users.id`, not `public_id`:* a block must survive the blocked member's profile being taken down
    and saved again, which gives it a new public identifier. It also gives the cascade when either account is
    deleted and the dead-credential 401 for a blocker deleted during the request.
  - *One row per direction,* so "A unblocks B" can never remove B's block of A, and A's list is one index scan.
  - *The second index* serves what the primary key cannot: access by `blocked_id` alone, which is the cascade
    when the blocked user is deleted and any later query that starts from the blocked member. Which index the
    planner picks for a given statement was not measured.
- **A block is symmetric in effect: `Blocked(a, b)`.** One statement, `SELECT EXISTS (… WHERE (blocker_id,
  blocked_id) IN ((a, b), (b, a)))`, with the same answer in both orders; each order is an equality on both
  columns of the primary key. It is the one question about a block that anything outside `safety` asks. A block
  made by A hides A from B as much as B from A (approved): a member who blocks someone does not want to be
  watched by them, and does not want to keep watching either.
- **The routes:**

  | Route | Success | Notes |
  |---|---|---|
  | `PUT /v1/me/blocks/{id}` | 204 | the body is never read; also when already blocked |
  | `DELETE /v1/me/blocks/{id}` | 204 | also when there was no block |
  | `GET /v1/me/blocks` | 200 `{"blocks":[{"id","display_name"}]}` | newest first; always an array |

  - *`PUT` and `DELETE` on the target, not `POST /v1/me/blocks` with a body:* the pair is the resource, so the
    method alone makes a repeat harmless. The client resends a protected request once after a 401 and a member
    retries after a 503 (018), so both must change nothing the second time. They are added to the list of
    retriable operations in `requestTimeout`'s comment.
  - *The two writes are one handler* (`handleBlockWrite`) given `Service.Block` or `Service.Unblock`, so their
    answers for an identifier that names nobody cannot drift apart.
- **A route under `/v1/me` carries another member's identifier** (changes 031 and the "member's own resource"
  rule of `backend.md`). 031's rule was that ownership is a property of the routes: every write is under
  `/v1/me/…` and selected by the session. That still holds: the block is the caller's, the blocker is always the
  session, and nothing in the path, the query or a body can name another blocker (the body is not even read).
  The identifier names the block's *target*. `/v1/profiles/{id}` stays GET-only, and no request can change
  anything that belongs to the member named.
- **A write for an identifier that names no profile answers 204** (a deviation from 031's "one 404"). The
  handlers resolve `{id}` with `profile.Service.Public`; `ErrNotFound` (unknown, malformed, a `users.id`, a member
  with no profile) is answered 204 with nothing stored, the answer of a block that was stored, identical in
  status, body and headers. Only the caller's own identifier is told apart (422 `member: self`), which tells the
  caller nothing they don't know.
  - *Why:* a block makes the blocker's profile read as 404 to the blocked member. If a write to that
    identifier answered 404 for "no profile" and 204 for "exists", one request would turn "gone or blocked?"
    into "blocked". It is the reasoning of 005 (neutral answers) applied to a protected route.
  - *Consequence, and wanted:* a member can block a member who has blocked them. Blocking first is not a way
    out of being blocked or, later, reported.
  - *Turned down, 404 as on the reads:* simpler to explain, and it would tell a client about a mistyped
    identifier. The app only ever sends identifiers it was given, and the session layer refuses a malformed one
    before any request.
  - A malformed identifier still makes no query (`profile.Public` decides), as on the reads.
- **`Block` is one transaction that locks the blocker's `users` row first** (`FOR NO KEY UPDATE`; absent →
  `ErrUserGone` → 401), then reads the blocker's count and whether this block exists in one statement: if it
  exists, done; at 200, `blocks: too_many`; else insert.
  - *Why a transaction and a lock:* the limit cannot be checked and the row inserted in one statement that
    stays exact under concurrency. The lock makes one member's blocks run in turn, so each counts what the one
    before committed: two blocks at 199 leave 200 with one refused, and sixteen identical ones insert once.
  - *Lock order (012, 029):* the `users` row is the first lock, as in `auth`. The token flows hold it `FOR
    UPDATE` and login holds it `FOR SHARE`; `FOR NO KEY UPDATE` conflicts with both, so this waits for them and
    they for it, and neither is a cycle: each takes the `users` row before any other lock on that user and
    afterwards touches only that user's token and session rows, which this transaction never locks.
  - *Two members blocking each other at once do not deadlock:* each holds its own row `FOR NO KEY UPDATE` and
    the insert takes only `FOR KEY SHARE` on the other's, for the foreign key, which `FOR NO KEY UPDATE` does not
    block. This is the analysis of 029.
  - *A foreign-key failure on `blocked_id`* means the target was deleted after the handler resolved it: there
    is nothing to block, nothing is stored and the answer is 204.
  - *`Unblock` is one `DELETE`* on the primary key. It needs no lock and no check of the user: without the user
    there is no row.
- **Idempotent:** a block that exists is not stored again, keeps its `created_at` (so its place in the list) and
  is never refused for the limit; removing a block that doesn't exist is not an error. Neither writes a log line.
- **At most 200 blocked members per member** (`safety.MaxBlocks`; an assumed default, a constant). It bounds
  what one account can store, and it bounds the list to one response under the 64 KiB a client reads, which is
  why the list has no paging: an entry with the longest name there can be (50 characters of four bytes each) is
  264 bytes, and a full list of them about 52 KiB.
  - *The list is written without `encoding/json`'s HTML escaping* (`writeBlocks`, for this response only).
    `writeJSON` writes `&`, `<` and `>` as six bytes each; a valid name of one letter and 49 of those made a
    359-byte entry, and 200 of them about 70 KiB, which the client would refuse to read: that member's list
    would never load. The escaping protects JSON placed inside HTML; this answer is `application/json` with
    `nosniff`. Without it no character of a name takes more than four bytes, and the bound is exact. Tests
    build a full list of each kind of name. (Found at implementation, fixed at the review of task group 2. The
    other responses keep `writeJSON`; none of them is a list.)
- **Nobody blocks themselves:** `Block` and `Unblock` refuse `blocker == blocked` with `member: self` before any
  query; the CHECK backs it. A member with no profile has no public identifier, so only a member with one can
  name themselves at all.
- **Enforcement is at the two member reads** (`memberFor` in `server/members.go`, which both go through). After
  `profile.Public` returns the owner's `UserID`, and when it differs from the reader's: `safety.Blocked(reader,
  owner)`; true is returned as `profile.ErrNotFound`, the very error of a miss, so the status, the body and the
  headers cannot drift. Nothing is logged, and the member-read limit is spent as for any miss.
  - *In `server`, not `profile`:* it is the composer of the member read already, and 031 gave "who may read"
    to the caller of `Public`. `profile` stays unaware of blocks.
  - *After `Public`:* a miss costs no extra query, and the block needs the `UserID` anyway. A member reading
    their own identifier is never asked about.
  - *Not one transaction with the read:* a block committed between the check and the read shows on the next
    request, as 031 accepted for a save.
  - *The picture answers `profile_not_found`, not `avatar_not_found`,* whether or not there is a picture: the
    second would confirm that the profile exists.
  - *One more query per member read,* a primary-key lookup, only when the identifier resolved to another member.
- **The list leaves out a member who has blocked the caller** (decided at the review of task group 2; it
  narrows the approved "the list names every blocked member who has a profile"). `listBlocked` has an anti-join
  on the reverse row. Without it the list was a way across a block: if A and B have each blocked the other,
  whoever was first, B's list named A with A's current name, which is something of A's profile, and it showed
  B that the 404 on A's profile is a block and not a deleted profile. B needed no trick for it, and one existed
  too (a block of A's identifier answers 204 for anybody). The spec requires that no response to B differ from
  what B would get if A's profile did not exist, and with the anti-join B's list is byte for byte that.
  - *B's own block of A is untouched:* it is stored, it still hides the two from each other, it still counts
    toward the limit, and `DELETE /v1/me/blocks/{id}` still removes it. It shows again when A's block is gone.
  - *The price, accepted with the fix:* while both have blocked each other, neither sees the other in the
    list, so neither can undo their block from a list; only a client that still holds the identifier can.
    Two members who each blocked the other stay hidden from each other until one of them does.
  - *Fixed in the list, not in the block write:* refusing to store a block of a member who blocked the caller
    would not cover the member who blocked first, and would take away "blocking first is no way out".
- **The list is composed in `server`.** `safety.ListBlocked` returns the blocked user ids, `created_at DESC`;
  `profile.Service.PublicByUsers(ctx, ids)`, new, returns the public profiles of those users in one query
  (`WHERE user_id = ANY(…)`, no query for no ids); the handler emits them in the list's order as
  `blockedMemberResponse{id, display_name}`, a type of its own like `memberProfileResponse`.
  - *A blocked member with no profile is left out* (approved): they have nothing to be named by. The block
    still holds, and a profile saved again shows them under its new identifier.
  - *Names only, no picture* (approved): the member picture route answers 404 across a block, and a second way
    to serve a blocked member's picture is not worth one screen.
  - *Not limited:* it is the caller's own data, like the other own reads.
  - *Two reads, not one snapshot:* a name changed in between is a name a moment newer.
- **Limit** (amends 018): `UserLimits.BlockWrite`, `user_block_write`, burst 10 then 1 every 6 s (an assumed
  default), one bucket for `PUT` and `DELETE`, inside `authn` and keyed by the caller, so a member's sessions
  share it and unauthenticated requests spend nothing. Requests for unknown identifiers, repeats and refusals
  spend it. `serverOptions` wires it and `TestServerOptionsWireEveryRateLimit` covers it. Twelve limiters.
- **Logs:** `block: added` and `block: removed`, with the blocker's `user_id` only and only when a row
  changed. Never the blocked member, by account id, public identifier or name: a line naming both would link
  two members in the one place 031 keeps free of that. The handlers and the hidden reads log nothing.
- **Account deletion:** both foreign keys cascade, so a block goes when either account does; a deleted
  blocked member leaves the list and no longer counts toward the limit. No endpoint deletes an account (027).
- **What a block means for features that do not exist yet.** This is why `Blocked` is symmetric, and each later
  change must follow it:
  - Any route through which one member reads, finds, lists or contacts another asks `safety.Blocked`, or
    filters by `blocks` in both directions in its query. The answer for a blocked pair is whatever "that member
    does not exist" is on that surface, never a message about a block.
  - *Friend requests:* one sent across a block is answered as for an unknown member; nothing is stored and
    nobody is notified. Creating a block removes pending requests in both directions and an existing
    friendship; unblocking restores neither. The Friends change implements both, in the transaction that
    writes the block or composed in `server`, and says which in its record.
  - *Discovery, search and any list of members:* blocked pairs are excluded in both directions.
  - *Chat and notifications:* no new message or notification crosses a block.
  - *The actions themselves* (approved): the first feature through which a member meets another must expose
    "Block" and "Report" on other members' profiles before it is released.
- **The gate of 031 is restated, and not lifted by this change** (approved). No feature that lists, suggests or
  searches members ships, and the app is not released to the public, until **all three** hold:
  1. blocking and reporting are deployed (blocking is built here; reporting is the rest of this change);
  2. the review process of `docs/moderation.md` is established: a named reviewer and a review interval are
     written in it. That document does not exist yet;
  3. the feature that lets members meet exposes "Block" and "Report" on other members' profiles.
- **Accepted risks:**
  - *A blocked member can still infer the block:* a profile that answered 200 now answers 404, and a second
    account sees it. Inherent to hiding. No single response says "blocked", and nothing is sent to them.
  - *A member at the limit learns more:* with 200 blocks, a block of an existing member answers 422 and of an
    unknown identifier 204. It needs 200 real blocks and reveals what a second account reveals.
  - *A blocked member opens a new account.* Not solved here; accounts need a verified address, and a report
    about the new account is the remedy.
  - *An unknown identifier on a write is silently accepted,* so a client bug could go unseen.
  - *Rolling back the binary stops enforcing blocks:* the previous binary ignores the table. The rows stay and
    apply again on redeploy.
  - *The limit is per process* (018), as every other.
  - *A block that is not listed still counts toward the 200,* and cannot be removed from a list: one whose
    target has no profile, or has blocked the caller. A member with many of them is refused a new block with
    no entry to remove. It needs many such blocks, and a target's profile saved again, or their block removed,
    brings the entry back. Counting only listed blocks was turned down: the count would then tell a member
    how many of the members they blocked have blocked them.
  - *The time a write takes differs* between an identifier that names a member (a transaction) and one that
    names nobody (one read, or none when malformed), as on the reads (031). It was not measured.
- **Tests of blocking:** the table (the primary key and the reverse row, `blocks_not_self` by name, both
  cascades, 00009 applied and rolled back with rows present); the service on the database (a block, a repeat
  writing and logging nothing, an unblock with and without a row and leaving the other member's block, `Blocked`
  in both orders, the list newest first, only the caller's and without a member who blocked the caller, self refused with no query, a deleted blocker and
  a deleted target, the limit with a repeat at it and room after an unblock, sixteen concurrent identical blocks,
  two at 199, mutual blocks without a deadlock, a concurrent block and unblock, the exact log lines);
  `profile.PublicByUsers` (the profiles of the users given, none for a user without one or a deleted one, no
  query for no users, nothing logged); over HTTP each scenario of the `member-blocking` spec that has a route:
  the three routes and their exact answers, the session as the only blocker with another member's identifiers
  in the query and a body, a caller with no profile, every kind of identifier that names nobody answered
  byte-identically to a stored block and reaching no database when malformed, blocking a member who blocked the
  caller, the list's exact keys with no account id, email or timestamp, the current name, a member without a
  profile left out and back under a new identifier, a member who blocked the caller left out whoever blocked
  first (the list byte-identical to the one from before, through a rename, back after their unblock, and the
  hidden block still removable), a full list under 64 KiB and in order for names of four-byte characters, of
  `&` and of `<>`, the limit and two
  concurrent blocks at it, nothing of the blocked member changed and a third member's read byte-identical, both
  directions hidden on both member routes with a 404 byte-identical to an unknown identifier's (with and without
  a picture), a third member and one's own identifier unaffected, the read restored only when no block is left,
  the member-read limit spent by a hidden read, the 401 matrix on the three routes with nothing changed,
  `no-store`, 405, the bucket (shared by the two writes and a member's sessions, spent by every write, not by
  unauthenticated requests, separate from the four others), a caller deleted before the write, a deleted blocked
  member leaving the list and the count, the handlers failing closed, an expired deadline as 503, an opaque 500
  with the route's pattern, and the exact log lines with no line for a hidden read; and that `main` wires the
  limit.
- **Deferred:** everything about reports (the rest of this change); the client (034); what a block does to
  friend requests, discovery, search and chat (each of those changes); paging the list; a picture in the list; a way to undo, from the app, a block that is not listed;
  telling "no profile" from "exists" on a write; an endpoint that deletes an account.
