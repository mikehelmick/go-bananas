# Security Policy

go-bananas is a web framework, so security issues in it can affect every
application built on it. We take reports seriously and appreciate responsible
disclosure.

## Reporting a vulnerability

**Please do not open a public issue for security vulnerabilities.**

Report privately via GitHub's private vulnerability reporting:
[Security → Report a vulnerability](https://github.com/mikehelmick/go-bananas/security/advisories/new).

Include what you can: affected package(s) and version, a description of the
issue, reproduction steps or a proof of concept, and the impact you believe it
has. You can expect an acknowledgment within a few days; please allow a
reasonable window for a fix before public disclosure.

## Supported versions

go-bananas is pre-1.0; only the **latest release** receives security fixes.

| Version | Supported |
|---|---|
| latest `v0.x` release | ✅ |
| older releases | ❌ — please upgrade |

## Scope notes

- The `FILESYSTEM` and `IN_MEMORY` secrets/keys providers are intended for
  development and testing, not production secret storage.
- The [`examples/ssr-oidc`](./examples/ssr-oidc) app is demonstration code; its
  dev mode (`DEV_MODE=true`, the default) intentionally enables a password-less
  dev login and relaxed cookie/HTTPS settings and must not be deployed as-is.
- Dependency vulnerabilities are tracked via Dependabot and `govulncheck`;
  reports for issues in third-party dependencies are still welcome if
  go-bananas's usage of them is what makes the issue exploitable.
