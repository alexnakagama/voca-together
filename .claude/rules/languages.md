---
paths:
  - "backend/internal/language/**"
  - "backend/internal/server/languages.go"
  - "backend/internal/server/languages_test.go"
  - "backend/internal/db/migrations/00006_languages.sql"
  - "mobile/lib/**/*language*"
  - "mobile/lib/api/api_paths.dart"
  - "mobile/lib/api/auth_api.dart"
  - "mobile/lib/session.dart"
  - "mobile/lib/screens/failure_presentation.dart"
  - "mobile/lib/l10n/app_en.arb"
  - "mobile/test/**/*language*"
  - "mobile/test/screens/failure_presentation_test.dart"
  - "mobile/test/leak_test.dart"
  - "mobile/test/support/fakes.dart"
---

# Languages rules (`GET /v1/languages`, `GET`/`PUT /v1/me/languages`, both sides)

Records: 029 (backend, in force), 030 (client, **draft**: everything in it is implemented, the editor included, and the
author confirmed it by hand on the emulator; the final pass over the documents is pending), 032 (where the summary sits and what opens the
editor). The general rules for a member's own resource are in `backend.md`.

## Backend (implemented)

- `internal/language` owns the catalog and members' languages and imports none of `auth`, `server` and `profile`.
  Nothing in a route, query or body names a user, a `kind` or a `position`.
- Never log a member's languages: no codes, levels or counts. They are personal data, public by intent.
- Other signed-in members read a member's languages only inside `GET /v1/profiles/{id}` (decision 031, the pattern
  in `backend.md`), which calls `language.Service.Get` with the owner's `UserID`. The language routes themselves
  stay the caller's own.
- `PUT /v1/me/languages` replaces the whole selection, so its body must hold both `spoken` and `learning` as arrays;
  only `[]` clears a list, and both may be empty (nothing is required). A missing or `null` list is 400
  `invalid_request` (`languagesRequest.complete`, in `server`): never read one as an empty list, which would delete
  rows.
- A save is one transaction that locks the `users` row `FOR NO KEY UPDATE` first (the lock order in `auth.md`);
  keep that order in anything added to it.
- Codes and levels are identifiers, not text: never trim or lowercase them. `language.Level`'s numbers are the ones
  stored: never renumber them.
- The `languages` catalog changes only by a new migration with `INSERT`s, so every environment has the same one.
  `testutil.DB` never truncates it.

## Client: data, API and session layer (implemented)

`lib/api/languages.dart` (`LanguageLevel`, `Language`, `UserLanguage`, `UserLanguages`), `ApiPaths.languages` and
`myLanguages`, `AuthApi` and `SessionManager` (`languageCatalog()`, `languages()`, `saveLanguages()`), and the
language cases of `presentFailure` (in `lib/screens/failure_presentation.dart`).

- `UserLanguages` is the member's complete selection, never one list or a change to one. Its `toJson` always emits
  both `spoken` and `learning` as arrays (`[]` when empty, never `null`, never left out), and it is the whole body
  of the PUT: put nothing between it and the request that could drop a key. Its lists are unmodifiable copies.
- `UserLanguage` and `UserLanguages` have value equality, and the order of a list counts: the editor's "anything
  changed?" is this comparison. Keep `==` and `hashCode` in step with the fields.
- No rule of the server is repeated in the client: no minimum (either list, or both, may be empty), no maximum, no
  duplicate check, no "`native` only for spoken", no code format. The selection is sent as given.
- Parsing checks the shape only, and strictly: both lists must be arrays, every code a non-empty string, every
  level one of the seven identifiers exactly (no trimming, no case folding). Anything else, a level this app
  doesn't know included, fails the whole response as `ApiProtocolException(malformedBody)`. Never skip an entry or
  substitute a level: the next save replaces everything and would delete or change it (030).
- `GET /v1/me/languages` always answers 200: "none yet" is two empty lists. There is no 404-to-empty mapping as
  the profile has; every 404 stays an error.
