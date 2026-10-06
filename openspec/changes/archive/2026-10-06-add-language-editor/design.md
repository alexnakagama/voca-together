# Design

## Context

See `proposal.md` for the motivation. What exists and constrains the approach:

- **Backend (decision 029, unchanged).** `GET /v1/languages` (catalog, ordered by name), `GET /v1/me/languages`
  (always 200; two empty lists mean "none"), `PUT /v1/me/languages` (full replace, idempotent, both lists
  required as arrays, 200 with the stored selection). 422 codes per list: `too_many`, `unknown_language`,
  `invalid_level`, `duplicate` (a code in both lists is reported on `learning`). The server names the list, never
  the entry. Writes are limited per user (burst 10, then 1 per 6 s).
- **Step 4 (unchanged in shape).** `UserLanguages` is the complete selection and its `toJson` always emits both
  lists. `SessionManager.languageCatalog()`, `languages()` and `saveLanguages()` are one `_authorized` call each
  and cache nothing. `presentFailure` already returns `spokenError` and `learningError`.
- **Step 5.** `LanguageRow` (name, endonym, level button, remove button) is built for this screen and unused.
  `ProfileLanguagesSection` loads once when Profile opens and has no way in to an editor.
  `languageLevelLabel` is the one place a level becomes text.
- **Approved for the editor (decision 030, `.claude/rules/languages.md`).** No minimum; always the complete
  selection; its own screen at `/profile/languages`, in `signedInRoutes`; the visibility notice on the editor;
  Profile shows the new selection after a save. 030 leaves "the layout of the editor and the names of its screen
  and states" to this step.
- **Screen conventions (decisions 021, 024, 028).** `StatefulWidget` with ephemeral state only, a `mounted`
  check after every `await`, a `_busy` guard, a request id that drops late answers, every failure through
  `presentFailure`, nothing shown for `FailureKind.sessionEnded`, no state-management package, no session
  status in `lib/screens/`.
- **Where the stage plan and this request differ.** The plan (not in the repository) excluded a reordering UI
  and listed it as out of scope; this request includes ordering. See decision 1.

## Goals / Non-Goals

**Goals:**

- The smallest screen that completes the flow: load, add, level, order, remove, save, cancel.
- No new layer and no new pattern: the editor is a third screen built like `ProfileScreen`.
- The member can never lose stored languages by a client mistake: what was loaded is what is sent back, plus
  the member's own changes.

**Non-Goals:**

- A view model, controller, `ChangeNotifier` or any object that outlives the screen.
- Sharing loaded data between the Profile summary and the editor.
- Explaining server limits in advance (a counter, a disabled "add" at five).

## Decisions

Decisions marked **needs approval** change something earlier steps or the stage plan fixed, or are product
wording. The others follow directly from existing records.

### 1. Reordering with move-up and move-down buttons (needs approval)

Each row gets two icon buttons that swap it with its neighbour in the same list. The first row's "up" and the
last row's "down" are disabled.

- *Why add it at all:* the request asks for ordering, and the order is meaningful (the first language of a list
  is the primary one, 029). Without it, changing the primary language means removing and re-adding entries and
  choosing every level again.
- *Why buttons over drag-and-drop (`ReorderableListView`):* a list has at most a handful of rows; buttons are
  ordinary labelled 48 dp targets that the existing accessibility checks cover as they are; a drag handle
  inside the screen's `SingleChildScrollView` competes with scrolling and needs a nested, shrink-wrapped list;
  and widget tests of a drag are longer and more fragile than taps.
- *Alternative, a per-row overflow menu* ("Move up", "Move down", "Remove"): fewer buttons per row, but it hides
  the remove action behind a second tap. Kept as the fallback if the row can't be made to fit (see Risks).
- *Not included:* moving a language to the other list. Its level may not be valid there (`native`), and
  remove-then-add already does it.

### 2. `LanguageRow` gains the move buttons (needs approval)

