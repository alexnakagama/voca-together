# Design

## Context

See `proposal.md` for the motivation, the ten approved product decisions and the four constants still assumed
(P3, P4, P5, P9). What
exists and constrains the approach:

- **The gate (decision 031).** Profiles are readable by every signed-in member "before reporting, blocking or
  moderation exist", accepted because an identifier is learned only from its owner. No feature that lists,
  suggests or searches members ships before reporting and blocking do. Take-down is a manual `DELETE`.
- **The member read (031, `server/members.go`).** `GET /v1/profiles/{id}` and `/avatar` resolve the path value
  with `profile.Service.Public`, which returns the owner's internal `UserID`, then read languages and the
  picture by that `UserID`. `server` composes; `profile`, `language` and `avatar` import neither each other nor
  `auth`. Everything that is not the id of a saved profile is one 404 `profile_not_found`. Nothing is logged.
- **A member's own resource (`backend.md`).** Selected only by the authenticated `UserID`, never an id in a
  route, query or body; a save is a full replace and idempotent, because the client resends once after a 401
  and retries after a 503; each write has its own per-user bucket, wired in `serverOptions`, where a limit left
  out disables itself silently (`TestServerOptionsWireEveryRateLimit`).
- **Public identity.** A member is named to others only by `profiles.public_id`. A member with no profile has
  no public identity. `users.id` never leaves the server and is the key the logs carry.
- **Lock order (012, 029).** A transaction that needs the `users` row takes it first. `language` takes it
  `FOR NO KEY UPDATE`, which does not block the `FOR KEY SHARE` an insert referencing the user takes.
- **Account deletion.** No endpoint deletes an account (deferred by 027). Every table of user data references
  `users` with `ON DELETE CASCADE`; a write for a user deleted after authentication answers 401
  (`ErrUserGone`).
- **Client.** `MemberProfileScreen` at `/members/<id>` is reached only from "See public profile" with the
  caller's own id. 032's rules for it: read-only, no app bar action, never reads the caller's own profile.
  `_authorized` resends once after a 401, so a protected write must be idempotent. `ApiClient` already sends
  PUT and DELETE, reads a 204, and caps a JSON answer at 64 KiB. `authRedirect` is a function of the status and
  the path: a set of exact paths and one pattern (`Routes.isMember`). Every failure goes through
  `presentFailure`; client validation is empty fields only.
- **Specs.** `member-profile` and `profile-avatar` state the 404s and "a member route cannot change anything".

## Goals / Non-Goals

**Goals:**

- A block is enforced where a member's data is served, by one question with one answer for both directions,
  which later features reuse instead of inventing their own.
- Being blocked is not observable in any single response: it looks like a profile that does not exist.
- A report cannot be read back, by anyone, through the API or the logs.
- Ownership stays a property of the routes: every write is under `/v1/me/…` and belongs to the session.
- Every write is idempotent, so the client's resend and a member's retry change nothing.

**Non-Goals:**

- Anything in the product that acts on a report: status, a review screen, an answer to the reporter, automatic
  hiding after N reports. Acting on a report is a documented manual process (decision 15).
- A copy of the reported content in a report (approved: none).
- A way to reach another member's profile, or any notification.
- A relation model for Friends. Only the rule Friends must follow is written down (decision 7).
- A general "viewer context" in profile responses. The member response stays the definition of "public".
- Paging the blocked list, or an interface in front of the new package.

## Decisions

### 1. One new domain package, `internal/safety`, with two tables

`safety.Service`: `Block`, `Unblock`, `Blocked` (is there a block between two users, either way),
`ListBlocked` (the user ids the caller blocked, newest first), `ParseReport` and `Report`. It takes and returns
internal user ids only, imports none of `auth`, `server`, `profile`, `language` and `avatar`, never reads the
request context, and has typed errors (`ValidationError` with `FieldError`, `ErrUserGone`) mapped in
`writeServiceError` like the others. `main` builds it; `server.New` takes it as a sixth argument.

- *Why one package and not `block` and `report`:* both are "what one member does about another", share the
  target rule (not oneself), the error type and the never-log rules, and are wired and documented together.
  Two packages would double the wiring for two small files of SQL.
- *Why not inside `profile`:* blocks reference users, not profiles (decision 2), and Friends and chat will ask
  `Blocked` without wanting anything of a profile.
