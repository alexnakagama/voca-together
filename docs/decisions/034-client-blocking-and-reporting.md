# 034: Blocking and reporting in the client (the member menu, the blocked list, the report screen)

> **Status:** in force. Implemented and tested on the host (OpenSpec change `add-block-and-report`, task groups 4
> to 7). **Not verified on a device, and not reachable there:** the app opens no profile but the caller's own, so
> nothing in it leads to "Block" or "Report" yet (see "Accepted limitations"). No emulator run is recorded here.
>
> **Backend side:** 033. Its gate is not lifted by this record.
>
> **Changes earlier records:** 032 (the member screen has an app bar menu and three actions, reads the caller's
> own profile, and is no longer the only route that names a member), 021 (a second route pattern,
> `/members/<id>/report`, and a new exact route, `/blocked`) and 023 (four new `SessionManager` methods).
>
> **Current rules:** `.claude/rules/safety.md` ("Client"), `.claude/rules/profile.md` ("Client: a member's
> profile"), `.claude/rules/mobile.md`.

- **Scope:** the client side of 033. A member can block another from that member's profile, undo it at once,
  see and unblock the members they blocked, and report a member. Nothing lists, suggests or searches members,
  nothing notifies anyone, and no report is ever shown.
- **Two routes:**

  | Route | Screen | Whose |
  |---|---|---|
  | `/blocked` | `BlockedMembersScreen`: the members the caller blocked, each with "Unblock" | the caller's own; names nobody |
  | `/members/<id>/report` | `ReportMemberScreen`: the form that reports that member | the member whose profile has that public identifier |

  - `Routes.blocked` joined `Routes.signedInRoutes`, which stays a set of exact paths.
  - **A second pattern (amends 021 and 032).** `Routes.memberReport(id)` builds `/members/<id>/report` and
    `Routes.isMemberReport` accepts exactly that for a canonical lowercase UUID, beside `isMember`.
    `authRedirect` is still a function of the session status and the path alone. `/members/abc/report`, an
    upper-case id, `/members/<id>/report/` and `/members/<id>/report/x` go home like any unknown path.
  - *The route carries the identifier and nothing else:* no reason, no text of the form, no name. What a member
    types about another travels only in the body of the report.
  - Both stay flat `GoRoute`s and both are pushed, so back returns to the opener. The report route is not nested
    under the member route: nested, navigating to it would build the profile underneath and load it, and the
    report screen must request nothing when it opens.
  - *A trailing slash in the running app:* `go_router` removes it before the redirect is asked, as for every
    route, so `/members/<id>/report/` shows the same screen for the same identifier. `authRedirect` itself
    refuses the path, which is what the table in `router_test.dart` checks.
- **The member screen learns whether the profile is one's own (amends 032).** Its load adds
  `SessionManager.profile()` to `memberProfile(id)` and `languageCatalog()`; the three fail whole, with "Try
  again". The profile is the caller's own when its `id` equals the caller's; a caller with no profile is never
  the owner.
  - *Why a third request:* the route carries only an identifier, and the menu must not appear on one's own
    profile, which is the only profile the app opens today.
  - *Turned down:* a flag in `GoRouter`'s `extra` (lost when the route is rebuilt, and a second thing a route
    would say); an `is_self` field in the member response (that response is the definition of "public" and must
    not depend on the reader, 031); showing the menu always and letting the server refuse with `member: self`.
  - *Fail whole, not "hide the menu when that load fails":* a member who came to block someone must not find the
    action missing without a word.
- **The menu.** A `PopupMenuButton` in the app bar, with "Report" and "Block" as two separate items, present
  only when a profile loaded and it is not the caller's own: not while loading, on a failure, for an unavailable
  profile or once blocked. It is disabled while a block or an unblock is being sent. Opening it sends nothing.
