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
- `email` handles delivery only. `auth` owns the content, links and expiry wording. The dependency runs
  `auth → email`, never the reverse.
- Production must refuse to start with `LogSender`: it logs message bodies, which contain live tokens.
  There is no silent fallback.
- Auth sends email in the background (so response timing doesn't reveal accounts), with its own timeout
  derived via `context.WithoutCancel`, never the finished request's context. `Recorder` rejects cancelled
  contexts, so tests catch that mistake.

## 008: Minimal libraries
- Backend: stdlib `net/http` routing (Go 1.22+ patterns), pgx, goose, x/crypto, x/time/rate.
- Flutter: `http`, `flutter_secure_storage`, and `ChangeNotifier` for state.

## 009: Passwords are NFKC-normalized before hashing and policy checks
- Every password is normalized with Unicode NFKC before it is hashed, verified, or checked against the policy.
  The 10–128 character limits count Unicode code points after normalization.
- Why: the same visible password can arrive as different bytes. "ñ" can be one code point or "n" plus a combining
  tilde (from copy-paste, password managers, decomposed text sources, or Korean syllables vs separate letter parts).
  Japanese and Chinese input methods can also type Latin letters in full-width form (`ｐａｓｓ`). Without
  normalization, those users fail to log in for no visible reason. That matters for a language-exchange audience.
- Standards: NIST SP 800-63B recommends NFKC or NFKD before hashing. RFC 8265 (PRECIS OpaqueString) uses NFC and
  keeps full-width characters distinct. We follow NIST, which the password policy already cites.
- Cost: NFKC merges compatibility variants (`ﬁ` → `fi`, `²` → `2`, full-width → ASCII), a negligible entropy loss.
  ASCII passwords are unaffected.
- Constraint: **never change the normalization form without a migration path.** Once hashes exist, a change would
  lock out affected users. A migration would verify against both forms during a transition and rehash on login.

## 010: Email addresses are restricted to printable ASCII
- Addresses are trimmed and lowercased, and must be a plain `local@domain` in printable ASCII: no display names,
  quoted local parts, comments, IP-literal domains, or domains whose final label is all digits.
- Why ASCII-only:
  - **Delivery:** non-ASCII addresses need SMTPUTF8 support end to end, which email providers support unevenly.
    Verification is mandatory, so an address we can't reliably email can't complete signup.
  - **Look-alikes:** Unicode confusables (Cyrillic `а` vs Latin `a`) would allow distinct accounts that look identical.
  - **Canonicalization:** Unicode addresses have several normalization and case-folding forms, so one mailbox could
    map to several stored strings, i.e. duplicate accounts.
- Header-injection safety comes from rejecting whitespace and control characters, not from the ASCII restriction.
- Cost: users whose address contains non-ASCII characters can't register. Users of internationalized domains can
  still type the domain's ASCII (punycode) form (`ana@xn--espaa-rta.es`). Clients must show a clear message.
- Possible later relaxation: accept Unicode domains by converting them to punycode (`golang.org/x/net/idna`) before
  storing. Non-ASCII local parts only if a real need appears.

## 011: Registration (M1 step 4)
- `POST /v1/auth/register` answers 202 `{"status":"accepted"}` for a new **and** an existing address. The existing
  account is left untouched (no password change, no new token) and its owner gets the "account exists" email.
  Both paths hash the password and send email in the background, so timing doesn't reveal accounts either.
- User and verification token are inserted in one transaction (`ON CONFLICT DO NOTHING` on the email, so concurrent
  sign-ups for one address are safe). Email is sent only after commit.
- Verification links expire after **24 hours** (DB clock). Tokens use `NewToken("")`, without a prefix.
- Error responses: `{"error":{"code":"…"}}`; `validation_failed` (422) adds `"fields":[{"field":"…","code":"…"}]`
  with all failing fields. Malformed JSON, unknown fields, trailing data or a body over 8 KiB: 400
  `invalid_request`. Anything else: 500 `internal_error`, details only in server logs.
- **If the email fails after commit**, the failure is logged (kind and error only) and the response stays 202;
  there is no retry. The unverified account remains and the user recovers with resend-verification, which replaces
  the token. Durable delivery (an outbox) is deferred.
- Production refuses to start while only `LogSender` exists (see 007).
- **Deferred to the rate-limiting/hardening step:** registration can be abused to send email to any address
  (email bombing) and to spend argon2 CPU and memory (19 MiB per hash); background sends aren't capped either.
