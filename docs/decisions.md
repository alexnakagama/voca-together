# Architecture decisions

Short log of decisions and why they were made. Newest at the bottom.

## 001: Modular monolith: Go + PostgreSQL + REST
One deployable Go service, one database. Feature packages under `backend/internal/`.
Microservices would add operational cost with no benefit at this stage.

## 002: Opaque, DB-backed session tokens instead of JWT
- Access token (`vt_at_…`, 15 min) and refresh token (`vt_rt_…`, 30 days sliding, 90 days absolute), each 32 random bytes.
- Only SHA-256 hashes are stored (in `sessions`).
- Why: logout, revoke-all-on-password-reset and refresh-reuse detection all need server state anyway, so a JWT would add
  key management and algorithm pitfalls without making the system stateless. One indexed lookup per request is cheap
  at our scale. Clients treat tokens as opaque, so we can still switch later.
- Refresh rotates on every use. Presenting an already-rotated refresh token revokes the whole session.

## 003: argon2id for passwords
- OWASP baseline parameters (m=19 MiB, t=2, p=1), stored in PHC format so they can be raised later with rehash-on-login.
- Policy (NIST 800-63B): 10–128 characters, no composition rules, reject common passwords and passwords equal to the email.

## 004: One-time tokens (verification / reset)
- 32 random bytes; only the SHA-256 hash is stored, in `user_tokens` with a `purpose` column.
- Consumed with a single atomic `UPDATE … WHERE used_at IS NULL AND expires_at > now() RETURNING`, which makes them single-use and replay-safe.
- At most one active token per user and purpose (partial unique index). Issuing a new token deletes the old one.

## 005: No account enumeration
register, resend-verification and forgot-password always return 202. Login returns the same 401 for an unknown email
and a wrong password. `email_not_verified` (403) is returned only after the password is correct.

## 006: Email links open small backend-served HTML pages
- GET renders a confirm button or form, and only POST consumes the token (so mail-scanner prefetch is harmless).
- The pages call the same service as the JSON API, so in-app deep links can be added later with client work only.

## 007: Email behind an interface
`email.Sender` with a dev `LogSender` and a test `Recorder`. The real provider will be chosen later.

## 008: Minimal libraries
- Backend: stdlib `net/http` routing (Go 1.22+ patterns), pgx, goose, x/crypto, x/time/rate.
- Flutter: `http`, `flutter_secure_storage`, and `ChangeNotifier` for state.
