# Security Policy

Security policy for **tabnas/parser**. The organization-wide policy in
[tabnas/.github](https://github.com/tabnas/.github/blob/main/SECURITY.md)
is canonical; this file records it here so the policy is present in the
repository it applies to.

## Reporting a vulnerability

**Please do not open a public issue for security problems.**

Report privately, either:

1. **Preferred:** via GitHub's private vulnerability reporting — *"Report a
   vulnerability"* under this repository's **Security** tab; or
2. by email to **richard@ricebridge.com**, subject line beginning
   `[SECURITY]`.

Please include the affected version(s), which implementation is affected
(TypeScript, Go, or both), the impact, and a reproduction if you have one.

## What to expect

- **Acknowledgement** within 72 hours.
- An initial **assessment** within 7 days.
- A fix targeted within **90 days**, with a disclosure date coordinated with
  you. Reporters are credited unless they ask not to be.

## Supported versions

Security fixes are applied to the **latest release** only. Older versions do
not receive fixes.

## Scope notes

Parsers are a common attack surface. Crashes and hangs on malformed input
(denial of service via pathological documents) are **in scope** — this
repository parses untrusted text by design. Vulnerabilities in third-party
dependencies belong upstream, but tell us if this package is exploitable
through one.
