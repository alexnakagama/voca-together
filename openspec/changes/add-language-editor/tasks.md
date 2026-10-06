# Tasks

Commands run from the repository root. Flutter is not on `PATH` here: pass `FLUTTER=~/develop/flutter/bin/flutter`
to `make mobile-analyze` and `make mobile-test`. Nothing is committed or pushed. Before starting, the decisions
marked "needs approval" in `design.md` must have an answer.

## 1. Model equality

- [x] 1.1 Add `==` and `hashCode` to `UserLanguage` (code and level) and to `UserLanguages` (both lists, in order) in `mobile/lib/api/languages.dart`, leaving `toJson`, parsing and the redacted `toString` as they are; verify with new cases in `mobile/test/api/languages_test.dart` (equal selections, a different level, a different order, an entry in the other list, equal hashes for equal values, `toString` still redacted) passing
- [x] 1.2 Verify `mobile/test/leak_test.dart` and `mobile/test/architecture_test.dart` still pass unchanged

## 2. `LanguageRow` move controls

- [x] 2.1 Add `onMoveUp`, `onMoveDown`, `moveUpLabel` and `moveDownLabel` to `LanguageRow` (`mobile/lib/ui/widgets/language_row.dart`), with the row's controls in their own `Wrap`; the widget still imports neither the models nor the localizations; verify `mobile/test/ui/language_row_test.dart` covers each of the four callbacks, null callbacks disabling their buttons, the labels, and 48 dp targets and contrast in both themes
- [x] 2.2 Verify the row at text scale 2.0 on a 320 dp wide surface, with a long name and all four controls, lays out without overflow and with every control hittable (test in `language_row_test.dart`); if it cannot, stop and report before using the overflow-menu fallback of design decision 1
- [x] 2.3 Update `mobile/lib/ui/previews/language_row_previews.dart` for the new parameters, add a preview of a row with a disabled move, and list it in `mobile/test/ui/previews_test.dart`; verify the previews test passes

## 3. Strings and level descriptions

- [x] 3.1 Add the strings of design decision 11 to `mobile/lib/l10n/app_en.arb`, each with an `@` description, and regenerate the localizations with `flutter pub get` (never edit the generated files); verify `make mobile-analyze` is clean and the generated files contain every new getter
- [x] 3.2 Add `languageLevelDescription` beside `languageLevelLabel` in `mobile/lib/screens/language_labels.dart`, exhaustive over the seven levels; verify with a test that every level has a distinct, non-empty description

## 4. Route and loading

- [x] 4.1 Add `Routes.languages = '/profile/languages'` to `signedInRoutes` and a `GoRoute` for it in `mobile/lib/router.dart`, leaving `authRedirect` untouched; verify the `authRedirect` table in `mobile/test/router_test.dart` gains `/profile/languages`, `/profile/languages?x=1`, `/profile/languages/` and `/profile/languages/es` with the expected results for the three statuses, and the "no redirect loop" test passes
- [x] 4.2 Create `LanguagesScreen` in `mobile/lib/screens/languages_screen.dart` with the load of design decision 4 (both requests together, whole failure, request id, nothing shown for an ended session) and its loading, load-failed-with-retry and loaded read-only layout (notice, both headings, rows by name with the code as the fallback); verify in a new `mobile/test/screens/languages_screen_test.dart` the spec's "Loading the editor" scenarios and the unnamed-code display, with one catalog request and one selection request per load
- [x] 4.3 Add the editor's loading, load-failed, empty and filled states to `mobile/test/screens/accessibility_test.dart` and `mobile/test/screens/privacy_test.dart`; verify both pass, including large text at 320 by 480 dp

## 5. Editing the lists

- [x] 5.1 Create the picker and the level choice in `mobile/lib/screens/language_picker_sheet.dart` (design decision 5) and a larger catalog body in `mobile/test/support/fakes.dart`; verify the spec's "Adding a language" scenarios in `languages_screen_test.dart`: search on name, endonym and code ignoring case, no match, chosen languages left out of both lists' pickers, backing out of either sheet, and a catalog of more than a hundred entries scrolling to its last entry with the keyboard inset set
- [x] 5.2 Wire "Add a language" for both lists (picker, then level, then append) and each row's level control; verify the spec's "Choosing a level" scenarios, including that Native is offered under "I speak" and absent under "I'm learning", and that the current level is marked
- [x] 5.3 Wire move up, move down and remove; verify the spec's "Ordering a list" and "Removing a language" scenarios, including disabled moves at the ends and for a single entry, and a removed language offered again
- [x] 5.4 Add the picker and the level choice as states to `accessibility_test.dart` and `privacy_test.dart`; verify both pass

