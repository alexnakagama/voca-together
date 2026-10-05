# 005: No account enumeration

> **Status:** in force, with one deliberate exception: Google sign-in's 409 `account_exists` (020).
>
> **Worked out in:** 011, 012, 013, 017 (each endpoint), 018 (rate-limit responses keep it).
>
> **Current rules:** `CLAUDE.md` (Security), `.claude/rules/auth.md`.

register, resend-verification and forgot-password always return 202. Login returns the same 401 for an unknown email
and a wrong password. `email_not_verified` (403) is returned only after the password is correct.
