# 029: Languages (stage 8, backend)

> **Status:** in force.
>
> **Client side:** 030 (draft; not implemented).
>
> **Current rules:** `.claude/rules/languages.md`.

- **Scope:** a signed-in user reads the catalog of languages and reads and replaces **their own** languages: the
  ones they speak and the ones they are learning, each with a level. No endpoint returns one member's languages
  to another; discovery, matching and ranking come later and will read through this package.
- **Identifiers are codes, never names.** A code is the BCP 47 primary language subtag in lowercase: the ISO 639-1
  two-letter code where one exists, else the ISO 639-3 three-letter code (`yue`, `fil`). It is what Android
  locales, CLDR and external datasets use, so localized names need no mapping table later. One entry per language
  a learner would name (`zh` is Mandarin, `yue` Cantonese). Scripts and regional variants (`pt-BR`, `zh-Hant`) are
  not codes; if matching needs them they become a column on `user_languages`, not new catalog rows.
- **Model:** one row per (user, language) with a `kind` and a `level`.
  - `kind` is `spoken` ("I know it and can offer it") or `learning` ("I want to practise it"). A language is in at
    most one of a member's two lists. A learner still has a level, so "I speak Spanish at B2 and still practise
    it" is `learning`, `b2`.
  - **Level** is CEFR plus native as one ordered scale: `a1 a2 b1 b2 c1 c2 native` in the API, `smallint` 1–7 in
    the database, so "B2 or better, natives included" is one comparison. `native` is allowed only for `spoken`;
    several native languages are allowed and none is required. A `smallint` with a CHECK rather than a Postgres
    enum (values can't be removed, awkward in transactional migrations) or text (doesn't order). The numbers are
    the ones stored and must never change (`language.Level`, tested both ways).
  - **Order is the member's:** each list is ordered by `position` 0–4, the first being the main one. The range is
    also what limits a member to **5 languages of each kind** (`language.MaxPerKind`) without a trigger.