`LanguageRow` gets `onMoveUp`, `onMoveDown` (nullable, null disables, like the two it has) and their two labels.
It stays pure UI with every text passed in. The four controls sit in a `Wrap` of their own so they can break
onto a second line at large text.

- *Why change a step 5 widget:* it was written for this screen and nothing else uses it, so there is no caller
  to keep compatible. Wrapping it from the screen instead would put row layout in two places.
- The existing previews and tests are updated, and one preview is added for a row at the end of a list.

### 3. Value equality on the models (needs approval)

`UserLanguage` gets `==` and `hashCode` over code and level; `UserLanguages` gets them over both lists, element
by element and **in order**. `toString` stays redacted.

- *Why:* "has the member changed anything" is exactly "does the selection on screen equal the one loaded", and
  order is part of the selection. Step 4 deferred equality to this step for that reason.
- *Alternative, a `dirty` flag set on every edit:* it stays set after an edit is undone (move down, move up), so
  the member is asked to discard changes that don't exist. A private comparison helper in the screen works too,
  but tests of the save body and of the models want the same comparison.
- `Language` (a catalog entry) gets none: nothing compares catalog entries.

### 4. Screen state, and nothing above it

`LanguagesScreen` in `lib/screens/languages_screen.dart` takes only the `SessionManager`. Its `State` holds:

| Field | Meaning |
|---|---|
| `_loading`, `_loadError`, `_request` | the load, as in `ProfileScreen` |
| `_catalog` (`List<Language>`), `_byCode` | the picker's source and the rows' names |
| `_loaded` (`UserLanguages?`) | the selection as last loaded: the baseline |
| `_spoken`, `_learning` (`List<UserLanguage>`) | the working copies the member edits |
| `_busy` | a save is in flight |
| `_banner`, `_spokenError`, `_learningError` | what the last failed save showed |

- The selection to save is always built the same way, `UserLanguages(spoken: _spoken, learning: _learning)`,
  and passed whole to `saveLanguages()`. There is no path that sends one list (030).
- `_changed` is a getter: that selection `!=` `_loaded`. Nothing else tracks changes.
- The working lists start as copies of what was loaded, including entries whose code the catalog doesn't name,
  so a save can only drop a language the member removed.
- Every edit (add, level, move, remove) is one `setState` that also clears `_banner` and both list errors. Both
  are cleared, not only the edited list's, because `duplicate` across the lists is reported on `learning` and
  may be fixed in `spoken`.
- The load mirrors `ProfileLanguagesSection._load`: both requests started together, `Future.wait`, whole
  failure, `sessionEnded` shows nothing.

*Alternative, a controller class holding the selection:* it would be the first such object in the app, and the
rule is that screens hold ephemeral state themselves. The screen is larger than `ProfileScreen` but has the
same shape.

### 5. The picker and the level choice are two modal sheets

In `lib/screens/language_picker_sheet.dart`: two functions that show a sheet and complete with the choice or
null (`Future<Language?>`, `Future<LanguageLevel?>`).

- *Picker:* a scroll-controlled modal sheet with a search field and a lazily built list over the catalog entries
  not already chosen. Filtering is a case-insensitive "contains" on name, endonym and code. The screen passes
  the available entries; the sheet knows nothing about the session.
- *Level choice:* a short sheet, one row per level: the short label from `languageLevelLabel` and a new
  one-line description from `languageLevelDescription` (same file, same pattern). The screen passes the levels
  to offer and the current one.
- Adding is picker, then level, then append. A null from either adds nothing. After each `await` the screen
  checks `mounted`.
- The choice travels as the sheet's result, an object in memory. No code or level goes in a route.
- *Why sheets and not routes:* a route for the picker would need the "which list" in its path or state, and the
  result would have to come back through the router.
- *Why hide chosen languages:* a language can be in one list only (029), so offering one already chosen leads
  only to a server `duplicate` error the member must then undo. Hiding it is not a validation step: nothing is
  checked on save, and a `duplicate` from the server is still shown if it ever comes.

