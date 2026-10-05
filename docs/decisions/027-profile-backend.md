# 027: User profile (stage 7, backend)

> **Status:** in force.
>
> **Changed later:** 029 added a third domain package and a second per-user limit, each with its own bucket.
>
> **Client side:** 028.
>
> **Current rules:** `.claude/rules/profile.md`, `.claude/rules/backend.md`.

- **Scope:** a signed-in user creates, reads and updates **their own** profile. No endpoint returns one user's
  profile to another; discovery, languages, matching and everything else about a member come later.
- **Fields:** `display_name` (required, 1–50 characters) and `bio` (optional, up to 500), nothing else. They are the
  least that makes a member presentable, and each later feature gets its own table keyed by `user_id` (or an
  additive column with a default) instead of a speculative one now. A unique handle was left out on purpose: it
  brings a uniqueness index, reserved names, case and confusable rules and a conflict response, and belongs with
  discovery.
- **Public and private:** `display_name` and `bio` are **public by intent**: they are what a future discovery
  endpoint may show other members, and the app says so on the profile form. Everything else stays private: email,
  `email_verified_at`, `users.id` (what identifies a member publicly is a discovery decision), sessions and
  identities. The profile's timestamps are returned to the owner only.
- **Table** (migration 00005): `profiles(user_id PK → users ON DELETE CASCADE, display_name, bio NOT NULL DEFAULT '',
  created_at, updated_at)`. A separate table rather than columns on `users`, which is the auth row the token flows
  lock `FOR UPDATE`. The primary key is the owner, so one profile per user is a constraint and the only lookup
  needs no other index. `bio` is never NULL (one representation of "none", as with
  `users_password_hash_not_empty`). CHECKs backstop the application: `profiles_display_name_length`,
  `profiles_display_name_trimmed`, `profiles_bio_length` (`char_length` counts characters, like the application).
  Existing users get no row: **no row means no profile yet**, and nothing is backfilled.
- **Package:** `internal/profile` (`Service.Get`, `Service.Save`, validation, SQL) depends on neither `auth` nor
  `server` and has its own typed errors. Dependency direction is now `main` → `server` → `auth`, `profile`. The
  handlers pass the authenticated `UserID`; the package never sees a request.
- **API**, both routes behind `requireAccessToken`, with no id in the path, query or body:
  - `GET /v1/me/profile` → 200 `{"display_name","bio","created_at","updated_at"}`, or 404 `profile_not_found` when
    none was saved (a new error code).
  - `PUT /v1/me/profile` `{"display_name","bio"}` → **200** with the profile as stored, whether it was created,
    changed or already the same. A full replace: an absent or `null` `bio` is cleared. A 201 for the first save
    would make a retried create answer differently from the first attempt.
  - POST + PATCH was not chosen: it adds a create conflict, partial-update semantics and a second route for a
    two-field resource with a single owner.
- **Ownership by construction:** the owner is the `UserID` of the session the access token belongs to (016) and is
  the only key of both statements. The contract has no place for another user's identifier, so there is no check
  that could be forgotten: an id in the query is ignored, and an id (or a timestamp, or any other name) in the body
  is an unknown field → 400 `invalid_request`. The request and response structs list their fields explicitly, which
  is also the mass-assignment guard.
