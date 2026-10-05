# 003: argon2id for passwords

> **Status:** in force.
>
> **Worked out in:** 009 (NFKC before hashing), 013 (concurrency limit, rehash on login), 018 (queue timeout).
>
> **Current rules:** `.claude/rules/auth.md`.

- OWASP baseline parameters (m=19 MiB, t=2, p=1), stored in PHC format so they can be raised later with rehash-on-login.
- Policy (NIST 800-63B): 10–128 characters, no composition rules, reject common passwords and passwords equal to the email.