### 6. `native` is not offered under "I'm learning" (needs approval)

The level choice for the learning list is A1 to C2. Nothing is checked on save, and an entry already holding
`native` there (the database forbids it) would be shown and sent as it is.

- *The tension:* `.claude/rules/languages.md` says no server rule is repeated in the client, and names "`native`
  only for spoken" among them. That rule is about the models and the save: the selection is sent as given. The
  stage plan specified the picker this way.
- *Why offer less rather than everything:* with `native` offered, the member picks it, saves and gets
  "One of these levels can't be used here", which doesn't say which entry or why. The picker is the only place
  the app can prevent a choice that is never valid.
- *Alternative:* offer all seven under both lists and let the server refuse. Simpler and literally faithful to
  the rule, at the cost of that dead end.
- The maximum per list is **not** treated the same way: "Add a language" never disables, because the limit is a
  number the server may change (029, 028) and its error text needs no number.

### 7. Leaving, unsaved changes and a save in flight (needs approval)

- The screen wraps its content in `PopScope(canPop: !_changed && !_busy)`. The app bar's back button and
  Android back both go through `Navigator.maybePop`, which honours it (checked in go_router 18.0.2:
  `GoRouterDelegate.popRoute` calls `maybePop`). The Cancel button calls the same `maybePop`, so the three are
  one path.
- A refused pop while `_busy` does nothing. A refused pop otherwise shows an `AlertDialog`: "Discard changes?",
  "Keep editing", "Discard". "Discard" leaves with `context.pop()`, which calls `Navigator.pop` and is not
  subject to `PopScope` (`GoRouterDelegate.pop`).
- A successful save leaves with `context.pop()` too, so it needs no flag to get past `PopScope`.
- If nothing is below the editor (it is only ever pushed from Profile, and deep links are disabled), leaving
  goes to `Routes.profile` instead of popping.
- A redirect is not a pop: when the session ends, `authRedirect` replaces the stack and `PopScope` is not asked,
  so no dialog appears. An open sheet or dialog must go with it; that is a test, not an assumption (see Risks).
- *Save is disabled until `_changed`:* an unchanged save is a write the member doesn't need (and one of the ten
  in the limiter's burst), and a disabled Save doubles as the "nothing to save" indicator. After a failed save
  the selection still differs from the baseline, so Save stays enabled for the retry.
- *Back is ignored during a save:* if the member left mid-save, the summary's reload could be answered before
  the save is stored and show the old selection. The wait is bounded by the client's request timeout.
- *Alternative, no confirmation* (as on the profile form, which has none): the editor holds more work than two
  text fields, several taps per language, and the request asks for unsaved-change detection.

### 8. Save and its failures

```
_save: if (_busy || !_changed) return
       _busy = true; clear banner and list errors
       stored = await session.saveLanguages(UserLanguages(spoken, learning))
       not mounted → return
       success → context.pop()
       failure → presentFailure; sessionEnded → return
                 _banner = message; _spokenError; _learningError; _busy = false
                 bring the first list with an error into view (after the frame)
```

- A list's error is a `FormErrorBanner` directly under that list's heading (a live region, like every other
  error); `_banner` is one above the notice.
- The stored selection returned by a successful save is not used: the editor closes and Profile reloads
  (decision 9). No "saved" banner is shown on a screen that is already gone.
- A timeout or a 503 leaves the lists and Save as they are. The retry sends the same body, which is safe
  because the save is idempotent (029), as is `_authorized`'s single resend after a 401 (023).
- A 200 that fails to parse is the generic unexpected failure (030); the retry is equally safe.
- No client validation exists to fail before the request, so the screen has no "required" strings.

### 9. Profile: the way in, and reloading on every return (needs approval)

`ProfileLanguagesSection` gets a `SecondaryButton` "Edit languages" below its content in the two loaded states
(languages shown, none chosen). Its handler is:

