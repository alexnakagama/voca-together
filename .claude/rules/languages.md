---
paths:
  - "backend/internal/language/**"
  - "backend/internal/server/languages.go"
  - "backend/internal/server/languages_test.go"
  - "backend/internal/db/migrations/00006_languages.sql"
  - "mobile/lib/**/*language*"
  - "mobile/test/**/*language*"
---

# Languages rules (`GET /v1/languages`, `GET`/`PUT /v1/me/languages`, both sides)

Records: 029 (backend, in force), 030 (client, **draft**). The general rules for a member's own resource are in
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

## Client (not implemented: the app calls none of these routes yet)

These are the approved requirements for the code that will (030). When it exists, update `mobile.md`'s lists
(`signedInRoutes`, the `SessionManager` methods), add `mobile/lib/session.dart` to this file's `paths`, and remove
"draft" here and in 030.

- No minimum in the client: `spoken`, `learning` or both may be empty. Add no "at least one" check.
- The save model is the member's complete selection. Its `toJson` always emits both `spoken` and `learning` as
  arrays (`[]` when empty, never `null`, never left out), and the PUT body is never partial.
- Languages are edited on their own screen at `/profile/languages`; Profile shows a read-only summary. The route
  names nobody and carries no language code.
- Languages are public by intent: the app says so before the member saves. Where is decided with the screens.
- `GET /v1/me/languages` always answers 200: "none yet" is two empty lists, not a 404.
