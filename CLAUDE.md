# CLAUDE.md

## Project

VocaTogether: a language-exchange app. One repository, two parts:

- `backend/`: the Go API (module `vocatogether/backend`). Modular monolith: one Go service, one PostgreSQL 17
  database, stdlib where possible (`net/http` routing patterns, pgx, goose, x/crypto, x/text).
- `mobile/`: the Flutter client, Android only (package `vocatogether`, applicationId and namespace
  `com.vocatogether.app`).

Keep dependencies minimal on both sides.

## Commands

Backend, from the repo root (the Makefile `cd`s into `backend/`):

```sh
make db-up    # start local PostgreSQL 17 (docker compose); also creates the voca_test database
make test     # all tests against voca_test, with -p 1
make vet      # go vet ./...
make run      # run the API on :8080 (migrations apply on startup)
make db-down
```

Single test, from `backend/`:

```sh
TEST_DATABASE_URL=postgres://voca:voca@localhost:5432/voca_test?sslmode=disable \
  go test -p 1 ./internal/auth -run TestLogin
```

DB-backed tests **skip silently** when `TEST_DATABASE_URL` is unset, so a "passing" run without it proves little.
Always keep `-p 1`.

Mobile:

```sh
make mobile-analyze   # flutter analyze (FLUTTER=/path/to/flutter if it isn't on PATH)
make mobile-test      # flutter test
# from mobile/:
flutter run --dart-define-from-file=config/dev.json   # emulator → backend from `make run` at http://10.0.2.2:8080
flutter build apk --debug --dart-define-from-file=config/dev.json
```

## Where the rest lives

Rules say what holds now and load by themselves; decision records say why and are read one at a time.
`docs/README.md` explains the whole layout.

| File | Content | Loaded |
|---|---|---|
| `.claude/rules/backend.md` | Package boundaries, a member's own resource, migrations, API conventions | with `backend/**` |
| `.claude/rules/mobile.md` | Build config, structure, token boundary, UI, screens, strings | with `mobile/**` |
| `.claude/rules/testing.md` | Test database rules, mobile fakes and harness, enforcement tests | with test files |
| `.claude/rules/auth.md` | Auth invariants, lock order, limits, email, the client session layer | with the auth files of both sides |
| `.claude/rules/google-sign-in.md` | The Google sign-in contract | with the Google files of both sides |
| `.claude/rules/profile.md`, `languages.md` | Each feature's rules, backend and client | with that feature's files |
| `.claude/rules/config.md` | Environment variables, startup configuration | with `backend/internal/config/**`, `backend/cmd/api/**`, `.env.example` |
| `docs/architecture.md` | Package and layer map of both parts, client status | read on demand |
| `docs/decisions.md` | Index of the decision records: by task, by status, what superseded what, deferred work | read on demand |
| `docs/decisions/NNN-*.md` | One record each: reasoning, lock orders, race analyses, accepted risks. "Decision 017" in code or docs is `docs/decisions/017-*.md` | read the one you need |

A rule loads only when the Read, Write or Edit tool is used on a path it matches; searches and shell commands do
not trigger it. When planning or answering without having touched such a path, read the relevant rule file first.

## Architecture boundaries

- Backend dependency direction: `cmd/api/main.go` builds everything → `internal/server` (HTTP only) →
  `internal/auth`, `internal/profile` and `internal/language` (domains; `profile` and `language` import neither
  `auth` nor each other); `internal/auth` → `internal/email`, `internal/googleid`. Never the reverse. Only `main`
  reads configuration; domain packages never read the request context.
- Migrations: add new numbered files in `backend/internal/db/migrations/`; never edit applied ones.
- Mobile: `main.dart` is the composition root and the only place long-lived objects are built. Pass them down by
  constructor: no provider/riverpod/bloc/get_it, no top-level mutable state.
- Mobile token boundary: screens and UI never see a token and never read or change session state. They call
  `SessionManager`'s public API and `AccountApi`; `router.dart`'s `authRedirect` is the only navigation policy.

## Security

- Before changing auth, session, token or Google sign-in code, on either side, read the rule file for it and the
  decision records it cites (`docs/decisions.md` lists them by task). Read each record's header first: it says what
  later records changed.
- Never log emails, passwords, tokens, token hashes, user agents, IPs or request bodies. Google ID tokens, `sub` and
  the client ID count too.
- `DATABASE_URL` and `RESEND_API_KEY` are secrets. Everything in `mobile/config/*.json` is compiled into the APK and
  is public: never put a secret there.
- No account enumeration: responses and work must not reveal whether an account exists (the one deliberate
  exception is Google sign-in's 409 `account_exists`).
- Never put a token or an email in a mobile route.

## Workflow

- When a change makes or alters a design decision, add a numbered record in `docs/decisions/` and index it, as
  `docs/README.md` describes. Never rewrite an earlier record: note the change in its header.
- Checks for a change: backend `make vet` and `make test` (with the database up); mobile `make mobile-analyze` and
  `make mobile-test`.
- Do not commit or push: the user reviews and commits.
- Keep this file short. A new rule goes in the narrowest matching `.claude/rules/` file, a description in
  `docs/architecture.md`, and reasoning in a decision record. Add here only what nearly every session needs.
