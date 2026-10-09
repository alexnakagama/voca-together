# Proposal

## Why

Decision 031 made every saved profile and picture readable by any signed-in member and accepted, as a **hard
gate**, that no feature that lists, suggests or searches members ships before reporting and blocking exist. Until
then a member has no way to stop another from seeing them or to tell us about abuse, and taking something down is
a manual `DELETE`. This change builds blocking and reporting and writes down how a report is reviewed and acted
on. It is necessary for lifting the gate and not sufficient: the gate stays until that process is in operation.

## What Changes

**In scope**

- **Blocking.** A signed-in member blocks and unblocks another member, named by the profile's public identifier:
  `PUT` and `DELETE /v1/me/blocks/{id}`, both idempotent, both 204.
- **A block hides both members from each other (approved).** While a block exists in either direction, each
  member's profile and picture answer the other with the same 404 `profile_not_found` as an identifier that names
  nobody. The check is on the backend, in the two member routes, through one "is there a block between these
  two" question that every later feature (friend requests, Discover, search, chat) must ask.
- **A list of the members one has blocked:** `GET /v1/me/blocks` (public identifier and name, newest first), and a
  "Blocked members" screen with "Unblock", since a blocked member's profile can no longer be opened.
- **Reporting.** A member reports another for one of five reasons, with optional details:
  `PUT /v1/me/reports/{id}` with `{"reason","details"}`, 204. Reasons (approved): `harassment`,
  `inappropriate_content`, `spam`, `impersonation`, `other`.
- **One report per reporter and reported member (approved).** The same content again changes nothing; a different
  reason or details replaces the earlier report.
- **Reports are write-only.** No route returns a report, to anyone. The reported member is never told, nothing
  they can request changes, and nothing about a report is logged but the reporter's account id.
- **No self-block and no self-report,** refused by the server and not offered by the app.
- **Account deletion (approved):** blocks go when either account does; reports about a deleted member go with
  them; a report whose reporter is deleted stays, with no reporter.
- **Limits:** two new per-user write limits (blocks; reports) and a cap on how many members one may block.
- **Mobile:** "Block" and "Report" in a menu on the member profile screen, each its own flow (a confirmation for
  blocking; a screen with the reasons and a details field for reporting); the "Blocked members" screen, opened
  from home; strings in `app_en.arb`.
- **A manual moderation process,** documented in a new `docs/moderation.md`: how reports are read (SQL), what
  action each outcome takes (the manual `DELETE`s of 031), how a report is closed, and the known limitation
  that reported content may have changed. The reviewer's name and the review interval are left for the author.
- Two migrations, a new backend domain package, decision records, rules, the architecture map, tests on both
  sides.

**Out of scope**

- User search, friend requests, chat, notifications, Discover.
- A moderation dashboard or any route that reads reports; report status, assignment or an answer to the reporter.
  Reports are read with SQL, and a take-down stays the manual `DELETE` of 031, as `docs/moderation.md` describes.
- A snapshot of what was reported (the name, text or picture at the time).
- Any entry point that opens another member's profile; notifying anyone of a block or a report.
- Blocking or reporting a member who has no profile: they have no public identifier and nothing of theirs is
  reachable.
- An account-deletion endpoint (only the foreign keys are decided here); deep links; more locales.
- Committing or pushing.

**Deviations from existing conventions** (reasoning in `design.md`)

1. **A route under `/v1/me` carries another member's identifier,** which `backend.md` forbids for a member's own
   resource. The block and the report still belong to the caller and are selected by the session; the identifier
   names their *target*. `/v1/profiles/{id}` stays GET-only.
2. **A write for an identifier that names no profile answers 204, not 404.** Otherwise a blocked member could
   confirm the block with one request: their read says 404, a write would say something else.
3. **The member profile response depends on who reads it** (a 404 for a blocked pair). 031's "one 404" gains one
   more cause; the body and headers stay identical.
4. **The member profile screen reads the caller's own profile,** only to know whether the profile shown is the
   caller's, which 032 ruled out.

**Decisions approved** (1 to 4 before the proposal was written, 5 to 10 at its review)

