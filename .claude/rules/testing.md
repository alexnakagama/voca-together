---
paths:
  - "backend/**/*_test.go"
  - "backend/internal/testutil/**"
  - "mobile/test/**"
---

# Testing rules

Commands are in `CLAUDE.md`.

## Backend

- DB-backed tests use `testutil.DB(t)`, which migrates and TRUNCATEs a single shared database
  (`users, user_tokens, sessions, user_identities`; new tables are added there with their migration).
- They skip silently when `TEST_DATABASE_URL` is unset. Never use `t.Parallel` in them and always keep `-p 1`.
- Test doubles: `email.Recorder` for sent email, `googleid.Fake` for Google verification.

## Mobile

- Session tests are host-only and use `test/support/fakes.dart`: `FakeServer` on `MockClient`, `InMemoryTokenStore`,
  `FakeAuthClock`.
- Google: `FakeGoogleIdentity` (`pumpApp(google: …)`), and `FakeGooglePlatform` under the real adapter.
- Widget tests use `test/screens/harness.dart` (the real app over `FakeServer`).
- `test/screens/accessibility_test.dart` and `privacy_test.dart` cover every screen state.
- `test/ui/previews_test.dart` lists every preview function.

## Enforcement tests

- `test/architecture_test.dart` enforces the import allowlist, fails if `lib/screens/` or `lib/ui/` names a token
  type or value, and forbids `SessionStatus`, `.status` and `authRedirect` in `lib/screens/`.
- `test/leak_test.dart` checks redaction and where each secret travels.