- **Tables** (migration 00006):
  - `languages(code PK, name UNIQUE, endonym, created_at)`: the catalog, with the English name and the name the
    language gives itself. CHECKs: `languages_code_format` (`^[a-z]{2,3}$`), `languages_name_text`,
    `languages_endonym_text`. Seeded with 107 languages by the migration. A table rather than a Go list: the
    foreign key makes an unknown code impossible for any writer, discovery can join on it, and adding a language
    is one reviewed `INSERT` migration. Names are data, not localized strings.
  - `user_languages(user_id → users ON DELETE CASCADE, language_code → languages, kind, level, position,
    created_at)`, primary key `(user_id, language_code)`. The catalog reference has no `ON DELETE`: a language
    somebody has can't be removed. CHECKs `user_languages_kind_check`, `user_languages_level_range`,
    `user_languages_native_is_spoken`, `user_languages_position_range`, and `user_languages_position_key`
    `UNIQUE (user_id, kind, position)`. **No row means no languages chosen.** No `updated_at`: the set is
    replaced whole, so `created_at` is when the set was written.
  - Index `user_languages_by_language (language_code, kind, level)`: the lookup discovery will make ("who speaks
    `es` at C1 or better") and the index behind the catalog foreign key. Created now, on an empty table.
  - `testutil.DB` truncates `user_languages` and never `languages`, which is seed data.
- **Package:** `internal/language` (`Service.Catalog`, `Get`, `Save`, validation, SQL) imports neither `auth` nor
  `server` nor `profile` and has its own typed errors. Dependency direction is now `main` → `server` → `auth`,
  `profile`, `language`. Handlers pass the authenticated `UserID`; the package never sees a request.
- **A separate resource,** not new fields on `PUT /v1/me/profile`: that PUT is a full replace (027), so adding
  languages to it would make every existing client's save clear them.
- **API,** all three routes behind `requireAccessToken`, with no id in the path, query or body:
  - `GET /v1/languages` → 200 `{"languages":[{"code","name","endonym"}]}`, ordered by `name`, the same for every
    member. Authenticated because only signed-in screens need it, so it needs no per-IP limit; it can be opened
    later without changing its shape.
  - `GET /v1/me/languages` → **200 always**: `{"spoken":[{"language","level"}],"learning":[…]}`. A member who has
    chosen nothing gets two empty arrays, never `null` and never a 404: an empty collection is a value.
  - `PUT /v1/me/languages`, same body → **200** with the lists as stored, whether they changed or were already
    the same. POST or PATCH per item was not chosen: at most ten small items with one owner don't need per-item
    routes, conflicts or partial updates.
- **PUT is a full replacement, and it must say what replaces each list.** The body is the member's whole
  selection, and array order is the stored order. The four things a list can be in a body:
  - **A non-empty array** replaces the list.
  - **An empty array (`[]`) clears the list.** It is the only way to clear one. Both lists may be empty, which
    clears all of the member's languages: nothing is required. (The plan's `required` rule, at least one of each,
    was dropped; whether the app asks for one is the client's decision, 030.)
  - **An omitted key is invalid** → 400 `invalid_request`, and nothing is changed.
  - **An explicit `null` is invalid** → 400 `invalid_request`, and nothing is changed. A `null` body likewise.

  So both `spoken` and `learning` must be present as arrays in every save. *Why:* reading a missing or `null`
  list as empty would turn a client's mistake into a deletion: a build that forgets a key, a serializer that drops
  nulls or empty values, or a later client that sends only the list it edited would each silently delete up to
  five of the member's languages and answer 200. A replacement that deletes must be told to. Requiring the keys
  costs an honest client nothing, since it always holds both lists.
  - *Unlike 027,* where an absent or `null` `bio` is cleared: an empty bio is the default of an optional text
    field, and losing one is a single field the member retypes. Here the omitted thing is a collection, and the
    same leniency deletes rows. 027 is not changed.
  - *400, not 422:* it is the body's shape that is wrong (a required member is missing or has the wrong type),
    which is `invalid_request` everywhere in this API, like a wrong type or an unknown field. 422 is for a
    well-formed selection that breaks a rule, and its `fields` name rules a member can fix; no member can fix a
    missing key. The plan's `required` code meant "the list is empty" and is not used.
  - *Where the rule lives:* in the HTTP layer (`languagesRequest.complete`), because presence is a property of the
    JSON body. `internal/language` has no notion of an absent list: `language.Input` holds two Go slices, a nil
    one and an empty one are the same selection (`Selection.Equal`), and `parse` never reports a missing list.
    Nothing in the domain, the store or the schema changed for this rule.
  - *Inside a list* nothing is deleted by leniency, so the ordinary rules answer: an item that is `null` or `{}`,
    or whose `language` or `level` is missing or `null`, is an item without a language or a level → 422
    (Validation, below), and the save writes nothing.
  - A key repeated in the body counts as its last value (the decoder's rule), so `"learning":[…],"learning":null`
    is a `null` list and is refused.
- **Ownership by construction:** the owner is the `UserID` of the session the access token belongs to (016) and is
  the only key of every statement on `user_languages`. An id in the query is ignored; an id, a `kind`, a
  `position` or any other name in the body or in an item is an unknown field → 400. Request and response structs
  list their fields explicitly.
- **Validation** lives in `language` and reports every failing list at once (422 `validation_failed`). The field
  is the list (`spoken` or `learning`), one error per list and code, spoken first, in this order:

  | Code | When |
  |---|---|
  | `too_many` | more than 5 items in the list |
  | `unknown_language` | a code that isn't exactly two or three lowercase ASCII letters, or isn't in the catalog |
  | `invalid_level` | a level that isn't one of the seven names; or `native` in `learning` |
  | `duplicate` | a code twice in the list; a code in both lists is reported on `learning` |

  Codes and levels are identifiers, not text: nothing is trimmed or lowercased, so `ES`, ` es` and `C1` are
  invalid. Errors are per list rather than per item because the app builds its lists from the catalog, so an
  item error means a bug or a stale catalog, and the `fields:[{field,code}]` shape stays as it is. An item that
  is `null` or `{}` has no language and no level and gets both codes. Shape rules are checked without the
  database (`parse`); whether a well-formed code names a language is checked inside the save's transaction
  (`checkKnown`), only for a selection that parsed. An invalid save writes nothing.
- **Save is one transaction, and the `users` row comes first.** A set can't be compared and replaced in one
  statement without racing, so unlike 027 this needs a transaction (`replaceSelection`):
  1. `SELECT 1 FROM users WHERE id = $1 FOR NO KEY UPDATE`. No row → `ErrUserGone`.
  2. `SELECT code FROM languages WHERE code = ANY(…)`; a missing one → `unknown_language`, rolled back.
  3. Read the current rows. Equal to the request → commit having written nothing.
  4. Otherwise `DELETE` the user's rows and insert the new ones with one `INSERT … SELECT FROM unnest(…)`.
  - *Why the user row:* it serializes one member's saves, so each sees what the one before committed and the rows
    always hold one request's whole selection; and it detects a deleted user. It follows the existing lock order
    (auth transactions take the row `FOR UPDATE` first), so a save waits for them and they for it, never a
    deadlock. `FOR NO KEY UPDATE` rather than `FOR UPDATE`: it doesn't block the `FOR KEY SHARE` that inserting a
    row referencing the user takes (a session, a profile), so a save never holds up a login. After it come only
    the member's own `user_languages` rows and key-share locks on catalog rows, which nothing updates; the
    inserted rows hold their languages, so none can leave the catalog between the check and the commit.
  - *Concurrent saves* are applied in turn and **the last one wins whole**; the one lost is the same owner's
    other edit. No optimistic locking, as in 027.
  - *Different members* lock different rows and don't wait for each other.
- **Identical saves are idempotent no-ops.** When the member already has exactly the submitted selection (same
  languages, levels and order; a missing row set and two empty lists are the same), step 3 ends the transaction:
  **no row is rewritten and no save is logged**, and the answer is the same 200. A PUT is therefore safe to retry
  after a 503 or a lost response (018, `requestTimeout`) and safe for the client to resend after a 401 and a
  refresh (023).
- **Limits:** the body is capped at **8 KiB**. Unlike a profile's text (027's 64 KiB), nothing a person types can
  be long here: the longest valid selection is under 2 KiB fully escaped, so a body near the limit is never an
  honest one. `PUT` has a **per-user** limit, `user_languages_write`: burst 10, then 1 / 6 s
  (`UserLimits.LanguagesWrite`, `limitByUser`), checked after authentication and before the body is read, so
  malformed, invalid and unchanged saves spend it too and unauthenticated requests never do. It is a bucket of
  its own: saving a profile and saving languages don't use up each other's allowance. A 429 did nothing. The two
  GETs are small reads behind authentication and are not limited. In process, with 018's single-instance caveat;
  `serverOptions` wires it and is tested.
- **Errors:** 400 `invalid_request` (malformed JSON, a body that isn't an object, a missing or `null` list, wrong
  type, unknown field, trailing data, over 8 KiB); 401 `invalid_access_token`; 422 `validation_failed`; 429
  `rate_limited` with `Retry-After`; 503 at the request deadline, including a save still waiting for the user's
  row, which then wrote nothing; 405 for other methods (the mux, before any database access); opaque 500
  otherwise. No new error code. `writeServiceError` maps `language.ValidationError` and `language.ErrUserGone`
  as it does the profile's. Every response has `Cache-Control: no-store`.
  A user deleted between authentication and the write gets the 401 of a dead credential (016). The same race on
  `GET` answers 200 with empty lists, which is what a member with no languages gets and reveals nothing; the next
  request gets the 401.
- **Privacy and logs:** a native language can hint at where someone is from, so languages are personal data that
  is **public by intent**, like the profile's text (027): discovery will show them, and the app must say so before
  saving (030). A save that changes something logs `languages: saved` with `user_id` only; an unchanged one logs
  nothing. Codes, levels, counts and request bodies are never logged, and validation errors and responses name
  lists and codes, never the submitted values (tested).
- **Accepted risks:**
  - *Levels are self-declared* and unverified; matching must treat them as hints.
  - *`native` as level 7* treats native as "above C2", a simplification that keeps comparison to one integer.
  - *One list per language:* a member can't both offer and practise a language as two entries; the level on a
    `learning` row carries that.
  - *A curated catalog:* a member whose language is missing can't add it until a migration does.
  - *English names and endonyms only* until the app has a second locale; a `language_names(code, locale, name)`
    table adds per-locale names without touching these.
  - *A language save and an auth write for the same user briefly wait for each other* (the user row). Chosen over
    an advisory lock because it follows the existing lock order and detects a deleted user.
  - *The catalog is fetched whole on each request*, with no cache or ETag.
  - 5 per list, 10 writes a minute and the seed list are product guesses: a constant, one CHECK and a migration.
- **Deployment:** the migration only creates two tables, an index and seed rows, so it is safe while the previous
  version still serves, and the backend can ship before the client. Only new routes; no existing request or
  response changes. Roll back by redeploying the previous binary, which never touches the new tables. The down
  migration drops every member's languages and the catalog and is for development. Later catalog additions are
  `INSERT`-only migrations; old clients need nothing, since names come from the API.
- **Tests:** each constraint by name, both foreign keys, the cascade, the seed (anchors such as `yue`, no endonym
  that is another language's English name), the index, and 00006 applied and rolled back with rows present; the
  level mapping both ways and its order; table-driven validation (every rule on both lists, all errors at once,
  5 and 6 items, cross-list duplicates, non-canonical codes and levels, errors that never contain the input); the
  service on the database (get before saving, create, replace, reorder, clearing, a replay that writes no row
  version and logs nothing, separate users, an unknown code writing nothing, a deleted user, an ended context, 16
  concurrent distinct saves leaving one whole selection, 16 identical ones all succeeding, a save waiting for a
  transaction that holds the user row, the lock allowing rows that reference the user); over HTTP the exact
  responses, the catalog's shape and order, clearing one list or both with `[]`, every body with a missing or
  `null` list refused with the stored rows untouched and no save logged (including one that would otherwise equal
  the stored selection), the 401 matrix on all three routes with nothing written, a session
  ended between requests, one user unable to reach another's rows through the query or the body, every 400 and
  422, the body limit at and over its edge, 405 and 404 for other methods and paths, handlers failing closed
  without the middleware, a user deleted before the read or the write, opaque 500s logged with their route, 503
  at the deadline with nothing written, concurrent saves with reads in between, the per-user limit (its own
  bucket, shared by a user's sessions, spent by rejected requests, not by unauthenticated ones) and logs free of
  languages; and that `main` wires the limit.
- **Deferred:** everything in the scope note; any read of another member's languages; regional variants and
  scripts; sign, constructed and classical languages; per-locale language names; catalog administration at
  runtime; a cache or ETag for the catalog; deleting one's languages other than by saving empty lists (with
  account deletion and export); optimistic locking; limiting protected reads (018).