- Each `SessionManager` method is one `_authorized` call and caches nothing. The resend after a 401 is safe only
  because the save is idempotent (029).
- `UserLanguage` and `UserLanguages` redact `toString`: a member's languages are personal data. A code or a level
  never goes in a log, an exception or a route.
- `presentFailure` maps `too_many`, `unknown_language`, `invalid_level` and `duplicate` on `spoken` and `learning`
  to `spokenError`/`learningError`, one text per code for both lists, stating no number. There is no `required`
  code. The backend names the list, not the entry.

## Client: widgets and the Profile summary (implemented)

`lib/ui/widgets/language_chip.dart` and `language_row.dart`, `lib/screens/language_labels.dart`
(`languageLevelLabel`, `languageLevelDescription`), `lib/screens/profile_languages_section.dart`
(`ProfileLanguagesSection`, mounted by the read-only profile page once the profile has loaded) and
`lib/screens/profile_language_lists.dart` (`ProfileLanguageLists`, the two chip lists, which the member profile
screen shows too).

- The two widgets are pure UI and take every text as a string: they import neither the models nor the
  localizations. A level becomes text only through `languageLevelLabel` and `languageLevelDescription`, in
  `lib/screens/`.
- `LanguageRow` is the editor's row. Its buttons wrap under the name, and among themselves, when they don't fit:
  keep both `Wrap`s, and every button a 48 dp target.
- The summary is read-only and calls only `languageCatalog()` and `languages()`, together, once. It never saves,
  holds no selection for a save, and has no control that opens the editor (032).
- It doesn't reload by itself: the profile page mounts a new summary (a key per load) each time it loads, which
  is on opening and on every return from the edit screen, saved or not. So a failed reload shows the error, never
  languages that may no longer be stored.
- Its state is its own: a failed load shows an error and a retry inside the section, and the rest of the page
  doesn't depend on it. Don't merge the two loads or their errors.
- `ProfileLanguageLists` shows a selection that was already loaded and requests nothing. Its "none chosen" text
  is passed in, because it differs between one's own page and another member's profile.
- The load fails whole: if either request fails, no language is shown (never a partial list, never "none yet").
- A code the catalog doesn't name is shown as the code, so every language the member has stays visible.
- A list with no language gets no heading; with both empty, one "none yet" text.

## Client: the editor (implemented)

`lib/screens/languages_screen.dart` (`LanguagesScreen`, at `Routes.languages` = `/profile/languages`) and
`lib/screens/language_picker_sheet.dart` (`showLanguagePicker`, `showLanguageLevelPicker`).

- No minimum and no maximum in the editor: add no "at least one" check and never disable "Add a language"; saving
  two empty lists clears the selection. Nothing is checked before a save.
- The editor always saves the complete selection, both lists, whichever one was edited, built in one place
  (`_selection`). Its working lists start as copies of what was loaded, so a save drops only what the member
  removed; a code the catalog doesn't name is shown as the code and kept.
- The route names nobody and carries no language code. A picked language or level travels as the sheet's result,
  never through the router.
- The editor is opened only from the "Languages" row of the profile edit screen (032), and leaves to whatever
  opened it with `context.pop()`: the edit screen is still there under it, with what was typed.
- Languages are public by intent: the notice is on the editor, above the lists, where the save is. The summary
  carries no notice.
- The pickers offer less, they check nothing: a language already in either list is not offered, and "Native" is
  not offered under "I'm learning" (030). Don't turn either into a check on save.
- "Changed" is the selection on screen differing from the one loaded, never a flag. Save is available only then.
- Cancel, the app bar's back and the system's back all go through `_requestLeave`: at once when nothing changed,
  after the discard question otherwise, and not at all while a save runs. Leaving after a save or a "Discard" uses
  `context.pop()`, which `PopScope` doesn't hold back.
- A failed save keeps both lists. A list's errors show under that list, anything else above the notice; any edit
  clears them all, under both lists.
- Moving is within one list. A language changes list by being removed and added.
