# 018: Authentication hardening and abuse protection (M1 step 11)

> **Status:** in force.
>
> **Changed later:** 019 delivered the production sender and the verify-email page (deferred at the end).
> 020 put Google sign-in on `ip_login` and `account_login` and added a cleanup of Google token uses, which
> 026 removed. 027 and 029 added per-user limits on protected writes, which this decision had left
> unlimited; there are now nine limiters, not seven. 031 added the first limit on a protected read
> (`user_member_read`, for reads that name another member), which this decision had also left unlimited:
> ten limiters.
>
> **Current rules:** `.claude/rules/backend.md` (API conventions), `.claude/rules/auth.md`,
> `.claude/rules/config.md`.

- **Two limiter layers, in process** (`internal/ratelimit`: a token bucket per key, one mutex, no goroutine, no
  dependency). Per client IP in `server`, wrapped around individual routes in `server.New` like `authn`; per account in
  `auth.Service`, which owns `NormalizeEmail`. `main` builds both (`server.NewIPLimits`, `auth.NewAccountLimits`); a
  nil limiter allows everything, so tests pass zero values. `x/time/rate` (008) was not adopted: a ~100-line bucket
  gives fixed-size keys, a lossless sweep and a fake clock with no dependency.
- **Per IP** (checked before the body is read, so junk costs a token too):
  | Bucket | Routes | Burst | Refill |
  |---|---|---|---|
  | `ip_login` | login + google (shared, 020) | 20 | 1 / 3 s |
  | `ip_register` | register | 10 | 1 / min |
  | `ip_email` | resend-verification + forgot-password (shared) | 5 | 1 / 2 min |
  | `ip_token` | verify-email + reset-password JSON + reset form POST (shared) | 10 | 1 / 6 s |
  | `ip_refresh` | refresh | 60 | 1 / s |

  Generous enough for a household, a classroom or a carrier NAT behind one address; they bound what one source can
  make us do (argon2, outbound email, DB load). Token guessing is already infeasible (256-bit), so `ip_token` only
  bounds load. Not limited: `/healthz`, `GET /reset-password` (no DB), logout and `/v1/me` (malformed tokens never
  reach the DB; general API limiting comes with the rest of the API).
- **Per account** (key: SHA-256 of the normalized address, so no addresses sit in memory):
  `account_login` 10 then 1/min (≈ 1 450 guesses a day at most against one account, from any number of IPs);
  `account_mail` 5 then 1/h, **shared by register, resend and forgot** (≤ ≈ 29 emails a day reach any mailbox from
  us, whatever the mix of endpoints). Every attempt consumes, whatever its outcome (no refund on success), so the
  limit shows nothing about outcomes.
- **No enumeration:** the per-account key is the address **whether or not an account exists**, the check runs after
  input validation (422 first) and **before any DB or argon2 work**, and unknown addresses consume too (forgot and
  resend send them nothing but still count). Unknown, unverified and verified addresses therefore get the identical 429
  on the identical attempt with the same `Retry-After`, at the same cost.
- **Accepted cost:** anyone can keep a victim's `account_login` or `account_mail` bucket empty (about one request a
  minute, or five an hour). The victim then waits for `Retry-After`; no token or session is affected. Every per-account
  limit has this property; risk-based challenges are deferred.
- **429** `{"error":{"code":"rate_limited"}}` with `Retry-After` (whole seconds, rounded up, ≥ 1) and
  `Cache-Control: no-store`; the reset form gets a "Too many attempts" page. A 429 did nothing, so retrying after
  `Retry-After` is safe everywhere, **including refresh with the same refresh token** (an explicit exception to 014's
  "never retry a refresh": that rule is about responses that may have been lost, a 429 never rotated anything).
- **Limiter memory:** ≈ 100 bytes per key; entries whose bucket is full again are swept (lossless: a full bucket
  equals a missing one) at most once a minute, lazily inside `Allow`. Each limiter tracks at most 100 000 keys (≈ 10 MB;
  seven limiters ≈ 70 MB worst case). A new key at the cap is **allowed untracked** and logged. Principle: *limiters
  degrade open, resource bounds degrade closed.* Failing closed would let anyone who can mint keys (attacker-chosen
  emails, IPv6 /48s) lock everyone out; the process stays safe regardless, because argon2 slots, the queue timeout, the
  DB pool, the request deadline and the email cap are hard bounds. Logs are aggregated per limiter and minute
  (`ratelimit: rejected` with a count; WARN when the cap is hit) and never contain keys.
- **Single instance only:** buckets are per process, so N instances behind a load balancer give ≈ N× the limits, and
  a restart refills every bucket (one extra burst per deploy, accepted). Before scaling out, move limiting to the edge
  (proxy/CDN per-IP limits) or to shared state. Redis would be exact but is a new stateful service to run and secure,
  with its own failure mode on the login path; not justified for one instance. The first candidate is a
  PostgreSQL-backed counter (no new infrastructure, one write per limited request).