- *The public id is resolved in `server`,* with `profile.Service.Public`, exactly as the member reads do. The
  new package never sees a public id, so it cannot log or return one.

### 2. The `blocks` table (migration 00009)

```
blocks(
  blocker_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  blocked_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (blocker_id, blocked_id),
  CONSTRAINT blocks_not_self CHECK (blocker_id <> blocked_id)
)
CREATE INDEX blocks_blocked_id_idx ON blocks (blocked_id);
```

- *Keyed by `users.id`, not `public_id`:* a block must survive the blocked member's profile being taken down
  and saved again (a new public id), and it gives the cascade and the dead-credential 401 for free.
- *One row per direction,* so "A unblocks B" can never remove B's block of A, and the list is one index scan.
- `Blocked(a, b)` is one statement: `SELECT EXISTS (… (blocker_id, blocked_id) IN ((a, b), (b, a)))`. Both
  directions are covered by the primary key: each is an equality on both of its columns, `(a, b)` and `(b, a)`.
- *The second index* is not for the reverse half of that lookup. It is for what the primary key cannot serve,
  access by `blocked_id` alone: the cascade when the blocked user is deleted, and later queries that start from
  the blocked member. (Amended at the review of task group 1; which index the planner picks for a given
  statement was not measured and is not claimed here.)

### 3. Block and unblock: routes, idempotence, the cap and concurrency

| Route | Success | Notes |
|---|---|---|
| `PUT /v1/me/blocks/{id}` | 204 | the body is never read; also when already blocked |
| `DELETE /v1/me/blocks/{id}` | 204 | also when there was no block |
| `GET /v1/me/blocks` | 200 `{"blocks":[{"id","display_name"}]}` | newest first; always an array |

- *PUT and DELETE on the target, not `POST /v1/me/blocks` with a body:* the pair is the resource, so the
  method alone makes a repeat harmless, which `mobile.md` requires of anything `_authorized` may resend.
- **`Block` is one transaction that locks the caller's `users` row first** (`FOR NO KEY UPDATE`, absent →
  `ErrUserGone`), then: if the row exists, done; else count the caller's blocks, refuse at 200
  (`blocks: too_many`), insert. The lock makes one member's blocks run in turn, which is what makes the cap
  exact under concurrency. Two members blocking each other at once each hold their own row and take only
  `FOR KEY SHARE` on the other's, which `FOR NO KEY UPDATE` does not block: no deadlock, the analysis of 029.
  A foreign-key failure on `blocked_id` means the target was deleted meanwhile: nothing to block, 204.
- *`Unblock` is one `DELETE`.* It needs no lock and no user check: with no user there is no row.
- *The cap (200, P5):* it bounds the list to one response under the client's 64 KiB (an entry is at most about
  270 bytes), so no paging is needed, and it bounds what one account can store.
- *Self:* `Block` and `Unblock` refuse `blocker == blocked` with `member: self` before any query; the CHECK
  backs it.
- *Logs:* `block: added` and `block: removed` with the caller's `user_id` only, and only when a row changed.

### 4. A write for an identifier that names no profile answers 204 (a deviation from 031's "one 404")

The handlers resolve `{id}` with `profile.Service.Public`. `ErrNotFound` (unknown, malformed, a `users.id`, no
profile) is answered 204 with nothing stored. Only the caller's own id is told apart (`member: self`), which
tells the caller nothing they don't know.

- *Why:* a block makes the blocker's profile read as 404 to the blocked member. If a write to that id answered
  404 for "no profile" and 204 for "exists", one request would turn "gone or blocked?" into "blocked". With one
  answer for both, the block and report routes say nothing about what an id names. It is the reasoning of 005
  (neutral answers), applied to a protected route.
- *Consequence:* a member **can** block and report a member who has blocked them, which is wanted: blocking
  first must not be a way to escape a report.
- *For the report,* the body is validated **before** the id is resolved (`safety.ParseReport`), so a 422 never
  depends on whether the id names anyone.
- *Alternative, 404 as on the reads:* simpler to explain and it tells a client about a mistyped id; turned down
  for the oracle. The app only ever sends ids it was given.

### 5. Enforcement: the two member routes ask `Blocked` after resolving the id

