---
paths:
  - "backend/internal/safety/**"
  - "backend/internal/server/safety.go"
  - "backend/internal/server/safety_test.go"
  - "backend/internal/server/members.go"
  - "backend/internal/db/migrations/00009_blocks.sql"
  - "backend/internal/db/migrations/00010_reports.sql"
  - "docs/moderation.md"
  - "mobile/lib/api/blocked_member.dart"
  - "mobile/lib/api/report_reason.dart"
  - "mobile/lib/screens/member_profile_screen.dart"
  - "mobile/lib/screens/blocked_members_screen.dart"
  - "mobile/lib/screens/report_member_screen.dart"
  - "mobile/lib/screens/confirm_dialog.dart"
  - "mobile/test/session_safety_test.dart"
  - "mobile/test/**/*blocked_members*"
  - "mobile/test/**/*report_member*"
  - "mobile/test/**/*member_profile*"
---

# Safety rules: blocking and reporting (`/v1/me/blocks`, `/v1/me/reports/{id}`, both sides)

Records: 033 (backend), 034 (client). The general rules for a member's own resource and for reading another
member are in `backend.md`; the member screen's own rules are in `profile.md`. How a report is read and acted
on, by hand, is `docs/moderation.md`.

## The package

- `internal/safety` owns blocks and reports and imports none of `auth`, `server`, `profile`, `language` and
  `avatar`.
- It names members only by internal user id. A public id is resolved in `server` (`profile.Service.Public`) before
  the call and never passed in, so `safety` cannot store, return or log one.
- A block is one row per direction in `blocks`, keyed by `users.id`: it survives the blocked member's profile
  being removed and saved again, and it goes when either account does.
- A report is one row per reporter and reported member in `reports`, keyed by `users.id` too. It goes with the
  **reported** account (`ON DELETE CASCADE`) and stays, with a NULL reporter, when the **reporter's** goes (`ON
  DELETE SET NULL`). Never make the two foreign keys alike.

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

## The report route

- `PUT /v1/me/reports/{id}` with `{"reason","details"}` → 204 and no body. The report belongs to the caller: the
  reporter is always the session, and the path's `{id}` is the *target's* public id. `reportRequest` lists the
  two fields; any other key is 400, so nothing in a body names a reporter.
- **Reports are write-only.** No route returns a report, a count or whether one exists, to anyone, the reporter
  included, and a report changes nothing any member can request. Never add a `GET` or a `DELETE` under
  `/v1/me/reports`, a field about reports to a response, or anything that acts on a report by itself (hiding
  after N reports, a block). Reports are read with database access only, by the process in `docs/moderation.md`.
- **Order in the handler: decode (400), `safety.ParseReport` (422), resolve the id, `safety.Report`.** The body
  is validated before the id is resolved, so a 422 or a 400 never depends on what the id names. Keep that order.
- An id that names no profile answers 204 with nothing stored, as on the block routes and through the same
  `targetUserID`. Only the caller's own id is told apart, after the body: 422 `member: self`.
- `Report` never asks `Blocked`: a member can report one who blocked them, and one they blocked.
- The reason is one of `safety.Reasons` exactly (the CHECK `reports_reason` lists the same five; a new reason is
  a migration and both lists). Details are optional and at most `safety.DetailsMaxLength` (1000) characters:
  NFC, line breaks as LF, tabs as spaces, trimmed, no control character but LF, no U+FFFD. They are **not** the
  profile's rules: details are never shown to a member, so invisible and bidirectional characters are kept.
  Never render details in the app without revisiting that.
- Only `safety.ParseReport` makes a `ReportContent`; `Report` takes nothing else. One statement (`INSERT … ON
  CONFLICT … DO UPDATE … WHERE the content differs`): a repeat writes and logs nothing, a different reason or
  details replace the pair's one report whole, `updated_at` moves and `created_at` stays.
- A report holds no copy of the reported member's name, text, languages or picture. Don't add a column for one.
- `UserLimits.ReportWrite` is the route's own bucket (the picture's allowance). Refused reports, repeats and
  unknown ids spend it. The body cap (64 KiB) is far above the field limit on purpose: a long paste is 422
  `details: too_long`, not 400.

## Logs

- `block: added` and `block: removed`, with the blocker's `user_id` only and only when a row changed.
- `report: saved`, with the reporter's `user_id` only and only when a row was written or changed.
- Never log whom: not the blocked or reported `user_id`, a public id or a name. Never log a report's reason,
  its details or its id. A read hidden by a block logs nothing.

## Client (034)

- **Reachability:** "Block" and "Report" exist only in the menu of `MemberProfileScreen`, on another member's
  profile, and nothing in the app opens another member's profile yet. A feature through which a member meets
  another must expose both on other members' profiles before it is released (the gate's third condition), and
  must offer a lasting way to remove a block that is not in the list.
- `SessionManager.blockMember(id)`, `unblockMember(id)`, `blockedMembers()` and `reportMember(id, reason,
  details)`: one `_authorized` call each, nothing cached. They rely on the three writes being idempotent
  (`mobile.md`). An id is checked before any request (`ApiPaths.checkMemberId`).
- A block and every unblock are confirmed first (`confirmDialog`); backing out sends nothing. While one is being
  sent its controls are disabled and, on the member screen, leaving is held back.
- **After a block the member screen shows nothing of the profile:** it drops the name, text, languages and
  picture, and the blocked text and its dialog name nobody.
- **"Unblock" in the blocked state is an immediate undo by the route's id.** It never reads `blockedMembers()`,
  and afterwards the screen loads again: the profile, or "isn't available". Don't add "Unblock" to the
  unavailable state, and don't make the blocked text point to "Blocked members": a member who blocked back is
  not listed there.
- `/blocked` (`BlockedMembersScreen`) names nobody in its route, loads on every entry and keeps nothing; an
  unblocked member leaves the list on screen without a reload. It shows names only: the name is the only text
  of a response these screens show. Opened from home.
- `/members/<id>/report` (`ReportMemberScreen`) carries the public id and nothing else: never a reason or
  anything typed. It requests nothing when it opens and shows nothing of the member.
- **The report form's only check is that a reason is chosen.** No rule of the server about a reason or the
  details is repeated; the details are sent exactly as typed, always with both keys. A failure keeps the reason
  and the text.
- **Reporting never blocks,** and nothing in the app says a report exists once its confirmation is left: no
  list, no badge, no "already reported". There is nothing to read (reports are write-only).
- Never print or log a reason, the details, or whom a member blocked or reported. `BlockedMember.toString` is
  redacted.

## The gate (restates 031; not lifted)

No feature that lists, suggests or searches members ships, and the app is not released to the public, until all
three hold: blocking and reporting are deployed; `docs/moderation.md` names a reviewer and a review interval
(both are blank today, and they are the author's to fill: never fill them in or assume a value); and the feature
that lets members meet exposes "Block" and "Report" on other members' profiles (built in the app by 034, and
reachable from nowhere yet).