- **Client IP and trusted proxies:** the app speaks plain HTTP; TLS ends at a reverse proxy. `TRUSTED_PROXY_HOPS=N`
  takes the client IP from the N-th `X-Forwarded-For` entry from the right (all XFF headers as one list; entries
  further left are client-controlled and never used); 0 uses the TCP peer. This is valid **only under the deployment
  invariant that the server is reachable exclusively through those N proxies**. If the server is also exposed directly
  while trusting XFF, any client can send its own header and choose its IP, i.e. its bucket: **client IP spoofing**.
  Keep the listener on a private network or firewall it to the proxy. Fail-safe: when the chain is shorter than N or
  the entry isn't an IP, the TCP peer is used, so such requests share the proxy's bucket rather than pick their own.
  IPv4-mapped addresses are unmapped and IPv6 is keyed by its /64 (one subscriber usually holds a whole /64).
  Production requires `TRUSTED_PROXY_HOPS` to be set explicitly (0 included), so the topology is always stated.
- **argon2 queue bound** (closes 013's deferral): waiting for a hash slot ends after 5 s with `auth.ErrOverloaded` →
  503 `service_unavailable`, `Retry-After: 5`. One queue serves every account state, so a 503 reveals nothing.
- **Request deadline:** each request context gets 10 s (below the 15 s `WriteTimeout`, so the 503 can be written;
  the `http.Server` timeouts never cancel the context, so a request could otherwise wait indefinitely for a DB
  connection). `context.DeadlineExceeded` → 503 (WARN); a client disconnect (`Canceled`) stays an opaque 500.
  **The deadline can fire after a transaction committed but before the response is written.** The client then sees a
  503 for work that was done, exactly like an unknown COMMIT outcome (017), which stays as is. What a retry does:
  - Safe, same end state: **verify-email** and **reset-password** (the retry gets `token:invalid`: the change stands,
    and the client tells the user to log in); **logout** (idempotent 204).
  - Safe, with a side effect: **register**, **resend-verification**, **forgot-password** (another email, or nothing for
    an existing account; each retry spends `account_mail`); **login** (another session; the orphaned one expires).
  - Not retriable with the same credential: **refresh**. If the rotation committed, the old refresh token is now the
    previous one and retrying it is reuse: the session is revoked (014) and the client logs in again, as after any lost
    refresh response. Clients must treat a 503 from refresh like a network error, and a 429 as safe to retry.
- **Email is best effort until a durable outbox exists.** A 202 from register, resend or forgot means *accepted*, never
  *delivered*. At most 32 background sends run at once (≥ 3 emails/s even if every send takes its 10 s timeout); when
  all are busy the message is **dropped** and logged (`auth: email dropped, too many sends in flight`, `kind` only). The
  response is unchanged, so neither enumeration nor timing is affected; the user asks again (within `account_mail`).
  This bounds goroutines when a slow provider meets abuse. A future outbox replaces `sendInBackground`'s body (insert in
  the request's transaction, deliver from a worker) without changing callers, and makes the drop policy obsolete.
- **Retention cleanup:** `auth.Service.RunCleanup`, started by `main` (first run after 1 min, then hourly; stopped
  and waited for on shutdown). It deletes, in 1 000-row batches, sessions whose absolute expiry, sliding refresh expiry
  or revocation is more than **30 days** past, and one-time tokens (used or not) that expired more than **30 days**
  ago; users are never deleted. Responses don't change: those tokens were already rejected. Rows are taken with
  **`FOR UPDATE SKIP LOCKED`**, so cleanup **never waits for a lock** and can't join a deadlock cycle with the token
  flows (user row, then token rows) or refresh/logout (one session row); a locked row is left for the next run.
  Deleting a referencing row locks nothing in `users`. Idempotent, so several instances may run it. Logs
  `auth: cleanup` with counts only.
- **Headers:** every response gets `X-Content-Type-Options: nosniff`, `Content-Security-Policy: default-src 'none';
  frame-ancestors 'none'` and `Referrer-Policy: no-referrer`; the reset page replaces the CSP with its own (017).
  `Strict-Transport-Security: max-age=31536000` in production only (no `includeSubDomains`: we don't own the policy of
  sibling hosts). Register, verify-email and resend now also send `Cache-Control: no-store`, like every other auth route.
- **Transport and config:** `MaxHeaderBytes` 32 KiB (default 1 MiB). `APP_BASE_URL` must not carry credentials, a
  query or a fragment (links append a path and `?token=`), and in production must not be localhost, a loopback or the
  unspecified address.
- **Password reset** needs nothing beyond the above: forgot is limited per address and IP, reset submissions per IP,
  tokens are 256-bit, single use and live 30 min, argon2 runs only for a live token, and policy failures don't consume
  it (017).
- **No migration.** Limits live in memory; cleanup uses existing columns. Its predicates scan both tables once an hour
  in small batches, fine at M1 scale; an `expires_at` index waits for measured need.
- **Deferred:** shared or edge rate limiting before running several instances; a per-user session cap (bounded by
  `account_login` for now; revoking other sessions inside login needs its own lock analysis, since two concurrent logins
  could deadlock); logout-all, session list, rotation lineage (014, 015); risk-based challenges and failed-login
  notifications; metrics, request IDs and IP logging for abuse forensics (needs a privacy decision); durable email
  outbox and a production email sender (sender and verify-email page done in 019); enforcing TLS in `DATABASE_URL`; the
  verify-email page; password change for logged-in users.
