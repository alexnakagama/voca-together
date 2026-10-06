# 030: Languages in the profile (client stage 8, draft)

> **Status:** draft, partly implemented. The data, API and session layer, the language widgets and the read-only
> summary on Profile exist and are tested; the editor does not (see the first bullet).
>
> **Backend side:** 029. **Changes** 023: `SessionManager` gained `languageCatalog()`, `languages()` and
> `saveLanguages()`.
>
> **Current rules:** `.claude/rules/languages.md`.

- **Status: draft, in three parts.** The decisions below were approved before any client code was written. The
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
  - **Not implemented (the editor):** the editor screen, its route `/profile/languages` and the button on Profile
    that opens it. `router.dart` knows nothing about languages, nothing calls `saveLanguages()`, `LanguageRow` is
    used by no screen, and a member can't change their languages in the app.
  - **Pending after the editor:** completing this entry (the editor's states and tests, deferred work), the final
    pass over the architecture map and the rule files, and verification on the emulator. The word "draft" is
    removed then.
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
  - *Read-only, and for now without a way in to the editor.* The button that opens the editor is added with the
    editor and its route: a button to a route that doesn't exist would send the member back to home. Until then
    the "none chosen" sentence states the fact and invites nothing.
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
  (028) keeps speaking for the name and the text alone. Its wording and exact place are settled with the editor.
- **Not decided here:** the layout of the editor and the names of its screen and states. They are settled in the
  editor step and written here then.
