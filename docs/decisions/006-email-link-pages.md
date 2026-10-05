# 006: Email links open small backend-served HTML pages

> **Status:** in force.
>
> **Worked out in:** 017 (the reset-password page), 019 (the verify-email page).
>
> **Current rules:** `.claude/rules/auth.md`.

- GET renders a confirm button or form, and only POST consumes the token (so mail-scanner prefetch is harmless).
- The pages call the same service as the JSON API, so in-app deep links can be added later with client work only.
