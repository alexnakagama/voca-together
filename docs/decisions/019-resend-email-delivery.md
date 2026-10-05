# 019: Production email delivery with Resend (M1 step 12)

> **Status:** in force.
>
> **Current rules:** `.claude/rules/auth.md`, `.claude/rules/config.md`.

- **Provider: Resend** (`email.ResendSender`, `POST https://api.resend.com/emails`), called with a stdlib `net/http`
  client rather than the Resend SDK (008): one JSON request needs no dependency, and owning the client lets us set
  timeouts, redirects and error handling ourselves. `email.Sender` stays provider-agnostic: `auth` builds
  `email.Message` (`To`, `Subject`, `Text`, optional `HTML`) and never knows which sender runs.
- **Sender selection** (`newEmailSender` in `main`, logged by provider name only, never the key or sender address):
  | `ENV` | Sender |
  |---|---|
  | `production` | Resend, always; `RESEND_API_KEY` and `EMAIL_FROM` are required, startup fails without them |
  | `development` | Resend only when `RESEND_API_KEY` is set (then `EMAIL_FROM` is required), `LogSender` otherwise |
  | `test` | `LogSender`, always; both variables are ignored even if set, so tests never reach the provider |

  There is no silent fallback: production never runs `LogSender`, which logs live tokens (007). `main` re-checks the
  key even though `config.Load` already requires it. `LogSender` logs only the text body.
- **`RESEND_API_KEY`** is a secret. Config holds it as `config.Secret`, redacted in every fmt verb, slog, JSON and text
  marshalling (a pointer inside, so even `%p` shows only an address); `ResendSender` redacts it the same way. Only
  its shape is checked (printable ASCII without spaces, `re_` prefix), which also keeps it safe in a header. No error,
  log or startup message echoes it, nor `EMAIL_FROM` (a misplaced key could land there).
- **`EMAIL_FROM`** must be on a domain verified in Resend and already be in canonical form: `local@domain` or
  `Name <local@domain>` (surrounding space trimmed). Any other spelling `net/mail` accepts is rejected (comments,
  quoted names or local parts, bare `<local@domain>`, repeated spaces), as are non-ASCII, a display name with anything
  but ASCII letters, digits and single spaces, and a domain without a dot or with an IP literal. What is validated is
  exactly what is sent.
- **Content:** verification and password-reset emails have a plain-text body (canonical, always present) and an HTML
  alternative (`auth/templates/action_email.html`: table layout, inline styles, no scripts, images or external
  resources, the link both as a button and as visible text; `html/template` escapes every field). If HTML rendering
  failed, the message would go out as text only. The account-exists and password-changed notifications are text
  only. Links are `APP_BASE_URL` + `/verify-email` or `/reset-password` + `?token=…` and nothing else.
- **Pages (006):** `GET /verify-email?token=…` now exists alongside `GET /reset-password`. Both render without
  touching the database or using the token (prefetch-safe: mail scanners change nothing); a malformed token gets a
  400 invalid-link page that doesn't echo input. `POST /verify-email` (form body only, query token ignored) calls the
  same `VerifyEmail`: 200 done, 400 invalid link (every unusable token alike), 429 / 503 / 500 pages. It shares
  `ip_token` with the JSON routes and the reset form (018); the GET, like `GET /reset-password`, has no limit; headers and CSP are the reset page's (017). Proxy and CDN
  access logs must drop query strings for `/verify-email` too.
- **Request hardening:** `User-Agent: vocatogether-backend` (Resend rejects requests without one), `Accept` and
  `Content-Type: application/json`, bearer auth. Timeouts: 10 s per request (client timeout, whatever the caller's
  context allows), 5 s TLS handshake, 10 s response header; the caller's context (the 10 s background send timeout,
  018) bounds it as well. At most **64 KiB** of the response is read; normal responses are drained and the connection
  reused, oversized ones are truncated and the connection dropped. **Redirects are not followed**: a 3xx is an
  error, since following it would resend the message (and possibly the key) elsewhere.
- **Error sanitization:** errors carry only the HTTP status and the provider's error `name` if it matches
  `^[a-z_]{1,64}$` (e.g. `validation_error`). The provider's free-text `message` is never used (it may repeat request
  data). Errors never contain the key, recipient or bodies; a caller's cancellation or deadline is reported as such.
  On a 2xx, a failed read of the (unused) body doesn't fail the send. `ResendSender` itself never logs.
- **No retries and no `Idempotency-Key` yet**, and **no durable outbox**: delivery stays best effort exactly as in
  018 (bounded in-flight sends, excess dropped, a failed send logged with `kind` and never undoing committed work).
  A 202 still means accepted, not delivered.
- **Tokens reach Resend.** One-time tokens are in the email bodies, so the provider receives, and may retain, live
  links; that is inherent to sending them by email. Mitigations: 256-bit single-use tokens with short lives (24 h
  verification, 30 min reset), consumed only by POST. **Resend click and open tracking must stay disabled** on the
  sending domain: click tracking rewrites links through the provider's redirector (the token would pass through
  another URL and its logs) and open tracking adds a remote image. The sender doesn't set tracking per request;
  this is a deployment requirement on the Resend domain settings.
- **No migration.** Configuration and code only.
- **Deferred:** durable outbox (with retries and an `Idempotency-Key` per message); bounce and complaint handling
  (webhooks, suppression); provider-specific rate limiting (our sends are bounded only by 018's in-flight cap and
  per-account limits); HTML versions of the notification emails (account exists, password changed).
