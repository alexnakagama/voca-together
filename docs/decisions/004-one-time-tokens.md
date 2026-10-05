# 004: One-time tokens (verification / reset)

> **Status:** in force.
>
> **Worked out in:** 012 (verification), 017 (password reset), 018 (retention cleanup).
>
> **Current rules:** `.claude/rules/auth.md`.

- 32 random bytes; only the SHA-256 hash is stored, in `user_tokens` with a `purpose` column.
- Consumed with a single atomic `UPDATE … WHERE used_at IS NULL AND expires_at > now() RETURNING`, which makes them single-use and replay-safe.
- At most one active token per user and purpose (partial unique index). Issuing a new token deletes the old one.
