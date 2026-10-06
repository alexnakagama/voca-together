---
paths:
  - "backend/**"
---

# Backend rules

The package map and startup order are in `docs/architecture.md`. Rules for one domain load with that domain's
files: `auth.md`, `google-sign-in.md`, `profile.md`, `languages.md`, `avatar.md`, and `config.md` for
configuration.

## Packages

- `internal/server` is the HTTP layer only. Protected routes are wrapped **individually** in `server.New` with
  `requireAccessToken`. Handlers read `auth.Identity` from context via `identityFrom` and pass `UserID` explicitly to
  services; a handler mounted without the middleware must fail closed.
- Domain packages (`auth`, `profile`, `language`, `avatar`) never read the request context or configuration. Their
  errors are typed and mapped to HTTP in `server` (`writeServiceError`). `profile`, `language` and `avatar` import
  neither `auth`, nor `server`, nor each other.
- `internal/ratelimit` limits are per process; see decision 018 before scaling out.

## A member's own resource (the pattern of decisions 027, 029 and 031)

- It is selected only by the authenticated `UserID` the handler passes: never add an id to its routes, query or
  bodies. Request and response structs list their fields explicitly, so anything else in a body is an unknown field.
- Never log what a member wrote or chose. A save logs the `user_id` and nothing else.
- A save is a full replace and idempotent: saving what is already stored writes nothing and logs nothing. Clients
  rely on it, because they resend a save after a 401 and retry after a 503.
- A protected route that writes gets a per-user limit (`UserLimits`, `limitByUser`) inside `authn`, so only the
  user's own authenticated requests spend it. Each resource has its own bucket (`ProfileWrite`, `LanguagesWrite`,
  `AvatarWrite`). `serverOptions` in `main` must wire every one, `MemberRead` included: a limit left out disables
  itself silently.

## Reading another member (the pattern of decision 031)

- A route that names a member takes the member's **public id** in the path and is a `GET`, nothing else. Every
  write stays under `/v1/me/…`, selected by the session: ownership is a property of the routes, not a check.
- The handler resolves the id with `profile.Service.Public`, which returns the owner's internal `UserID`, and reads
  the owner's other public data with that `UserID` through the owning package (`language.Service.Get`,
  `avatar.Service.Exists` and `Get`). The `UserID` never reaches a response or a log. The domain packages still
  don't import each other: `server` composes.
- A member who has no profile has no public id, so nothing about them is reachable. Not found, malformed and
  unavailable are one 404 `profile_not_found`, identical in body and headers.
- The response is its own struct listing the public fields, never an owner's response with fields removed.
- These reads share the per-user limit `UserLimits.MemberRead`, keyed by the reader, inside `authn`. A member's own
  GETs stay unlimited.
- Nothing about the member read is logged: no id, name, text or language.

## Migrations

- Embedded from `internal/db/migrations/*.sql` and applied on every startup. Add new numbered files; never edit
  applied ones.
- When a migration adds a table of user data, add it to the `TRUNCATE` in `testutil.DB`. Tables of seed data
  (`languages`) are never truncated.

## API conventions

- Errors are `{"error":{"code":"…"}}`; `validation_failed` (422) adds `fields:[{field,code}]` with all failing
  fields. Malformed JSON / unknown fields / trailing data / oversized body → 400 `invalid_request`. Anything else →
  opaque 500 `internal_error`, details only in logs.
- Auth endpoints and protected routes send `Cache-Control: no-store` on every response, errors included.
- Rate limits → 429 `rate_limited` with `Retry-After` (nothing was done; retry is safe). Argon2 or image decode
  queue timeout, Google keys unavailable, or the 10 s request deadline → 503 `service_unavailable` with
  `Retry-After` (work may have committed; see 018 for which endpoints are safely retriable, 026 for Google).
- Responses are JSON, except a profile picture, which is `image/jpeg` (`writeImage`); its errors are JSON like any
  other.
- New public routes that do real work get a per-IP limit in `server.New` (the per-account rules are in `auth.md`).