1. A block hides both ways, and unblocking is done from a list.
2. One report per pair, replaced by a later one.
3. On deletion: reports follow the reported member; a deleted reporter leaves the report without a reporter.
4. The five reasons above; details are optional free text.
5. **Reachability (P1).** "Block" and "Report" are not reachable through normal navigation at this stage: the
   app opens only one's own profile, and discovery and friend requests do not exist. That is accepted. The next
   feature through which a member meets another (Friends, Discover, search, chat) **must expose both actions on
   other members' profiles before it is released**; this is written into the gate.
6. **The gate is not lifted by this change (P2).** The endpoints existing is not enough. This change documents a
   minimal manual process for reviewing reports and acting on them (`docs/moderation.md`); a dashboard stays out
   of scope. The gate of 031 is kept, on listing features and on the public release, until that process is
   established: a named reviewer and a review interval, both filled in by the author.
7. **Reporting never blocks by itself (P6).** Two separate actions.
8. **No snapshot of what was reported (P7).** A report holds a reason and optional details and nothing of the
   profile. The limitation, that the content may change before anyone reviews it, is documented.
9. **The blocked list shows names only (P8)** and omits a member whose profile no longer exists; the block
   still holds.
10. **The blocked member is not notified (P10)** and is shown nothing but "this profile isn't available".

**Still assumed, not yet confirmed** (constants and one reading; changing any of them is a one-line change)

| # | Question | Default assumed |
|---|---|---|
| P3 | "App Store release": the client is Android only. | Read as the first store release (Google Play). Nothing here is iOS-specific. |
| P4 | Is a report's free text required for `other`? Its length? | Always optional; at most 1000 characters. |
| P5 | How many members may one member block? | 200. More answers 422 until one is unblocked. It keeps the list one small response with no paging. |
| P9 | The allowances. | Blocks and unblocks: burst 10, then 1 every 6 s. Reports: burst 5, then 1 a minute. |

## Capabilities

### New Capabilities

- `member-blocking`: blocking and unblocking another member, who may do it and to whom, what a block hides and
  from whom, the list of blocked members, how blocks behave under repeats, concurrency and account deletion, and
  the block flow and "Blocked members" screen in the app.
- `member-reporting`: reporting another member: the accepted reasons and details, one report per pair, that
  reports are stored and never returned, how the reporter's identity is protected, what happens on account
  deletion, and the report flow in the app.

### Modified Capabilities

- `member-profile`: an unavailable profile now includes one hidden by a block in either direction; the rule that
  no route naming a member can change anything is narrowed to "nothing of that member's", since the caller's own
  block and report routes name their target.
- `profile-avatar`: a member's picture is not served across a block; the answer is the 404 of an unknown profile.

## Impact

- **API (additive):** four new routes under `/v1/me`. `GET /v1/profiles/{id}` and its `/avatar` keep their
  contract and gain one cause of 404. No new error code; two new field codes (`member: self`,
  `blocks: too_many`) and the report's (`reason`, `details`).
- **Database:** migration 00009 adds `blocks`, migration 00010 adds `reports`.
- **Backend code:** a new `internal/safety` (blocks and reports); `internal/profile` (public profiles of several
  users, for the list); `internal/server` (the four routes, the block check in `members.go`, two per-user
  limits, thirteen limiters in all); `cmd/api/main.go` (wiring); `internal/testutil` (the `TRUNCATE`).
- **Mobile code:** `lib/api/` (paths, calls, two models), `lib/session.dart` (four methods), `lib/router.dart`
  (two routes), `lib/screens/` (the member screen's menu, a report screen, a blocked-members screen, the home
  entry, `presentFailure`), strings. No new dependency on either side and no transport change.
- **Documents:** decision records 033 (backend) and 034 (client) with header notes on 018, 021, 023, 031 and 032;
  a new `.claude/rules/safety.md`; `backend.md`, `profile.md`, `avatar.md`, `mobile.md`, `testing.md`;
  `docs/architecture.md`; `docs/decisions.md`; `CLAUDE.md`; a new `docs/moderation.md` with its row in
  `docs/README.md`.
- **Release:** this change does not make the app releasable to the public by itself; the gate above holds.
- **Deployment:** both migrations add a table, so the backend ships first and an older client keeps working. A
  rollback is a redeploy of the previous binary, which ignores both tables: blocks stop being enforced until the
  new binary is back.
