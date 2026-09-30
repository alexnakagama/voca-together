# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

VocaTogether: a language-exchange app. Right now the repo holds only the Go backend (`backend/`, module
`vocatogether/backend`). A Flutter client is planned; `docs/decisions.md` already specifies client contracts for it.

## Commands

Run from the repo root (the Makefile `cd`s into `backend/`):

```sh
make db-up    # start local PostgreSQL 17 (docker compose); also creates the voca_test database
make test     # all tests against voca_test, with -p 1
make vet      # go vet ./...
make run      # run the API on :8080 (migrations apply on startup)
make db-down
```

Single test (from `backend/`):

```sh
TEST_DATABASE_URL=postgres://voca:voca@localhost:5432/voca_test?sslmode=disable \
  go test -p 1 ./internal/auth -run TestLogin
```

- DB-backed tests use `testutil.DB(t)`, which migrates and TRUNCATEs a single shared database. They **skip**
  silently when `TEST_DATABASE_URL` is unset, so a "passing" run without it proves little. Never use `t.Parallel`
  in them and always keep `-p 1`.
- `testutil.DB` truncates `users, user_tokens, sessions`; add new tables there when adding migrations.

Config (env): `DATABASE_URL` (required, secret, never log it), `ENV` (`development`|`test`|`production`),
`HTTP_ADDR` (default `:8080`), `APP_BASE_URL` (used in emailed links; https required in production).
Production refuses to start because only the dev `LogSender` email implementation exists.

## Architecture

Modular monolith: one Go service, one PostgreSQL database, stdlib only where possible (`net/http` Go 1.22+
routing patterns, pgx, goose, x/crypto). Keep dependencies minimal.

- `cmd/api/main.go` builds all dependencies (config → pool → migrations → services → router) and handles graceful
  shutdown, then `authSvc.Wait()` drains background emails.
- `internal/server` is the HTTP layer only: routing (`server.go`), JSON decode/encode and error codes
  (`respond.go`), the `requireAccessToken` middleware (`authn.go`), and the HTML pages that emailed links open
  (`pages.go`, templates embedded from `pages/`; GET never uses a token, POST calls the same service). Protected
  routes are wrapped **individually** in `server.New`; handlers read `auth.Identity` from context via
  `identityFrom` and pass `UserID` explicitly to services, so domain packages never read the request context.
- `internal/auth` owns the domain: `Service` (business logic, argon2 slot limiter, background email sending),
  `store.go` (SQL), tokens, password policy/hashing, email content. Errors are typed (`errors.go`) and mapped to
  HTTP in `server`.
- `internal/email` handles delivery only (`Sender` interface, `LogSender` for dev, `Recorder` for tests). Dependency
  direction is `auth → email`, never the reverse.
- `internal/db`: pool + goose migrations embedded from `internal/db/migrations/*.sql` and applied on every startup.
  Add new numbered files; never edit applied ones.

### API conventions

- Errors are `{"error":{"code":"…"}}`; `validation_failed` (422) adds `fields:[{field,code}]` with all failing
  fields. Malformed JSON / unknown fields / trailing data / oversized body → 400 `invalid_request`. Anything else →
  opaque 500 `internal_error`, details only in logs.
- Auth endpoints and protected routes send `Cache-Control: no-store` on every response, errors included.

## Decisions log — read before changing auth

`docs/decisions.md` is the source of truth for security-relevant behavior and records the reasoning, lock orders,
race analyses, and deferred work for each step (numbered, newest at the bottom). When a change makes or alters a
design decision, append a new entry there in the same style. Key invariants:

- Opaque DB-backed tokens, not JWT: access `vt_at_…` (15 min), refresh `vt_rt_…` (30 d sliding, 90 d absolute).
  Only SHA-256 hashes are stored. Refresh rotates every use; reuse of the previous refresh token revokes the session.
- No account enumeration: register/resend/forgot always 202; login gives the same 401 for unknown email and wrong
  password, with equal work (dummy-hash verification). Emails are sent in the background with a context derived via
  `context.WithoutCancel`, never the request context.
- Uniform errors for all unusable tokens (one code per token kind); malformed tokens are rejected before any DB access.
- Passwords: argon2id (PHC format, rehash on login), NFKC-normalized before hashing/verifying/policy checks — never
  change the normalization form without a migration path. Emails: trimmed, lowercased, printable ASCII only.
- One-time tokens are consumed with a single conditional `UPDATE … RETURNING`. Lock order: transactions that touch an
  existing user's `user_tokens` lock the `users` row `FOR UPDATE` first; session-only operations lock a single
  session row.
- Never log emails, passwords, tokens, token hashes, user agents, or request bodies. `auth.Token` redacts itself in
  fmt, slog and JSON.
