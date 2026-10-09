# 030: Languages in the profile (client stage 8, draft)

> **Status:** draft, implemented. The data, API and session layer, the language widgets, the summary on Profile and
> the editor exist and are tested, and the author ran the app in the Android emulator by hand and confirmed that it
> works. Still pending before it is in force: the final pass over the documents (see the first bullet). No
> two-account check is pending for this record: it concerns a member's own languages, and the connection flows
> such a test needs do not exist yet.
>
> **Backend side:** 029. **Changes** 023: `SessionManager` gained `languageCatalog()`, `languages()` and
> `saveLanguages()`.
>
> **Changed later:** 032 moved the way in to the editor. The summary is on the read-only profile page, has no
> "Edit languages" button and no longer reloads by itself: the page mounts a new one each time it loads. The
> editor is opened from the "Languages" row of the edit screen (`/profile/edit`) and returns there. The two chip
> lists are `ProfileLanguageLists`, shared with the member profile screen. The editor itself is unchanged.
>
> **Current rules:** `.claude/rules/languages.md`.

- **Status: draft, built in three parts.** The decisions below were approved before any client code was written. The
  backend this builds on is complete and described by 029, which this entry does not change.
  - **Implemented (the data, API and session layer):** `lib/api/languages.dart` (`LanguageLevel`, `Language`,
    `UserLanguage`, `UserLanguages`), `ApiPaths.languages` and `ApiPaths.myLanguages`, `AuthApi.languageCatalog`,
    `languages` and `saveLanguages`, the token-free `SessionManager.languageCatalog()`, `languages()` and
    `saveLanguages(UserLanguages)`, and in `presentFailure` the fields `spokenError` and `learningError` with four
    strings.
  - **Implemented (the widgets and the summary):** `LanguageChip` and `LanguageRow` with their previews,
    `languageLevelLabel` and its seven strings, and `ProfileLanguagesSection`, the read-only Languages summary
    that `ProfileScreen` shows below its form. A member sees their languages; described under "What the summary
    does" below.
  - **Implemented (the editor):** `LanguagesScreen` at `/profile/languages`, the picker and the level choice
    (`lib/screens/language_picker_sheet.dart`), the "Edit languages" button of the summary, value equality on
    `UserLanguage` and `UserLanguages`, and the move buttons of `LanguageRow`. A member can change their
    languages in the app; described under "What the editor does" below.
  - **Verified:** the author ran the app in the Android emulator by hand and confirmed that it works. This was a
    manual run; no automated emulator test exists.
  - **Pending:** the final pass over the architecture map and the rule files. The word "draft" is removed then.
- **Scope:** the signed-in user reads the catalog and reads and replaces their own languages, over the contract of
  029. Nothing about other members.
- **No minimum in the client.** `spoken` may be empty, `learning` may be empty, and both may be empty. The app
  adds no rule of its own, such as "at least one of each", before a save.
  - *Why:* the backend requires nothing (029 dropped the plan's `required` rule and left the question to the
    client). A minimum held only by the app would be a rule the API doesn't keep, so the stored data could never
    be relied on to satisfy it, and it would leave a member no way to take back the last language of a list.
    Saving two empty lists is how a member clears their languages (029).
  - This replaces the stage plan's client check "each list has at least one". Client validation stays what 024
    and 028 made it: the server owns every rule (`too_many`, `unknown_language`, `invalid_level`, `duplicate`),
    and the app sends the selection as chosen.
  - If matching later needs a language of each kind, that is a gate to decide then (028 left the profile gate
    open in the same way), not a rule of this editor.
- **The save model is the complete selection, and its JSON always holds both lists.** A requirement for the first
  client step (models, `AuthApi`, `SessionManager`):
  - the model the app saves represents the member's whole selection, both lists, never one list or a change to
    one;
  - its `toJson` **always emits both `spoken` and `learning`, each as an array**; an empty list is `[]`, never
    `null` and never left out;
  - the body of `PUT /v1/me/languages` is never partial: there is no "save only the list that was edited" path,
    and no serializer setting that drops empty or null members may sit between the model and the request.
  - *Why:* 029 refuses a body with a missing or `null` list with 400 `invalid_request` so that a client's mistake
    can't delete a member's languages. From the app that 400 is a generic failure the member can do nothing
    about, so the client must be unable to produce it. With no minimum (above), an empty list is an ordinary
    value the app sends often, which is exactly the case a careless serializer drops.
  - *Tested:* the exact PUT body for a selection with both lists filled, with each list empty in turn and with
    both empty, each showing both keys as arrays (`test/api/languages_test.dart`,
    `test/api/auth_api_languages_test.dart`).