```
await context.push(Routes.languages)
if (!mounted) return
_retry()   // the existing spinner-then-load
```

- *Why the section owns the button:* it is the section's control, and the section's state is what must be
  refreshed. `ProfileScreen` is not touched.
- *Why reload on every return and not only after a save:* a save whose answer was lost (timeout) may have been
  stored, after which the member cancels; only a reload shows the truth. It also needs no result passed back
  through the navigator, so the editor and the summary share nothing. The cost is two small GETs and a brief
  spinner after a cancel.
- *Alternative:* the editor pops with the stored `UserLanguages` and the section shows it without a request. It
  couples the two widgets through a navigator result and is wrong in the lost-answer case.
- The section's rule stays true: it calls only `languageCatalog()` and `languages()` and never saves. The
  "loads once when Profile opens" wording in the rules becomes "when Profile opens and on each return from the
  editor".
- Not shown while loading or failed: the editor would very likely fail the same way, and the retry is right
  there.
- Profile stays mounted under the pushed editor, so text typed in the profile form is kept.

### 10. Route

`Routes.languages = '/profile/languages'`, added to `signedInRoutes`, with a top-level `GoRoute` whose builder
passes only `session`. `authRedirect` is unchanged: it already matches paths exactly, so
`/profile/languages/` and `/profile/languages/es` go home like any unknown path. The route is top-level, not a
child of `/profile`, as `/profile` is not a child of `/home`: the back stack comes from `push`.

### 11. Layout and strings (wording needs approval)

Top to bottom in the loaded state: the save banner if any; the visibility notice; "I speak" (heading, its error
if any, its rows, "Add a language"); "I'm learning" (the same); Save (`PrimaryButton`, busy while saving);
Cancel (`SecondaryButton`). An empty list shows only its heading and "Add a language". The scaffold, padding
and maximum width are those of `ProfileScreen`. The list headings reuse `languagesSpokenHeading` and
`languagesLearningHeading`.

New strings in `app_en.arb`, each with a description:

| Key | Text |
|---|---|
| `languagesEditButton` | Edit languages |
| `languagesEditorTitle` | Your languages |
| `languagesVisibilityNotice` | Other members will be able to see the languages you speak and are learning, and your level in each. |
| `languagesAddButton` | Add a language |
| `languagesSaveButton` | Save |
| `languagesCancelButton` | Cancel |
| `languagesDiscardTitle` | Discard changes? |
| `languagesDiscardMessage` | Your changes to your languages haven't been saved. |
| `languagesDiscardConfirm` | Discard |
| `languagesDiscardKeep` | Keep editing |
| `languagePickerTitle` | Choose a language |
| `languagePickerSearchLabel` | Search |
| `languagePickerNoMatch` | No language matches your search. |
| `languageLevelPickerTitle(name)` | Your level in {name} |
| `languageLevelSemantics(name, level)` | {name}, level {level} |
| `languageRemove(name)` | Remove {name} |
| `languageMoveUp(name)` | Move {name} up |
| `languageMoveDown(name)` | Move {name} down |
| `languageLevelDescriptionA1` … `C2`, `Native` | Beginner, Elementary, Intermediate, Upper intermediate, Advanced, Proficient, Native speaker |

The loading label (`languagesLoading`), `tryAgain` and the four list errors already exist.

### 12. Tests

In the project's existing style: the real app over `FakeServer` through `test/screens/harness.dart`.

- `test/api/languages_test.dart`: equality and hash of both models (same entries, different level, different
  order, different list), `toString` still redacted.
- `test/ui/language_row_test.dart`, `language_row_previews.dart`, `test/ui/previews_test.dart`: the four
  callbacks, disabled moves, labels, 48 dp targets, contrast, large text at 320 dp.
- `test/router_test.dart`: the `authRedirect` table gains `/profile/languages`, `/profile/languages?x=1`,
  `/profile/languages/` and `/profile/languages/es`; the routing group reaches the screen.
