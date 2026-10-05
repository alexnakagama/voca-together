# 007: Email behind an interface

> **Status:** in force.
>
> **Worked out in:** 018 (bounded, best-effort background sends), 019 (Resend as the production sender).
>
> **Current rules:** `.claude/rules/auth.md`.

`email.Sender` with a dev `LogSender` and a test `Recorder`. The production provider is Resend (019).
- `email` handles delivery only. `auth` owns the content, links and expiry wording. The dependency runs
  `auth → email`, never the reverse.
- Production must never run `LogSender`: it logs message bodies, which contain live tokens.
  There is no silent fallback (production requires Resend, 019).
- Auth sends email in the background (so response timing doesn't reveal accounts), with its own timeout
  derived via `context.WithoutCancel`, never the finished request's context. `Recorder` rejects cancelled
  contexts, so tests catch that mistake.