In `handleGetMemberProfile` and `handleGetMemberAvatar`, after `profile.Public` returns the owner's `UserID` and
when it differs from the reader's: `safety.Blocked(reader, owner)`; true is answered through
`writeServiceError(profile.ErrNotFound)`, the same path as a miss, so the status, body and headers cannot
drift. Nothing is logged. The member-read limit is spent as for any miss.

- *Why in `server`:* it is the composer of the member read already, and the rule is "who may read", which 031
  gave to the caller of `Public`. `profile` stays unaware of blocks.
- *Why after `Public`:* a miss costs no extra query, and the block needs the `UserID` anyway.
- *Not one transaction with the read:* a block committed between the check and the read shows on the next
  request, as 031 accepted for a save.
- *The picture answers `profile_not_found`, not `avatar_not_found`:* the second would confirm that the
  profile exists.

### 6. The blocked list is composed in `server`

`safety.ListBlocked` returns the blocked user ids, `created_at DESC`. `profile.Service.PublicByUsers(ctx,
ids)`, new, returns the public profiles of those users in one query (`WHERE user_id = ANY($1)`). The handler
emits them in the list's order as `blockedMemberResponse{id, display_name}`, a type of its own, and skips a
user with no profile (P8). `GET /v1/me/blocks` is the caller's own data, so it is unlimited like the other own
reads.

- *No picture:* the member picture route answers 404 across a block, and a second way to serve a blocked
  member's picture is not worth one screen.
- *Amended at the review of task group 2:* `ListBlocked` leaves out a member who has blocked the caller, so the
  list cannot name, or confirm a block by, a member who is hidden from the caller; the caller's block stays and
  is removable by id. And the response is written without `encoding/json`'s HTML escaping: with it, names made
  of `&`, `<` or `>` took an entry to 359 bytes and a full list over the client's 64 KiB (decision 3's "at most
  about 270 bytes" assumed no escaping). Reasoning and the accepted price in decision record 033.
- *Not two transactions' worth of care:* a name changed between the two reads is a name a moment newer.

### 7. What a block means for features that do not exist yet

Written into decision 033 and `.claude/rules/safety.md` as the rule each later change must follow, and the
reason `Blocked` is symmetric:

- Any route through which one member reads, finds, lists or contacts another asks `safety.Blocked`, or
  filters by `blocks` in both directions in its query. The answer for a blocked pair is whatever "that member
  does not exist" is on that surface, never a message about a block.
- **Friend requests:** one sent across a block is answered as for an unknown member; nothing is stored and
  nobody is notified. Creating a block removes pending requests in both directions and an existing friendship;
  unblocking restores neither. The Friends change implements both, in the transaction that writes the block
  or composed in `server`, and says which in its record.
- **Discover, search and any list of members:** blocked pairs are excluded in both directions.
- **Chat and notifications:** no new message or notification crosses a block.

- **The actions themselves (approved, P1):** the first feature through which a member meets another must
  expose "Block" and "Report" on other members' profiles before it is released. Today they exist on the member
  screen and nothing navigates to another member's profile; that feature's change owns making them reachable
  and verifying it on a device.

Nothing of this is built or testable now; the `member-blocking` spec covers the two surfaces that exist.

### 8. The `reports` table (migration 00010)

```
reports(
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  reporter_id uuid REFERENCES users (id) ON DELETE SET NULL,
  reported_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  reason      text NOT NULL,
  details     text NOT NULL DEFAULT '',
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT reports_reporter_reported_key UNIQUE (reporter_id, reported_id),
  CONSTRAINT reports_not_self   CHECK (reporter_id <> reported_id),
  CONSTRAINT reports_reason     CHECK (reason IN ('harassment','inappropriate_content','spam','impersonation','other')),
  CONSTRAINT reports_details_length CHECK (char_length(details) <= 1000)
)
CREATE INDEX reports_reported_id_idx ON reports (reported_id);
```

- *`SET NULL` for the reporter, `CASCADE` for the reported (approved):* a report must stay usable when its
  author leaves, and nothing about a member outlives their account. A NULL reporter never collides in the
  UNIQUE constraint, and the CHECK passes on NULL.
- *A surrogate `id`:* the pair cannot be the primary key once the reporter may be NULL. It is never returned.
- *The reason is text with a CHECK, not an enum type:* adding a reason is a new migration that replaces the
  CHECK, with no `ALTER TYPE`.
- *The index* is how reports are read today (by SQL, per reported member) and serves the cascade.

### 9. The report route

