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

Records: 029 (backend, in force), 030 (client, **draft**: its data, API and session layer, its widgets and the
Profile summary are implemented, its editor is not). The general rules for a member's own resource are in
`backend.md`.

## Backend (implemented)

- `internal/language` owns the catalog and members' languages and imports none of `auth`, `server` and `profile`.
  Nothing in a route, query or body names a user, a `kind` or a `position`.
- Never log a member's languages: no codes, levels or counts. They are personal data, public by intent.
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
(`languageLevelLabel`) and `lib/screens/profile_languages_section.dart` (`ProfileLanguagesSection`, mounted by
`ProfileScreen` below its form).

- The two widgets are pure UI and take every text as a string: they import neither the models nor the
  localizations. A level becomes text only through `languageLevelLabel`, in `lib/screens/`.
- `LanguageRow` is for the editor; no screen uses it yet.
- The summary is read-only and calls only `languageCatalog()` and `languages()`, together, once when Profile
  opens. It never saves and holds no selection for a save.
- Its state is its own: a failed load shows an error and a retry inside the section, and the profile form, its
  save and its banners don't depend on it. Don't merge the two loads or their errors.
- The load fails whole: if either request fails, no language is shown (never a partial list, never "none yet").
- A code the catalog doesn't name is shown as the code, so every language the member has stays visible.
- A list with no language gets no heading; with both empty, one "none yet" text.

## Client: the editor (not implemented)

There is no editor, no `/profile/languages` route and no control on Profile that opens one yet. These are the
approved requirements (030). When they exist, update `mobile.md`'s `signedInRoutes`, add the new screen files to
this file's `paths` if their names don't already match, and remove "draft" here and in 030.

- No minimum in the editor: add no "at least one" check, and saving two empty lists clears the selection.
- The editor always saves the complete selection, both lists, whichever one was edited.
- Languages are edited on their own screen at `/profile/languages`; Profile shows a read-only summary. The route
  names nobody and carries no language code.
- Languages are public by intent: the app says so before the member saves, on the editor, where the save is. The
  summary saves nothing and carries no notice.
- After the editor saves, the summary on Profile must show the new selection (it loads only when it is created).