- **The block flow.** "Block" asks first (`confirmDialog`, the scrolling layout of the profile form's discard
  question); dismissing it sends nothing. Confirmed, `blockMember(id)`.
  - *Stored:* the screen drops the profile it held (name, text, languages, picture, and a picture still loading)
    and shows a text that says the member is blocked. It doesn't leave by itself: back does.
  - *Failed:* the profile stays, with the message under the header: `blocksError` for `blocks: too_many`, which
    says that unblocking someone makes room and states no number, otherwise the mapped message.
  - *Busy:* the menu is disabled and leaving is held back (`PopScope`), as for a picture action. A second
    activation is ignored.
- **Unblock by identifier, from the blocked state: an immediate undo.** The blocked state has an "Unblock"
  button: a confirmation that names nobody (the profile was dropped), then `unblockMember(id)` with the route's
  identifier. It never reads `blockedMembers()`.
  - On success the screen runs its load again, so it shows the profile, or "This profile isn't available" when
    it can't be read (the other member's own block remains, or the profile is gone). That is the answer for any
    identifier and says nothing new. On failure the blocked state stays, with the message.
  - *Why here:* an unblock must need the identifier and nothing else, and this is the one place the app holds
    the identifier of a member it just blocked. The backend is unchanged: the blocker is always the session, so
    only the caller's own block is ever removed (033).
  - *What it is not:* a lasting way to unblock a member who is absent from the list. See "Accepted limitations".
    For that reason the blocked text says "Unblock" undoes it, and does not send the member to "Blocked
    members".
  - *Turned down:* putting such members back in the list (it would name, and confirm a block by, a member
    hidden from the caller, which 033 removed); "Unblock" on every "unavailable" profile (a control that mostly
    does nothing, on a state that must look the same for every identifier).
- **The blocked list (`/blocked`).** `BlockedMembersScreen(session)` calls `blockedMembers()` on every entry and
  keeps nothing: a labelled spinner, a failure with "Try again", "You haven't blocked anyone", or the names in
  the order the server returned them (most recently blocked first), each with "Unblock".
  - *Names only* (033: no picture, no link to the profile). The name is the only text of a response the screen
    shows. Each button is announced as "Unblock <name>".
  - *Unblock* asks first, naming the member, then `unblockMember(id)`. Removed, the member leaves the list held
    on screen **without a reload**; a failure keeps them, with the message above the list. One busy flag for all
    rows: one unblock at a time.
  - *Opened from home,* by a "Blocked members" button. The app has no settings screen, and the profile page is
    about one's public self.
  - *Not paged:* the server's limit of 200 keeps the list in one response (033).
- **The report screen (`/members/<id>/report`).** `ReportMemberScreen(session, id)`: a notice that the report is
  private and the member is not told who reported them, the five reasons as one radio group with none chosen,
  an optional details field, and "Send report". The title is "Report member".
  - *It loads nothing,* so it shows nothing of the member, not even a name, and "Report" on the member screen
    only pushes the route. Nothing is loaded again on the way back: the profile under it is as it was.
  - *A route and not a sheet,* so `authRedirect` stays the only thing that decides where a signed-out user is,
    and the flow is plainly separate from blocking.
  - *One client check:* a reason must be chosen. With none, "Choose a reason for the report." shows under the
    reasons, announced, and nothing is sent; choosing one clears it. No length or character rule of 033 is
    repeated, and the details are sent exactly as typed, with `details` always in the body (empty for none).
  - *Sent:* the form is replaced by a confirmation that says the report was sent and that the member can also
    be blocked from their profile, with "Back to profile". **Reporting never blocks:** no block request is sent
    and the profile is still shown on return (P6 of the proposal).
  - *Failed:* the reason and the text stay. `reasonError` shows under the reasons and `detailsError` under the
    field (`details: too_long` asks for a shorter text and states no number); anything else is a banner above
    "Send report", not at the top as on the other forms: this form is taller than a small screen, and the
    failure belongs where the member just acted. Sending again clears them. `member: self` has no text of its
    own and falls to the generic message: the menu is absent on one's own profile, so the app never sends it.
  - *In flight:* every reason, the field and the button are disabled, leaving is held back, and a second
    activation is ignored (the button's own latch and the handler's `_busy` check).
  - *No discard question on leaving:* the form is one choice and an optional text; 028's question guards a
    profile a member has composed.
  - *The reasons' texts are the app's own* ("Harassment or bullying", "Pretending to be someone else", …); the
    wire codes are `ReportReason.wire` and never shown.