`PUT /v1/me/reports/{id}` with `{"reason": "...", "details": "..."}` → 204. Order in the handler: the limit,
decode (`reportRequest` lists `reason` and `details`; anything else is 400), `safety.ParseReport` (422),
resolve the id (decision 4), `safety.Report` (self → 422).

- *PUT on the target:* one report per pair (approved), so the pair is the resource and a save is a full
  replace, the shape of `PUT /v1/me/profile`.
- *One statement:* `INSERT … ON CONFLICT (reporter_id, reported_id) DO UPDATE SET reason, details, updated_at
  WHERE the content differs`. A repeat writes and logs nothing. Concurrent reports take the row lock in turn
  and the last wins whole. A foreign-key failure on `reporter_id` is `ErrUserGone` (401); on `reported_id` the
  target is gone and the answer is 204.
- *204 and no body:* there is no report to give back, and returning one would be the first read of a report.
- *The body cap is 64 KiB,* far above 1000 characters, so a long paste gets `details: too_long` and not 400, as
  027 arranged for the bio.
- *Details are validated more simply than a bio:* valid UTF-8, NFC, CR and CRLF to LF, tabs to spaces, trimmed,
  at most 1000 characters, no other control character. The profile's rules against invisible and bidirectional
  characters protect text **shown to members**; details are read by the people who run the service and never
  rendered in the app, and copying those rules across packages is not worth it.
- *Logs:* `report: saved` with the reporter's `user_id` only, and only when a row was written. Never the
  reason, the details, the reported user or a public id. The reported `user_id` in a log line would link the
  two members in a place 031 keeps free of that.

### 10. Limits (amends 018; thirteen limiters)

`UserLimits.BlockWrite`, `user_block_write`: burst 10, then 1 every 6 s, shared by PUT and DELETE, like the
other writes. `UserLimits.ReportWrite`, `user_report_write`: burst 5, then 1 a minute, the picture's allowance:
a report is rarer than a save and each is something a person must later read. Both are inside `authn`, keyed
by the caller, wired in `serverOptions` and covered by `TestServerOptionsWireEveryRateLimit`. Requests for
unknown ids, refused ones and repeats spend them.

### 11. Client: data and session layer

- `ApiPaths.myBlocks`, `myBlock(id)` and `myReport(id)`; the two functions refuse a non-canonical id, like
  `memberProfile(id)`.
- `lib/api/blocked_member.dart`: `BlockedMember(id, displayName)`, strict `fromJson`, redacted `toString`.
  `lib/api/report_reason.dart`: the enum `ReportReason` with its wire code. Both on the screens' allowlist.
- `AuthApi` and `SessionManager` gain, each one `_authorized` call and nothing cached: `blockMember(id)` and
  `unblockMember(id)` → `void`, `blockedMembers()` → `List<BlockedMember>`, `reportMember(id, reason,
  details)` → `void`. All four writes are idempotent on the server, so the resend after a 401 is safe.
- `presentFailure` gains `reasonError`, `detailsError` and `blocksError` and their codes. `member: self` has no
  text of its own (the app never sends it) and falls to the generic message.
- No change to `ApiClient`.

### 12. Client: the member screen gets a menu and learns whether the profile is one's own (amends 032)

The screen's load adds `session.profile()` to the two it waits for; the menu ("Report", "Block") is in the app
bar only when a profile loaded and its `id` differs from the caller's own (a caller with no profile is never
the owner). The three loads fail whole, with "Try again".

- *Why the caller's own profile:* the route carries only an id, and the menu must not appear on one's own
  profile, which today is the only profile the app can open. *Turned down:* a flag in `GoRouter`'s `extra`
  (lost when the route is rebuilt, and a second thing a route says); an `is_self` field in the member response
  (the response is the definition of "public" and must not depend on the reader); showing the menu always and
  letting the server refuse.
- *Fail whole rather than hide the menu when that load fails:* a member who came to block someone must not
  find the action missing without a word.
- **Block:** a confirmation (the scrolling layout of the discard dialog), then `blockMember`. On success the
  screen drops the profile it held and shows the blocked text; back leaves. On failure the profile stays, with
  the message under the header. Busy disables the menu and holds back leaving, like a picture action.