- **Validation** lives in `profile` and reports every failing field (422, as everywhere). Lengths count code points
  after normalization. Both fields share one definition of whitespace:
  - a **space** is a tab or any Unicode space separator (no-break, em, ideographic, …);
  - a **line break** is LF, CR or CRLF;
  - **every other control or separator character** (vertical tab, form feed, NEL, the line and paragraph
    separators, NUL, …) is neither: it is never dropped or converted, and the text containing it is `invalid`,
    wherever it stands, the ends included.

  What each field does with them:
  - `display_name`: NFC; leading and trailing spaces and line breaks removed, and **every inner run of spaces and
    line breaks becomes one space**. A line break in a name is therefore accepted and stored as a space, on
    purpose: a name pasted over two lines is one name, and refusing it would only make the user retype it. Then
    `required` if empty, `too_long` over 50, `invalid` if it holds a character that isn't printable or has no letter
    or digit at all.
  - `bio`: NFC; line breaks become LF and tabs become spaces; spaces at the end of a line are removed, so a line
    of only spaces is a blank line; three or more line breaks in a row become two; leading and trailing spaces and
    line breaks are removed. Spaces inside and at the start of a line are kept (indentation, lists). Then
    `too_long` over 500, `invalid` for a non-printable character. May be empty.
  - **Printable** means Go's `unicode.IsGraphic` (letters, marks, numbers, punctuation, symbols, spaces) plus ZWJ
    and ZWNJ, which Persian, Indic scripts and emoji sequences need, plus LF in a bio. That excludes control
    characters, bidirectional overrides and isolates, zero-width and other invisible format characters,
    private-use and unassigned code points, and U+FFFD (what the JSON decoder leaves for invalid UTF-8).
  - **Invisible characters that Unicode classes as letters, symbols or marks** pass `IsGraphic` and are refused by
    name, in both fields: the Hangul fillers U+115F, U+1160, U+3164 and U+FFA0, the blank Braille pattern U+2800,
    the combining grapheme joiner U+034F and the Khmer inherent vowels U+17B4 and U+17B5. Without this a name
    made of U+3164 alone counted as "a letter" and showed as nothing. The list is explicit (`invisible` in
    `validate.go`) because the variation selectors and the Mongolian ones, which are in the same Unicode property,
    are needed by emoji, CJK and Mongolian text and stay allowed.
  - **Combining marks:** at most 8 in a row, and a joiner (ZWJ, ZWNJ) counts as one. A joiner draws nothing
    either, so it neither restarts the count nor pads a name without limit.
  - Normalizing is idempotent: sending back what the server returned stores the same text.
  - Any script is accepted: this is a language app. NFC, not NFKC (009 uses NFKC for passwords, where equivalence
    matters more than appearance): a name keeps the characters its owner chose.
  - Text is stored as normalized, not HTML-escaped: SQL is parameterized, the JSON encoder escapes `<>&`, and the
    client renders plain text.
- **Limits:** the body is capped at **64 KiB**, on purpose far above the longest valid profile (under 7 KiB with
  every character escaped as a surrogate pair). The field limits are the rule; the body limit only bounds what one
  request can make the server read and normalize. With a cap near the field limits (8 KiB at first) a long pasted
  bio was cut off by the cap and answered 400 `invalid_request`, which the app can only show as a generic failure;
  now anything a person plausibly pastes reaches validation and gets 422 `bio: too_long`. A body over 64 KiB
  (about twenty pages of text) is still 400, the API's convention for oversized bodies. The client sets no limit of
  its own (028).
  `PUT` has a **per-user** limit, `user_profile_write`: burst 10, then 1 / 6 s, keyed by the user ID, in `server`
  (`UserLimits`, `limitByUser`), checked after authentication and before the body is read. It is the first limit
  on a protected route; 018 left those unlimited. Requests that don't authenticate never spend it, so nobody can
  exhaust another user's allowance; mounted without authentication it fails closed. `GET` is one primary-key read
  behind authentication and is not limited, like `/v1/me`. The limiter is in process, with 018's single-instance
  caveat. `main` builds every limit in `serverOptions`, which is tested: a limit left out is the zero value and
  would silently disable itself.
- **One write statement, no transaction:** `INSERT … ON CONFLICT (user_id) DO UPDATE … WHERE the text differs …
  RETURNING`, in autocommit, so there is no partial write.
  - *Two first saves at once:* one inserts, the other updates; neither fails.
  - *Concurrent updates:* the row lock applies them in turn, and each writes both columns, so the row always holds
    one request's name and bio together. **The last one wins**; the one lost is the same owner's other edit.
    Optimistic locking (a version, 409 on a stale write, a reload-and-merge state in the app) was judged not worth
    its API and UI for a resource only its owner writes.
  - *Idempotent, without writing:* when the profile already holds exactly the submitted text (after
    normalization), the `WHERE` makes the statement update nothing: no new row version, `updated_at` untouched
    (it means "last change") and no log line. The statement then returns no row, and the profile is read back with
    a second statement; after a concurrent save that read shows the newer text, which is the truth at that
    moment. Repeating a save therefore changes nothing and answers exactly as before, which makes `PUT` safe to
    retry after a 503 or a lost response (018), and safe for the client to resend after a 401 and refresh (023).
  - *Locks:* the insert takes `FOR KEY SHARE` on the `users` row for the foreign key. It can wait for an auth
    transaction that holds that row `FOR UPDATE`, but takes nothing after it, so it can't be part of a deadlock;
    an update doesn't touch `users`. Retention cleanup (018) is unaffected.
