---
paths:
  - "backend/**"
  - ".env.example"
---

# Backend rules

The package map and startup order are in `docs/architecture.md`. Auth invariants are in `security.md`.

## Packages

- `internal/server` is the HTTP layer only. Protected routes are wrapped **individually** in `server.New` with
  `requireAccessToken`. Handlers read `auth.Identity` from context via `identityFrom` and pass `UserID` explicitly to
  services.
- `internal/config` is used only by `main`; `auth` and `server` read no configuration. Secrets are held as
  `config.Secret`.
- `internal/auth` owns the domain. Its errors are typed (`errors.go`) and mapped to HTTP in `server`.
- `internal/googleid` knows nothing about users, the DB or HTTP routes. `server` never imports it, and only `main`
  constructs the verifier (`nil` = disabled, outside production only).
- `internal/email` handles delivery only; `auth → email`, never the reverse. `ResendSender` does no retries and
  sanitizes its errors. Resend click/open tracking must stay disabled (links carry tokens).
- `internal/ratelimit` limits are per process; see decision 018 before scaling out.
- The email-link pages (`/verify-email`, `/reset-password`): GET never uses a token or the DB; POST calls the same
  service as the API.

## Migrations

- Embedded from `internal/db/migrations/*.sql` and applied on every startup. Add new numbered files; never edit
  applied ones.
- When a migration adds a table, add it to the `TRUNCATE` in `testutil.DB`.

## API conventions

- Errors are `{"error":{"code":"…"}}`; `validation_failed` (422) adds `fields:[{field,code}]` with all failing
  fields. Malformed JSON / unknown fields / trailing data / oversized body → 400 `invalid_request`. Anything else →
  opaque 500 `internal_error`, details only in logs.
- Auth endpoints and protected routes send `Cache-Control: no-store` on every response, errors included.
- Rate limits → 429 `rate_limited` with `Retry-After` (nothing was done; retry is safe). Argon2 queue timeout, Google
  keys unavailable, or the 10 s request deadline → 503 `service_unavailable` with `Retry-After` (work may have
  committed; see 018 for which endpoints are safely retriable, 020 for Google).
- New public routes that do real work get a per-IP limit in `server.New`; per-account checks run after validation and
  before any DB/argon2 work, keyed by the normalized address whether or not the account exists. Google sign-in is the
  exception: it shares `ip_login`, and its `account_login` key is `google:` + the verified `sub` (known only after
  verification, still before the DB).

## Config (env)

| Variable | Rule |
|---|---|
| `DATABASE_URL` | Required. Secret: never log it. |
| `ENV` | `development` \| `test` \| `production`. |
| `HTTP_ADDR` | Default `:8080`. |
| `APP_BASE_URL` | Used in emailed links; https and a public host required in production. |
| `TRUSTED_PROXY_HOPS` | Reverse proxies appending to `X-Forwarded-For`; default 0 = TCP peer. Must be set explicitly in production. Only valid if the server is reachable solely through those proxies, otherwise clients can spoof their IP. |
| `RESEND_API_KEY` | Secret, held as `config.Secret`: never log it. |
| `EMAIL_FROM` | Canonical `local@domain` or `Name <local@domain>` on a Resend-verified domain. |
| `GOOGLE_CLIENT_ID` | Public. The Web OAuth client ID that Google ID tokens must name as `aud`; never an Android client ID. Required in production (startup fails without it), optional in development (unset disables Google sign-in), ignored in test. |

- Email sender by `ENV` (decision 019): production always uses Resend and refuses to start without `RESEND_API_KEY`
  and `EMAIL_FROM`; development uses `LogSender` unless `RESEND_API_KEY` is set (then `EMAIL_FROM` is required too);
  test always uses `LogSender` and ignores both.
- No Google client secret, API key, service account or Firebase exists in this backend (decision 020 stage 6).