- **Unblock by id, an immediate undo (added before task group 6):** the blocked state has an "Unblock"
  control: a confirmation
  that names nobody (the profile was dropped), then `unblockMember(id)` with the route's id. It never reads
  `blockedMembers()`. On success the screen runs its load again, so it shows the profile, or "unavailable" when
  the other member's block remains, which is the answer for any id and says nothing new. On failure the blocked
  state stays, with the message. Busy as for the block.
  - *Why:* an unblock must need the id and nothing else, and this is the one place the app holds the id of
    a member it just blocked. The backend is unchanged: the blocker is the session's member, so only the
    caller's own block is ever removed (decision 3).
  - *What it is not (approved 2026-10-10):* a lasting way to unblock a member who is absent from the list.
    The blocked state is reached only by blocking a profile that could be read, so that member is in the list
    then; a member who blocks back later is hidden when the screen has been left, and the app then has no id
    for them. That gap is accepted for this change (Risks); the backend and decision 033 are not reopened.
    The blocked text therefore no longer says the member can be unblocked from "Blocked members".
  - *Turned down:* putting such members back in the list (it would name, and confirm a block by, a member
    hidden from the caller: the amendment of decision 6); "Unblock" on every "unavailable" profile (a control
    that mostly does nothing, on a state that must look the same for every id).
- **Report:** pushes `Routes.memberReport(id)`. Nothing is reloaded on return.

### 13. Client: the report screen and the blocked list

- `ReportMemberScreen(session, id)` at `/members/<id>/report`: a radio group of the five reasons, the details
  field, the privacy notice, "Send report". A route, not a sheet, so `authRedirect` stays the only thing that
  decides where a signed-out user is, and the flow is plainly separate from blocking. It loads nothing, so it
  needs no name: the title is "Report member".
- *No discard question on leaving:* the form is one choice and an optional text; 028's question guards a
  profile a member has composed. Leaving is held back only while the request is in flight.
