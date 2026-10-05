# 002: Opaque, DB-backed session tokens instead of JWT

> **Status:** in force.
>
> **Worked out in:** 013 (issuance), 014 (rotation and reuse), 015 (logout), 016 (authenticating a request).
>
> **Current rules:** `.claude/rules/auth.md`.

- Access token (`vt_at_…`, 15 min) and refresh token (`vt_rt_…`, 30 days sliding, 90 days absolute), each 32 random bytes.
- Only SHA-256 hashes are stored (in `sessions`).
- Why: logout, revoke-all-on-password-reset and refresh-reuse detection all need server state anyway, so a JWT would add
  key management and algorithm pitfalls without making the system stateless. One indexed lookup per request is cheap
  at our scale. Clients treat tokens as opaque, so we can still switch later.
- Refresh rotates on every use. Presenting an already-rotated refresh token revokes the whole session.