- **Errors:** 400 `invalid_request` (malformed JSON, unknown field, trailing data, wrong type, over 64 KiB); 401
  `invalid_access_token`; 404 `profile_not_found`; 422 `validation_failed`; 429 `rate_limited` with `Retry-After`;
  503 at the request deadline; 405 for other methods (the mux, before any database access); opaque 500 otherwise.
  `writeServiceError` (now in `respond.go`, since it serves both domains) maps both packages' validation errors
  through one conversion.
  A user deleted between authentication and the write violates `profiles_user_id_fkey` and gets the 401 of a dead
  credential, as `/v1/me` does (016). The same race on `GET` answers 404 instead; the next request gets the 401.
  Every response has `Cache-Control: no-store`.
- **Timestamps** are serialized exactly as `/v1/me` serializes its own (RFC 3339 with the offset the database
  driver returns; the client converts to UTC). Nothing profile-specific, so nothing was changed.
- **Logs:** a save that changes something logs `profile: saved` with `user_id` only; an unchanged one logs
  nothing. The name, the bio and request bodies are never logged, and validation errors name fields and codes,
  never the text (both tested).
- **Accepted risks:**
  - *No moderation or reporting.* A member can write anything the character rules allow. Nobody else can see it
    yet; this must be decided before any profile is shown to another member.
  - *Display names are not unique and not checked for look-alike characters,* so one member can take another's
    name. Same condition: to be decided with discovery (handles, blocking, reporting).
  - *Format characters other than ZWJ and ZWNJ are refused,* which rejects a few legitimate emoji sequences
    (subdivision flags use tag characters). Revisit if members report it.
  - *The invisible-character list is a list.* It holds the characters known to render as nothing; Unicode may add
    more, and look-alike (confusable) characters are not covered at all (see above).
  - *A body over 64 KiB is a 400,* not a field error, so a paste of that size gets the app's generic message.
  - The limits 50, 500 and 10 writes a minute are product guesses: constants and one CHECK each.
- **Deployment:** the migration only creates a table, so it is safe while the previous version still serves, and
  the backend can ship before the client. Roll back by redeploying the previous binary: goose applies only file
  versions missing from the database, so it starts normally at version 5 and never touches `profiles`. The down
  migration drops every profile and is for development.
- **Tests:** each constraint by name, the cascade, the down/up round trip and 00005 applied and rolled back with
  users and profiles present; table-driven normalization (limits in code points with multi-byte and astral
  characters, each separator and each refused control character in both fields and at the ends, every invisible
  character alone and as padding, the joiner bypass, bio line handling, scripts and emoji that must pass,
  idempotence); the service on the database (create, replace, a replay that writes no row version and logs
  nothing, a deleted user, an ended context, 16 concurrent distinct first saves leaving one whole row and 16
  identical ones all succeeding); over HTTP the exact response fields, the 401 matrix on both methods with nothing
  written, one user unable to reach another's row through the query or the body, every 400 and 422, line breaks
  and control characters in a name, invisible names, text far over the limits answered as `too_long`, the body
  limit at and over its edge, the longest profile fully escaped, the per-user limit and its fail-closed path, a
  user deleted before the read or the write, and logs free of profile text; and that `main` wires every limit.
- **Deferred:** everything in the scope note; deleting a profile (with account deletion and export); a handle or
  public identifier; moderation, reporting and blocking; optimistic locking; a profile required before using the
  app; limiting protected reads (018).
