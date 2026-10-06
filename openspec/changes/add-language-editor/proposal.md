# Proposal

## Why

A member can see their languages on Profile but cannot change them in the app: nothing calls
`SessionManager.saveLanguages()`, `LanguageRow` is used by no screen and there is no `/profile/languages` route
(decision 030, still a draft). This is mobile step 6 of stage 8: the editor that completes the flow, over the
backend contract of decision 029 and the data, session and widget layers of steps 4 and 5, none of which change
shape.

## What Changes

**In scope**

- A new signed-in screen at `/profile/languages` that loads the catalog and the member's selection, and edits both
  lists ("I speak", "I'm learning"): add a language from a searchable picker, choose and change its level, remove
  it, and move it up or down within its list.
- Save sends the complete selection (both lists, always) through `saveLanguages()`, shows the server's errors
  under the list they name, and returns to Profile on success. No rule of the server is checked in the app.
- Cancel and unsaved-change detection: leaving with changes (Cancel button, app bar back, Android back) asks for
  confirmation; leaving without changes doesn't.
- Loading, failed-load-with-retry and failed-save states, through `presentFailure` as everywhere else.
- The visibility notice ("other members will see your languages") on the editor, above the lists.
- An "Edit languages" button in the Profile Languages section, and the section reloading when the member comes
  back from the editor.
- Value equality on `UserLanguage` and `UserLanguages`, deferred to this step in step 4, to detect changes.
- `LanguageRow` gains optional move-up and move-down buttons.
- Strings, tests, and the documents that would otherwise describe the editor as missing.

**Out of scope**

- Any backend change: routes, bodies, validation, limits and migration stay as decision 029 has them.
- Any change to `ApiPaths`, `AuthApi`, `SessionManager`, `presentFailure`'s mapping or `LanguageChip`.
- Client-side copies of server rules: no minimum, no maximum of five, no duplicate check on save, no check of
  codes or levels.
- Moving a language from one list to the other in one gesture (remove it and add it to the other list).
- Caching the catalog, drag-and-drop reordering, localized language names, suggesting languages from the device
  locale, a "profile complete" gate.
- Stage 8 step 7: marking decision 030 *in force*, the final pass over the documents, and verification on the
  emulator.
- Committing or pushing.

**Decisions that need approval** (reasoning and alternatives in `design.md`)

1. **Reordering is added.** The stage plan left it out ("order is the order added"); this request asks for
   ordering. Proposed as move-up and move-down buttons on each row, not drag-and-drop.
2. **`LanguageRow` is extended** with the two optional move buttons (a step 5 widget, used by no screen so far).
3. **Value equality** on `UserLanguage` and `UserLanguages`, order-sensitive, with `toString` still redacted.
4. **`native` is not offered** in the level picker of "I'm learning". Nothing is checked on save.
5. **Leaving with unsaved changes asks first**; Save is disabled until something changed; back is ignored while
   a save is in flight.
6. **The summary reloads on every return** from the editor, saved or not.
7. **The wording of the visibility notice** and of the discard dialog.
8. **Documents:** this step records the editor's decisions in draft 030 and keeps the rules and the map true;
   the status change to *in force* stays in step 7.

## Capabilities

### New Capabilities

- `language-editor`: the signed-in member edits their own spoken and learning languages in the mobile app: how
  the editor is reached and left, what it loads, how languages are added, levelled, ordered and removed, how a
  save and its failures behave, and how Profile reflects the result.

### Modified Capabilities

None. `openspec/specs/` is empty: this is the project's first change, and the behavior already built (the
backend routes, the Profile summary) is described by decisions 029 and 030, not by a spec.

## Impact

- **New code:** `mobile/lib/screens/languages_screen.dart`, `mobile/lib/screens/language_picker_sheet.dart`.
- **Changed code:** `mobile/lib/router.dart` (route, `signedInRoutes`), `mobile/lib/api/languages.dart`
  (equality), `mobile/lib/ui/widgets/language_row.dart` and its previews, `mobile/lib/screens/language_labels.dart`
  (level descriptions), `mobile/lib/screens/profile_languages_section.dart` (button, reload),
  `mobile/lib/l10n/app_en.arb` and the generated localizations.
- **Tests:** a new `mobile/test/screens/languages_screen_test.dart`; additions to the router, model, row,
  previews, Profile section, accessibility, privacy and leak tests.
- **Documents:** `docs/decisions/030-client-languages.md`, `docs/decisions.md`, `docs/architecture.md`,
  `.claude/rules/languages.md`, `.claude/rules/mobile.md`.
- **API and backend:** none. The editor uses `GET /v1/languages`, `GET /v1/me/languages` and
  `PUT /v1/me/languages` exactly as they are.
- **Dependencies:** none added.