- `test/screens/languages_screen_test.dart` (new): one test or more per scenario of the spec, including the
  exact PUT body in each save case, a catalog of more than a hundred entries, a save that never answers, and the
  session ending with a sheet or the dialog open.
- `test/screens/profile_languages_section_test.dart`: the button in each state, the reload after a save, after
  a cancel and when it fails, profile text kept across the visit. "Asks once when the screen opens" stays true
  for opening.
- `test/screens/accessibility_test.dart` and `privacy_test.dart`: a case for each editor state (loading, load
  failed, empty, filled, picker, level choice, save failed, discard dialog).
- `test/leak_test.dart`: the UI flow includes an edit and a save; the language marker appears only in the body
  of the PUT and in no location.
- `test/support/fakes.dart`: a larger catalog body for the picker tests.
- `test/architecture_test.dart` needs no change (the screen imports only allowed libraries); it must still
  pass.

Verification is `make mobile-analyze` and `make mobile-test`. The backend is untouched, so `make vet` and
`make test` are run once at the end only to confirm that.

### 13. Documents (needs approval)

- `docs/decisions/030-client-languages.md`: its "not implemented" and "not decided here" parts are replaced by
  what the editor does (states, leaving, ordering, the picker, tests, deferred work). It stays a **draft**:
  flipping it to *in force*, the final pass and the emulator run are step 7. No record 031: 030 itself says the
  editor's layout and states are "settled in the editor step and written here then", and it is not yet in force.
- `.claude/rules/languages.md`: the "not implemented" section becomes the editor's rules; the summary's lines
  about loading once and `LanguageRow` being unused are corrected. The new files match the existing `paths`
  pattern `mobile/lib/**/*language*`.
- `.claude/rules/mobile.md`: `signedInRoutes` and the "no screen calls `saveLanguages()` yet" line.
- `docs/architecture.md`, `docs/decisions.md`: the client status rows.

Leaving these for step 7 would have the rules describe a missing editor while the code has one, which
`docs/README.md` rules out.

## Risks / Trade-offs

- [The row's four controls don't fit at text scale 2.0 on 320 dp: about 144 dp of icon buttons plus the level
  button in about 272 dp of content width] → the controls are a `Wrap` that breaks onto a second line; the
  large-text test at 320 dp is written with the row change, not after. If it still can't be laid out cleanly,
  fall back to the overflow menu of decision 1 and report it before continuing.
- [A sheet or dialog stays on screen after the session ends] → a test ends the session with each of them open
  and expects the log in screen alone. If go_router leaves one behind, the screen closes them itself when its
  load or save reports `sessionEnded`, without reading the session status.
- [`PopScope` and go_router behave differently than read from the source, in a later version] → the three ways
  of leaving are each tested with and without changes; nothing relies on the behavior untested.
- [Server rules surface only on save: the sixth language is refused after it was added] → accepted and already
  decided (028, 030): limits live on the server. The error says what to do without a number.
- [An unnamed code can't be re-added once removed, because the picker lists the catalog only] → accepted: it
  cannot occur against one server (the foreign key of 029), and removing is the member's own act.
- [The summary shows a spinner after a cancel] → accepted for correctness in the lost-answer case (decision 9).
- [Back is ignored for up to the request timeout during a save] → accepted; the Save button shows it is working.
- [Reloading the summary costs two GETs per return, and the editor two per open] → accepted; a catalog cache
  stays deferred (029, 030).
- [`languages_screen.dart` is the largest screen file] → the sheets live in their own file; private row and
  section widgets inside the screen file keep `build` readable.

## Migration Plan

Client only. No backend, database or API change, and nothing to migrate. Rollback is reverting the change: the
summary goes back to read-only and stored languages are untouched.

## Open Questions

None that change the specs or the tasks. The items marked "needs approval" above are proposed with a
recommendation; a different answer to 1, 6, 7 or 9 changes the matching requirement and its tasks, so they are
to be settled before `/opsx:apply`.