## 6. Saving

- [x] 6.1 Implement `_save` as in design decision 8 with Save disabled until the selection differs from the loaded one; verify in `languages_screen_test.dart` the exact PUT body (both keys as arrays, in the order shown) after editing one list, with each list empty in turn, with both empty, with a reordered list, with an unnamed code kept, and with more entries than the server allows; and that a successful save returns to Profile
- [x] 6.2 Verify Save availability and the in-flight state: disabled when nothing changed and again after a change is undone, one request for a double activation, every control disabled while a save that never answers is pending, and the timeout message afterwards
- [x] 6.3 Show failures as in the spec's "Save failures": list errors under their list, everything else in the banner, lists kept, messages cleared by any edit, the first list with an error brought into view; verify each 422 code on each list, errors on both lists, a 422 with no known field, 429 with and without `Retry-After`, 503, a network failure, a timeout, a 500, a 200 that fails to parse, a retry sending the same body, and a session that ends mid-save showing no error
- [x] 6.4 Add the save-failed state (banner and a list error) to `accessibility_test.dart` and `privacy_test.dart`, with a response body whose text must not appear on screen; verify both pass

## 7. Cancelling and unsaved changes

- [x] 7.1 Add the Cancel button, `PopScope` and the discard dialog as in design decision 7; verify in `languages_screen_test.dart` that Cancel, the app bar back button and the system back action each close at once with nothing changed, each ask with a change of every kind (add, level, move, remove), "Keep editing" and dismissing keep the lists, "Discard" closes with no PUT sent, back is ignored while a save is pending, and leaving during the load or from the load error asks nothing
- [x] 7.2 Verify the session ending with unsaved changes, with the picker open, with the level choice open and with the discard dialog open, each ends on the log in screen with nothing of the editor left; if a sheet or dialog survives the redirect, close it as described in design "Risks" and re-verify
- [x] 7.3 Add the discard dialog as a state to `accessibility_test.dart` and `privacy_test.dart`; verify both pass

## 8. Profile entry point, and the documents

- [x] 8.1 Add the "Edit languages" button to `ProfileLanguagesSection` (`mobile/lib/screens/profile_languages_section.dart`) in its two loaded states, pushing `Routes.languages` and reloading on return (design decision 9); `ProfileScreen` is not changed; verify in `mobile/test/screens/profile_languages_section_test.dart` the button's presence per state, the new selection shown after a save without reopening Profile, the reload after a cancel, a failed reload leaving the profile form working, and unsaved profile text kept across the visit with no profile PUT
- [x] 8.2 Extend the screens' UI flow in `mobile/test/screens/privacy_test.dart` with opening the editor, picking, a refused save, the discard question and a save; verify nothing is printed, the location stays `/profile/languages` with no query, and a language appears only in the body of the PUT. `mobile/test/leak_test.dart` has no UI flow: it already checks at the session layer that a language code travels only in the body of the save, and must still pass unchanged
- [x] 8.3 Update `docs/decisions/030-client-languages.md` (what the editor does, its states, leaving, ordering, the picker, tests, deferred work; status stays draft with only step 7 pending) and its row in `docs/decisions.md`; verify the record no longer says the editor, its route or the Profile button are missing
- [x] 8.4 Update `.claude/rules/languages.md` (the editor's rules out of "not implemented"; the summary's "loads once" and "`LanguageRow` is used by no screen" lines), `.claude/rules/mobile.md` (`signedInRoutes`, the `saveLanguages()` line) and the client status in `docs/architecture.md`; verify with a search that no document still says the editor is not implemented or that nothing calls `saveLanguages()`

## 9. Integration checks

- [x] 9.1 Run `make mobile-analyze FLUTTER=~/develop/flutter/bin/flutter` and verify it reports no issue
- [x] 9.2 Run `make mobile-test FLUTTER=~/develop/flutter/bin/flutter` and verify every test passes, including `architecture_test.dart`, `leak_test.dart`, `router_test.dart` and `previews_test.dart`
- [x] 9.3 Verify with `git status` and `git diff --stat` that nothing under `backend/` changed and no dependency was added to `mobile/pubspec.yaml`; then run `make vet` and, with the database up (`make db-up`), `make test`, and verify both pass
- [x] 9.4 Run `openspec validate add-language-editor --strict` and verify it passes

## Workflow follow-up

- The user reviews and commits; nothing is committed or pushed by the implementation.
- Stage 8 step 7 follows as its own step: decision 030 to *in force*, the final pass over the documents, and verification on the emulator against `make run`.
- Archive the change (`/opsx:archive`) after the user's review.