- **A separate editor, and a read-only summary on Profile.** Languages are edited on their own screen at
  `/profile/languages`; the Profile screen shows a read-only "Languages" summary and opens the editor. The
  profile form and its save (028) are not changed, and the two saves stay separate requests, as their resources
  are (029).
  - The route names nobody: like `/profile` it is always the caller's own, and it carries no language code.
  - It joins `Routes.signedInRoutes`; `authRedirect` stays a function of the session status and the path alone.
- **Languages are public by intent, and the app says so.** A member's languages and levels are what discovery
  will show other members (029). The app must tell the member this before they save, as the profile form does for
  the name and the text (028). *Where* it says so (on the editor, on the summary, or by extending the profile's
  existing notice) is decided during the implementation, following the conventions the screens already have.
  Nothing is shown to anyone yet.
- **Carried over from 029 and 023, for the implementation to respect:**
  - `GET /v1/me/languages` always answers 200, so "no languages yet" is two empty lists, not a 404 turned into a
    value as `profile_not_found` is (028).
  - The save is idempotent (029), which is what makes `_authorized`'s single resend after a 401 safe (023), and
    the member's own retry after a timeout or a 503.
  - The token boundary is unchanged: screens reach the routes only through token-free `SessionManager` methods
    and models that hold no token; a member's languages are personal data, so the models redact `toString`.
  - The failure mapping covers the four codes above on `spoken` and `learning`. There is no `required` code.