- `BlockedMembersScreen(session)` at `/blocked`, pushed from a new "Blocked members" control on home (the app
  has no settings screen; the profile page is about one's public self). It loads on every entry, removes an
  unblocked member from what it holds without a reload, and has one busy flag for all rows.
- `Routes.blocked` joins `signedInRoutes`. `Routes.memberReport(id)` and `Routes.isMemberReport(path)` are a
  second pattern beside `isMember`; `authRedirect` is still a function of the status and the path.
- Strings, each with an `@` description: the menu and its two items; the block confirmation, its two answers,
  the blocked text and the limit message; the report title, the five reasons, the details label, the notice,
  "Send report", "choose a reason", the two details errors and the confirmation; "Blocked members", its empty
  text, "Unblock", its confirmation and the label that names whom a control unblocks; the member screen's own
  unblock confirmation, which names nobody (decision 12), and the reworded blocked text.

### 14. Documents

- **Decision 033** (backend): blocks, reports, the symmetric rule and what it means for later features, the
  neutral 204, the lock analysis, the limits, deletion, accepted risks, the restated gate. Header notes on 031
  (the gate; "one 404" has one more cause; a `/v1/me` route may name a target) and 018 (thirteen limiters).
- **Decision 034** (client): the menu and the own-profile read, the two flows, the list, the two routes. Header
  notes on 032 (the member screen has actions and reads the caller's profile), 021 (a second route pattern and
  a new exact route) and 023 (four session methods).
- **Rules:** a new `safety.md` with narrow `paths` on both sides; `backend.md` (a `/v1/me` write may name its
  target; member reads ask `Blocked`), `profile.md` and `avatar.md` (the block 404; the member screen),
  `mobile.md` (routes, the session's public API), `testing.md` (the `TRUNCATE`).
- **Map and index:** `docs/architecture.md`, `docs/decisions.md`, and `CLAUDE.md` (the table, the dependency
  direction, and reports and blocks in the never-log list).
- **`docs/moderation.md`** (decision 15), with its row in the table of `docs/README.md`.

### 15. The gate after this change, and the manual process (approved, P2)

This change does not lift 031's gate. Decision 033 restates it: no feature that lists, suggests or searches
members ships, and the app is not released to the public, until **all three** hold:

1. blocking and reporting are deployed (this change);
2. the review process of `docs/moderation.md` is established: a named reviewer and a review interval are
   written in it;
3. the feature that lets members meet exposes "Block" and "Report" on other members' profiles (decision 7).

`docs/moderation.md` is a short runbook, read on demand, holding only what is not derivable from the code:

- *Reading:* the SQL that lists reports oldest first, and the one that shows a reported member's reports
  beside their current profile (`reports` joined to `profiles` on `reported_id = user_id`). Run with database
  access, which is the only way a report is ever read.
- *Acting:* the outcomes and their statements: nothing to do; take a profile down (`DELETE FROM profiles`);
  take a picture down (`DELETE FROM avatars`); remove the account (`DELETE FROM users`, which cascades). These
  are 031's manual take-downs, now with a reason to run them.
- *Closing:* a handled report is deleted (`DELETE FROM reports WHERE id = …`). There is no status column; a
  member who reports again creates a new row, which is the wanted behavior.
- *Limitations, stated:* a report holds no copy of what was reported, so the profile may have changed before
  it is read (P7), and a `NULL` reporter means the reporter's account was deleted.
- *Privacy:* a report's content and its reporter are never copied into a ticket, a chat or a log, and never
  disclosed to the reported member.
- *Two blanks, on purpose:* the reviewer and the interval. They are the author's to fill, and the gate's
  second condition is that they are filled. No value is assumed here.

- *Why a document and not a decision record or a rule:* a record is history and a rule loads with code; this
  is an operating procedure someone follows with a database prompt open. `docs/README.md` gains its row.
- *Why no status column to support it:* deleting a handled row needs no schema, and a status would be the
  first piece of the dashboard that is out of scope.

## Risks / Trade-offs

- [Block and Report cannot be reached on a device: the app opens only one's own profile] → Accepted (P1). They
  are built on the screen where they belong and verified by widget tests and, over HTTP, with two accounts.
  The gate requires the next social feature to expose them before its release (decisions 7 and 15).
- [Once the member screen is left, the app holds no id for a blocked member who is absent from the list (they
  blocked the caller too, or have no profile): the block stays, counts toward the 200, and the app offers no
  way to remove it] → Accepted for this change (approved 2026-10-10). The API removes it by id while the
  profile exists; a member with no profile has no id, so that block stays until they have a profile again. The
  member screen's "Unblock" is only an immediate undo (decision 12). A lasting surface is owed with the next
  social feature, as for P1.
- [Reports are stored and nobody reads them] → The gate stays until `docs/moderation.md` names a reviewer and an
  interval (decision 15). Review is manual, by SQL; that is accepted for the MVP.
- [A blocked member can still infer the block: a profile that answered 200 now answers 404, and a second
  account sees it] → Inherent to hiding. No single response says "blocked", and nothing is sent to them.
- [A member at the cap learns more: with 200 blocks, a block of an existing member answers 422 and of an
  unknown id 204] → Accepted: it needs 200 real blocks and reveals what a second account reveals.
- [A blocked member opens a new account] → Not solved here. Accounts need a verified address; a report about
  the new account is the remedy.
- [False or mass reporting] → One report per pair, a per-account limit, and no automatic effect: a report
  changes nothing by itself.
- [The reported member edits their profile before anyone looks: a report holds no snapshot] → Accepted for the
  MVP (P7) and stated in `docs/moderation.md`; the reporter's details are the only record of what was seen.
- [A deleted reporter's details stay, and their text may identify them] → The approved rule; reports are
  reachable only with database access. A retention period is deferred with the tooling.
- [One more query on every member read] → A primary-key lookup, only when the id resolved to another member.
- [An unknown id on a write is silently accepted, so a client bug could go unseen] → The app only sends ids it
  received; the session layer refuses a malformed id before any request.
- [Rolling back the binary stops enforcing blocks] → Stated in the migration plan; the rows stay and apply
  again on redeploy.
- [The two new limits are per process (018)] → As every other limit.
- [The member screen makes a third request, the caller's own profile, on every view] → Small, and it is what
  keeps the public response reader-independent.

## Migration Plan

1. Deploy the backend. Migrations 00009 and 00010 each add a table; the previous binary ignores both, so a
   rollback is a redeploy of it, during which blocks are not enforced and nothing can be reported.
2. Release the app. An older app keeps working: no existing request or response changed.
3. The down migrations drop every block and every report; they are for development only.

## Open Questions

None that change the specs or the tasks. P3 (the store), P4 (details optional, 1000 characters), P5 (200 blocked
members) and P9 (the two allowances) are still the assumed defaults; each is a constant. The reviewer and the
review interval in `docs/moderation.md` are the author's to fill and gate the release, not this change.
