---
paths:
  - "backend/internal/config/**"
  - "backend/cmd/api/**"
  - ".env.example"
---

# Configuration rules (backend)

Only `main` reads configuration, through `internal/config`; no other package does. Secrets are held as
`config.Secret`, which redacts itself. Startup order is in `docs/architecture.md`.

| Variable | Rule |
|---|---|
| `DATABASE_URL` | Required. Secret: never log it. |
| `ENV` | `development` \| `test` \| `production`. |
| `HTTP_ADDR` | Default `:8080`. |
| `APP_BASE_URL` | Used in emailed links; https and a public host required in production. |
| `TRUSTED_PROXY_HOPS` | Reverse proxies appending to `X-Forwarded-For`; default 0 = TCP peer. Must be set explicitly in production. Only valid if the server is reachable solely through those proxies, otherwise clients can spoof their IP (018). |
| `RESEND_API_KEY` | Secret, held as `config.Secret`: never log it. |
| `EMAIL_FROM` | Canonical `local@domain` or `Name <local@domain>` on a Resend-verified domain. |
| `GOOGLE_CLIENT_ID` | Public. The Web OAuth client ID that Google ID tokens must name as `aud`; never an Android client ID. Required in production (startup fails without it), optional in development (unset disables Google sign-in), ignored in test. |

- Email sender by `ENV` (decision 019): production always uses Resend and refuses to start without `RESEND_API_KEY`
  and `EMAIL_FROM`; development uses `LogSender` unless `RESEND_API_KEY` is set (then `EMAIL_FROM` is required too);
  test always uses `LogSender` and ignores both. There is no silent fallback: `LogSender` logs live tokens.
- Only `main` constructs the Google verifier (`nil` = disabled, outside production only), before the database
  connects, so bad configuration exits at once (decision 020, Stage 6).
- No Google client secret, API key, service account or Firebase exists in this backend.
