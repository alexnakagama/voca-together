# 009: Passwords are NFKC-normalized before hashing and policy checks

> **Status:** in force.
>
> **Current rules:** `.claude/rules/auth.md`.

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