- **Models and the session layer (amends 023).** `lib/api/blocked_member.dart`: `BlockedMember(id,
  displayName)`, strict `fromJson`, redacted `toString`. `lib/api/report_reason.dart`: the enum `ReportReason`
  with its wire code. `ApiPaths.myBlocks`, `myBlock(id)` and `myReport(id)`; the two functions refuse an id that
  isn't canonical. `AuthApi` and `SessionManager` gained `blockMember(id)` and `unblockMember(id)` → `void`,
  `blockedMembers()` → `List<BlockedMember>` and `reportMember(id, reason, details)` → `void`. Each is one
  `_authorized` call and caches nothing. The three writes are idempotent on the server (033), so the resend
  after a 401 is safe. `ApiClient` did not change.
- **Failures.** `presentFailure` gained `reasonError`, `detailsError` and `blocksError` and their five codes.
  Every screen here shows only the app's own localized text, and returns silently when the session ended: the
  router is already leaving.
- **Privacy.** Nothing about a block or a report is printed. A member's identifier travels only in the path of
  the member reads, the block, the unblock and the report, and in the location of the member and report screens.
  A reason and the details travel only in the body of the report and are on screen only while the form is.
- **Accepted limitations:**
  - **"Block" and "Report" are not reachable through navigation.** They are built on the member screen, where
    they belong, and the app opens that screen only for the caller's own profile, where the menu is absent.
    They are verified by widget tests that push the route, and the routes behind them by the backend's tests.
    **The next social feature, the first through which a member meets another, must expose both on other
    members' profiles before it is released,** and verify them on a device; that is the third condition of
    033's gate, and that feature's change owns it.
  - **A blocked member who is absent from the list cannot be unblocked from the app once the member screen is
    left.** The list leaves out a member who has blocked the caller too, and one with no profile (033). The
    block stays, counts toward the 200, and the app then holds no identifier for them. The API removes it by
    identifier while the profile exists; a member with no profile has no identifier, so that block stays until
    they have a profile again. "Unblock" in the blocked state is only an immediate undo. A lasting surface is
    owed with the next social feature.
  - *A third request on every view of a member's profile,* the caller's own profile: small, and it is what
    keeps the public response reader-independent.
- **Tests (host only):** the redirect table with `/blocked` and the report pattern for the three statuses
  (canonical, with a query, and every malformed shape) and no redirect loop; the member screen through the real
  app: the menu's presence and absence, the own-profile read failing whole, the block (backing out, stored,
  each failure, the limit, in flight, a double activation, the session ending) and the unblock by identifier
  (the same cases, the reload, a profile unavailable afterwards, the list never read); the blocked list: every
  loaded shape, each failure and its retry, a load on each entry, the unblock and its failures, in flight;
  the report screen: no request on opening, the five reasons as one choice, "choose a reason" with nothing
  sent, the exact PUT body with the details as typed and with none, each reason's code, the confirmation and
  the return to a profile that was not reloaded and not blocked, `details: too_long` under the field, each
  retryable failure keeping the form, a request that never answers (every control disabled, back ignored, a
  timeout), a double activation, and the session ending mid-request; home's "Blocked members" button; the
  accessibility run (tap targets, labels, contrast in both themes, twice the text size at 320 by 480 dp, an
  open keyboard, and both together for the report form) and the privacy run for every new state, including
  the location of each screen and where the identifier, the reason and the details travel; the two models,
  the four `AuthApi` calls and session methods, `presentFailure`'s new codes; the architecture test (the two
  models, the four public members) and the leak test.
- **Deferred:** exposing "Block" and "Report" where members meet, and a device check of both (the next social
  feature; 033's gate); a lasting way to remove a block that is not listed; a picture or a link to the profile
  in the blocked list, and paging it; editing or withdrawing a report, and any sign in the app that one was
  sent before; a discard question on the report form; more locales.