- **What the implemented layer does**, beyond the requirements above:
  - *Models.* A level is `LanguageLevel`, the seven identifiers of 029 in the scale's order. `UserLanguage` is a
    code and a level; `UserLanguages` holds unmodifiable copies of both lists. A catalog entry is `Language` (code,
    English name, endonym), and names are data from the catalog, never app strings.
  - *Parsing checks the shape and nothing else.* Both lists must be arrays, every string non-empty, every level
    known; the catalog fails as a whole on one bad entry. The client does not re-check what the server validates
    (code format, the maximum per list, duplicates, `native` only in `spoken`): a response is the server's own
    data, and a request is sent as chosen so that every rule stays the server's (024, 028).
  - *Transport.* Each call requires exactly 200. Unlike the profile's `profile_not_found`, no 404 is turned into a
    value: every 404 is an error.
  - *Session.* Each method is one `_authorized` call. Nothing is cached: the session holds tokens only, and the
    screens will load when opened. A catalog cache stays deferred (029).
  - *Failure mapping.* One text per code, the same under either list, with no number in it (the limits live on the
    server only, as 028 decided for lengths). An error belongs to a whole list because the backend names the list,
    not the entry (029).
  - *Tests:* the models (strict parsing, the PUT body in the four cases above, redaction, immutability), the three
    `AuthApi` calls, the three session methods (a stale token, a 401 then one refresh and one resend of the same
    body, a second 401, other failures once, no session, disposed), every new `presentFailure` case, and the leak
    and architecture tests (the bearer on the two new paths, a language code only in the body of the save, the
    models on the screens' import allowlist).
- **A level the app doesn't know is a broken response (accepted behavior).** `UserLanguage.fromJson` accepts only
  the seven identifiers of 029, exactly as written (no trimming, no case folding). Any other level, in either list,
  fails the **whole** response with `ApiProtocolException(malformedBody)`: `languages()` and `saveLanguages()`
  return nothing, not the entries that did parse. `presentFailure` shows it as the generic unexpected failure, as
  it does every protocol failure. The level is never shown, skipped, replaced by a nearby one or kept as raw text.
  - *Why not skip the entry or substitute a level:* the save is a full replace built from what the app holds. A
    selection that silently lacks an entry, or holds it at a level the member didn't choose, would delete or change
    that language on the member's next save, without their knowing. Failing the load makes that impossible: the
    app can't save what it never loaded, and the stored rows stay as they are.
  - *Why not carry an unknown level through as text:* the model would have to hold a level it can neither name nor
    order nor offer in a picker, and every screen would need a case for it, for a state no released backend can
    produce.
  - *When it can happen:* only if the backend gains a level this build doesn't have. 029 fixes the scale and its
    stored numbers, so that is a contract change that needs a client release first, not something an ordinary
    deployment does. A proxy's or a wrong server's body fails the same way, which is the point of strict parsing.
  - *Accepted cost:* if a level were ever added, a member who has it could not open their languages in an older
    build (the load fails with the generic message) until they update; nothing of theirs is lost or changed. The
    response to a save can only hold levels the app itself sent, so in practice this concerns the read. Should a
    save's 200 ever fail to parse, the save is already stored and the member's retry is harmless, because the
    save is idempotent (029).
  - The same holds for anything else off-contract in these bodies (a missing or `null` list, a non-string code).
    A code the *catalog* doesn't name is a different case: it parses, and what the editor shows for it is decided
    with the screens.
- **What the summary does** (`lib/screens/profile_languages_section.dart`):
  - *Its own widget with its own state*, below the profile form. It asks for the catalog (for the names) and the
    selection together, once, when the Profile screen opens, and is shown once the form is; while the profile is
    loading or failed to load, the screen shows only that. A failure of the languages shows an error and a retry
    inside the section: the form, its save and its banners are untouched, and a profile save neither reloads nor
    sends the languages.
  - *The load is whole.* If either request fails, nothing is shown but the error: never some of the languages,
    never "none yet" for a failure. A session that ended shows
    nothing, as on the profile (028).
  - *States:* loading (a labelled spinner); failed (the `presentFailure` text and "Try again"); none chosen (one
    sentence); otherwise "I speak" and "I'm learning", each a wrapping list of chips (name and level) in the
    member's order. A list with no language gets no heading.
  - *A code the catalog doesn't name is shown as the code.* It can't happen against one server (029's foreign
    key), and hiding the entry would show the member fewer languages than they have.
  - *Read-only, with the way in to the editor.* An "Edit languages" button below the languages, shown whenever
    the selection is (none chosen included) and not while it is loading or failed to load. It pushes the editor
    over the profile, so text typed in the profile form is kept, and when the member comes back, saved or not,
    the section asks for the catalog and the selection again (see "What the editor does").
  - *Widgets.* `LanguageChip` and `LanguageRow` take every text as a string and import neither the models nor the
    localizations; `languageLevelLabel` (in `lib/screens/`) is the one place a level becomes text. The chip is not
    a control and is read as one item. The row's level button is labelled with the language as well as the level,
    and when the name and the buttons don't fit side by side (large text, a narrow screen) the buttons move below
    the name. Levels are shown by their short names ("B2", "Native"); the longer descriptions come with the
    editor's level picker.
  - *Tests:* the two widgets (semantics, tap targets, contrast in both themes, large text at 320 dp) and their
    previews; the section (loading, none, both lists, one list, every level, an unnamed code, each kind of failed
    load with the form still saving and a retry, a timeout, a session ending or a logout mid-load, an answer after
    leaving, reopening); and the profile's accessibility and privacy runs with the section in them.
- **The visibility notice goes on the editor.** That is where the member saves, so that is where "before they
  save" is; the summary saves nothing and shows only what the member already chose, and the profile form's notice
  (028) keeps speaking for the name and the text alone. It is the first thing under the editor's title, above
  both lists: "Other members will be able to see the languages you speak and are learning, and your level in
  each."
- **What the editor does** (`lib/screens/languages_screen.dart`, `language_picker_sheet.dart`):
  - *One screen with its own state, like the profile's.* It asks for the catalog and the selection together when
    it opens; the load is whole (loading, or an error with "Try again", or both lists), a late answer is dropped,
    and a session that ended shows nothing. It edits copies of the two loaded lists, so a save can only drop a
    language the member removed. No object outlives the screen, and it shares nothing with the summary.
  - *Layout:* a failed save's message; the notice; "I speak" and "I'm learning", each with its heading, the
    server's error for that list, its `LanguageRow`s and "Add a language"; Save; Cancel.
  - *Adding:* a sheet with a search field over the catalog (name, own name and code, whatever the case), then a
    sheet with the levels, each with a one-line description; the language goes last in its list. Closing either
    sheet adds nothing. The choice is the sheet's result, never part of a route.
  - *Languages already chosen, in either list, are not offered.* A language is in one list only (029), so
    offering it again leads only to a `duplicate` the member must undo. This is not a check: nothing is verified
    on save, and the server's `duplicate` is still shown if it comes.
  - *"Native" is not offered for a language being learned.* The level choice under "I'm learning" is A1 to C2.
    The models and the save still repeat no rule of the server: nothing is checked, and an entry that held
    `native` there would be shown and sent as it is. Offering it would let the member pick a level that is never
    valid and then meet "one of these levels can't be used here", which names neither the entry nor the reason.
    The maximum per list is not treated this way: "Add a language" never disables, because the limit is a number
    the server may change (028, 029).
  - *Ordering.* Each row has "move up" and "move down", which swap it with its neighbour in the same list; the
    first can't go up and the last can't go down. The stage plan had left reordering out ("the order is the
    order added"); it was added with the editor because the first language of a list is the primary one (029)
    and changing it would otherwise mean removing and re-adding languages, levels included. Buttons rather than
    dragging: a list has a handful of rows, a drag competes with the screen's own scrolling, and buttons are
    ordinary labelled 48 dp targets. A language doesn't move between the lists: remove it and add it to the
    other.
  - *Whether anything changed is a comparison.* `UserLanguage` and `UserLanguages` have value equality, with the
    order counting, and "changed" is "the selection on screen differs from the one loaded". A flag set by every
    edit would stay set after an edit is undone. `toString` stays redacted.
  - *Save* is available only when something changed. It sends the complete selection in one request and, on
    200, leaves for the profile. While it runs every control is disabled and back is ignored: leaving then would
    let the summary load before the save is stored. There is no client validation, so no "required" text.
  - *A failed save keeps both lists.* The server's errors for a list (`too_many`, `unknown_language`,
    `invalid_level`, `duplicate`) show under that list and the first is scrolled to; anything else is one message
    above the notice. Any edit clears them all, under both lists, because a language in both is reported on
    `learning` and may be fixed in `spoken`. Retrying sends the same body, safely (the save is idempotent, 029).
  - *Leaving.* Cancel, the app bar's back and the system's back are one path: at once when nothing changed,
    otherwise after "Discard changes?" ("Keep editing" / "Discard"). The profile form has no such question (028);
    the editor holds more work, several taps per language. A session ending is a redirect, not a pop: nothing is
    asked, and an open sheet or dialog goes with the screen.
  - *The summary loads again on every return, not only after a save.* A save whose answer was lost may have been
    stored, after which the member cancels; only a load shows what is stored. It also means the editor passes
    nothing back. If that load fails the summary shows the failure, not the selection it had before. The cost is
    two small requests and a brief spinner after a cancel.
  - *A code the catalog doesn't name* is shown as the code, can be levelled, moved and removed, and is saved
    unless removed. Once removed it can't be added again, since the picker lists the catalog; it cannot occur
    against one server (029's foreign key).
  - *The row at narrow widths.* `LanguageRow`'s four buttons take about 230 dp, so beside a longer name on a
    phone they sit on a second line under it, and at large text they wrap again among themselves. Every button
    stays a 48 dp target.
  - *Tests:* the editor (`test/screens/languages_screen_test.dart`: loading and each failed load, adding, search,
    languages not offered, a catalog of 120 with the keyboard open, levels, ordering, removing, the exact body of
    the save in each case, availability of Save, a double tap, a save that gets no answer, every failure, the
    three ways of leaving with and without each kind of change, the session ending with a sheet or the question
    open, the location, large text at 320 dp); the summary's button and reload; the redirect table; equality of
    the models; the row and its previews; and the accessibility and privacy runs with the editor's states.
- **Deferred:** a catalog cache (two requests per open and per return); dragging to reorder; moving a language
  between the lists in one step; telling the member the limits before a save; per-locale language names.
