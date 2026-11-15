# Security hardening and fuzz suite (v1.1.0)

Status: approved · Author: Mike Helmick · 2026-11-14

## Goal

Every verified finding from the 2026-11 security scan is fixed. Each fix has a
regression or fuzz test that fails on v1.0.1, and a checked-in native Go fuzz
suite covers every package that handles untrusted input. All of it ships as
v1.1.0 with a migration guide, an upgrade skill, and advisories for the two
exploitable issues.

## Decisions

| ID | Decision | Door |
|---|---|---|
| D1 | No exported signature changes. Secure defaults may change. Ship v1.1.0 with a migration section. | one-way |
| D2 | `SecureHeaders` stops trusting `X-Forwarded-Host` by default. Apps opt in with a new variadic option. `X-Forwarded-Proto` stays trusted, because distrusting it would loop redirects behind TLS-terminating LBs. | one-way |
| D3 | Azure: no envelope format, so stored ciphertext is unchanged. Behavior is set in D23. | one-way |
| D4 | `cookiestore.New` always sets `HttpOnly` and defaults `SameSite` to Lax when unset. `Secure` is set by D21. | one-way |
| D5 | `ServeHTTPHandler` adds `IdleTimeout` 120s and `ReadTimeout` 5m. No `WriteTimeout`, so streaming keeps working. | two-way |
| D6 | Superseded by D16. | two-way |
| D7 | `HandleCSRF` caps the request body before parsing the form, adjustable through a new variadic option `WithCSRFMaxBodyBytes`. The default is set by D22. | one-way |
| D8 | All low-severity findings get fixed. `toJSON` is fixed, not removed. SECURITY.md is updated for v1. | two-way |
| D9 | Native `go test -fuzz` targets sit next to the code, with seeds in `testdata/fuzz/`. Seeds replay in normal `go test`. `make fuzz` runs every target. A nightly workflow fuzzes 60s per target; a crash fails the job (no artifacts, D19). Each fix lands with a test that fails first. | two-way |
| D10 | New `skills/go-bananas-upgrade/` skill analyzes an app for v1.1.0-affected usage and guides the migration. It is structured per version. | two-way |
| D11 | Nothing is pushed until the work is done. Phases are stacks of local branches, each reviewed before the next is stacked on it. One push at the end, with approval. | two-way |
| D12 | GitHub security advisories for #1 (CSRF bypass) and #2 (key traversal) are published at the v1.1.0 release. Claude drafts them and the owner publishes. | one-way |
| D13 | The `go` directive stays at `1.27`. The release notes recommend Go ≥ 1.27.4 for GO-2026-6859. CI pins 1.27.4 and runs `govulncheck`. | two-way |
| D14 | `MutateMethod` considers only `POST` with `Content-Type: application/x-www-form-urlencoded`, and reads at most 64 KB of the body. Multipart bodies are never parsed by it. *Changed during the build:* it buffers at most 64 KB+1, parses with `url.ParseQuery`, restores the body and never calls `ParseForm` (see plan Task 1b). | two-way |
| D15 | `SecureHeaders` adds `WithSSLHost(host)` and `WithAllowedHosts(hosts...)`. `WithTrustedHostHeaders` is documented as requiring `WithAllowedHosts`. The fuzz property becomes "the Location host is in the allowlist". | two-way |
| D16 | `TemplateConfig` gains `AllowedHosts []string`. Outside dev mode with an empty `ServerEndpoint`, the derived host is used only if it is allowlisted; otherwise it falls back to the first allowed host, or to empty plus the warning. The example sets `ServerEndpoint`. Supersedes D6. | two-way |
| D17 | Sessions record a created-at time. A new opt-in `CheckSessionMaxAge(maxAge, onExpired)` middleware (modeled on `CheckSessionIdleNoAuth`) enforces an absolute lifetime. The docs say cookie sessions can't be revoked except by rotating keys. | two-way |
| D18 | `cookiestore.New` copies the options before changing them and treats `SameSiteDefaultMode` as unset. *Changed during the build:* `SameSite=None` forces `Secure` at construction (even with `WithInsecureCookies`), instead of a `Save` error, so `New` keeps returning `*sessions.CookieStore` for callers that type-assert it. Per-session `Options` overrides are not policed (documented). The migration notes call out `form_post`/SAML. | two-way |
| D19 | `fuzz.yml` uploads no artifacts; a failure only fails the job, and the maintainer reproduces it locally. It sets `permissions: contents: read`, pins actions by SHA, and pins the `govulncheck` version. | two-way |
| D20 | v1.0.2 is cut from `v1.0.1` with only the #1 and #2 fixes, released the same day as v1.1.0. Go vuln DB reports for `middleware.MutateMethod` and `keys.(*Filesystem).Decrypt` are drafted by Claude and submitted by the owner. | one-way |
| D21 | `cookiestore` always sets `Secure`. A new variadic `cookiestore.New(fn, opts, ...Option)` adds `WithInsecureCookies()` for local HTTP dev. | one-way |
| D22 | The CSRF body cap defaults to 1 MiB and applies only to non-safe methods (as built, only when the token must be read from the body). Upload routes use a subrouter with their own `HandleCSRF`. | one-way |
| D23 | Azure `Encrypt` errors when `len(aad) > 0`. Azure `Decrypt` logs a warning once and still decrypts, so existing data stays readable. (API 7.0 with RSA-OAEP-256 never supported AAD.) | one-way |
| D24 | *Added during the build.* Split session cookies are bound to the main session by a private random ID; they are joined only when the ID matches and the session isn't new, and they are expired on logout (`MaxAge < 0`). Unbound v1.0.x split cookies are dropped once. | one-way |
| D25 | *Added during the build.* The stack order is 1a→1b→1c→2a→2b→2c→2d→3a→4a→4d→3b→4b→4c→5a→5b (4a/4d moved ahead of 3b). Each branch is one PR. | two-way |

## Findings in scope

| # | Sev | Finding | Fix |
|---|---|---|---|
| 1 | High | `MutateMethod` (`middleware/method.go`) rewrites any method, including POST to GET, from body *or query*. Used via `r.Use` it runs after routing, so the POST handler runs while `HandleCSRF` sees GET and skips validation. | Override only when the original method is POST, the value comes from `PostFormValue`, and the target is in {PUT, PATCH, DELETE}. Document that it must wrap the router. |
| 2 | Med | `keys.Filesystem.Decrypt` uses the ciphertext's version prefix unvalidated, so `../otherKey/1:` decrypts with another key. | Version must parse with `strconv.ParseUint`. Generated versions are `UnixNano` digits. |
| 3 | Med | `SecureHeaders` trusts `X-Forwarded-Host` for the HTTPS redirect, so an attacker controls the 301 target and can poison caches. | D2. |
| 4 | Med | The template `serverEndpoint` comes from the client Host header when unset. | D16 (supersedes D6). |
| 5 | Med | `secrets/vault` defaults `version=1`, so rotation is ignored. | Omit the version parameter when it is not given, so Vault returns the latest. |
| 6 | Med | Azure KMS silently ignores AAD. | D3. |
| 7 | Med | `cookiestore.New` sets no HttpOnly/SameSite defaults. | D4. |
| 8 | Med | `ServeHTTPHandler` sets only `ReadHeaderTimeout`. | D5. |
| 9 | Med | GO-2026-6859 (net/http) is reachable. CI pins Go 1.27.2. | D13. |
| 10 | Med | `cache.WriteThruLookup` holds the cache-wide lock during the backend lookup. | Per-key in-flight coalescing, with the lookup done outside `c.mu`. |
| 11 | Low | `HandleCSRF` parses up to 32 MB of multipart (plus unbounded temp files) before authentication. | D7. |
| 12 | Low | Cache TTL overflow (`time.Now().Add(huge)`) means every Set misses. One purge goroutine starts per expired hit. | `New` returns `ErrInvalidDuration` above 100y. Purge once per key. |
| 13 | Low | The Azure vault name is formatted into the URL, so a crafted name sends the bearer token to another host. | Validate `^[A-Za-z0-9-]{3,24}$` in secrets and keys. |
| 14 | Low | Vault `Signer.Sign` omits `key_version`, so signatures don't match the advertised key after rotation. | Send `key_version`. |
| 15 | Low | `RenderCSV` `Content-Disposition` filename is unquoted, allowing parameter injection. | `mime.FormatMediaType`. |
| 16 | Low | `escapeJSON` escapes only `"`. | `json.Marshal` the message. |
| 17 | Low | The `toJSON` template func returns `[]byte`, which renders as garbage and invites a `safeJS` misuse. | Return `template.JS` of escaped JSON (html/template-safe, `</script>` escaped). |
| 18 | Low | HSTS `preload` is on by default. | Make preload opt-in via a `SecureHeaders` option. Keep includeSubDomains and max-age. |
| 19 | Low | `flash` panics if a session flash key holds a non-map value. | Type-check and reset. |
| 20 | Low | `response.Back` doesn't check the Referer scheme. CSRF treats TRACE as safe. | Require http/https. Drop TRACE. |
| 21 | Low | The example app's logout is a GET (CSRF logout) and its state compare is not constant-time. | Logout becomes POST with a CSRF token. `subtle.ConstantTimeCompare`. |
| 22 | Low | `RenderJSON` returns `err.Error()` to clients, a documented but easy leak. CSRF tokens are not rotated on login. | Document both. Recommend `ClearCSRFToken` on privilege change. |
| 23 | Med | Cookie sessions have no absolute lifetime, so a stolen cookie lives forever. | D17. |
| 24 | Low | The example OIDC callback clears flow state in a `defer`, after the session is already saved. | Clear before writing the response. Document that session writes after the first byte are lost. |
| 25 | Low | The example admin check trusts an unverified email. | Require `email_verified`. |
| 26 | Low | HEAD/OPTIONS skip CSRF on routes without `.Methods()`. | Docs and the upgrade skill. |
| 27 | High | *Found during the build; exists in v1.0.1.* A captured split cookie (`_gbs-<key>`) replayed alone, with no main cookie, is joined into a fresh session, so values stored there (e.g. ID tokens) survive logout and max age. | D24. Backport scope is pending an owner decision. |
| 28 | Low | *Found by fuzzing.* `toStringSlice` panicked on nil, so `{{joinStrings .missing ", "}}` returned a 500. | A nil value is rejected like any other non-slice. |
| 29 | Low | *Found by fuzzing.* `RealHostFromRequest` dropped IPv6 brackets. | `net.JoinHostPort`. |

## Design

**Option plumbing (D2, D7, D18).** `SecureHeaders(devMode, serverType, opts ...SecureOption)`
and `HandleCSRF(h, opts ...CSRFOption)` use functional options. The variadic
parameter keeps every existing call compiling. The only thing that breaks is
code assigning these functions to a typed func variable, which is called out
in the migration notes. New options:

- `WithTrustedHostHeaders(headers ...string)`: restores `HostsProxyHeaders`.
- `WithHSTSPreload()`
- `WithCSRFMaxBodyBytes(n int64)`

**MutateMethod (#1).** The signature is unchanged. Rules:

- Only `POST` requests are considered.
- The value is read via `r.PostFormValue`.
- The value is uppercased and must be in {PUT, PATCH, DELETE}. Anything else
  is ignored and the method stays POST.

The method can never be downgraded to a safe method, so CSRF always sees a
non-safe method. The doc comment says it must wrap the router
(`MutateMethod()(router)`). `r.Use` cannot change routing, but it is no
longer a security hole.

**Fuzz suite.** Targets are named `Fuzz<Thing>` in `<pkg>/fuzz_test.go`. Seeds
go via `f.Add` plus regression inputs in `testdata/fuzz/Fuzz<Thing>/`.
`make fuzz` enumerates targets with `go test -list '^Fuzz'` per package and
runs `go test -run '^$' -fuzz '^<T>$' -fuzztime $(FUZZTIME)` for each.
`.github/workflows/fuzz.yml` runs nightly and on `workflow_dispatch` with
`FUZZTIME=60s`. A failure only fails the job; no artifacts are uploaded (D19).
Properties are invariants, not just "no panic":

| Package | Targets (property) |
|---|---|
| middleware | `FuzzMutateMethodCSRF` (no handler run for non-safe method without valid token; final method ∈ {orig, PUT, PATCH, DELETE}), `FuzzCSRFToken` (mask/unmask round-trip; arbitrary header/body never panics or validates), `FuzzSecureHeadersRedirect` (Location host ∈ {r.Host}), `FuzzTraceID` (stored ID matches `^[A-Za-z0-9_-]{1,128}$`), `FuzzProcessLocale`, `FuzzRequireHostHeader` (accepts only allowlisted), `FuzzStaticPath`, `FuzzCSP` |
| keys | `FuzzFilesystemDecrypt` (round-trip; any byte flip or non-digit version rejected; no panic), `FuzzParseECDSAPublicKey`; azure: `FuzzConvert1363ToASN1`, `FuzzParseP256PublicKey` |
| secrets | `FuzzFilesystemSecretName` (never reads outside root), `FuzzResolver` (non-`secret://` passthrough; `target=file` writes only under SecretsDir), `FuzzJSONExpander`; aws/vault ref parsers after extraction to pure funcs |
| cookiestore | `FuzzHotCodecDecode` (no panic; tamper/wrong-name rejected; round-trip) |
| internal/util | `FuzzSafeJoin` (result under root), `FuzzBase64Decode` (std/url agree), `FuzzTrimSpace` (idempotent) |
| form | `FuzzBind` (urlencoded + multipart; no panic; bounded memory) |
| render | `FuzzRenderJSON` (`json.Valid`), `FuzzRenderCSVFilename` (`mime.ParseMediaType` round-trips), `FuzzRenderHTMLEscaping` (no raw `<` from data), `FuzzToJSON` (output parses as JSON, no `</script>`) |
| response | `FuzzBack` (Location is `/` or same-host http(s)), `FuzzRealHost` |
| i18n | `FuzzLocaleLookup` (result is default or a loaded key) |
| flash | `FuzzFlashSession` (no panic on arbitrary session values) |
| cache | `FuzzCacheOps` (matches a map model), `FuzzCacheTTL` (New errors or Set→Lookup hits) |
| logging | `FuzzLevelFromString` |
| examples/ssr-oidc | `FuzzCallbackState` (wrong state → 400, no token exchange, flow keys cleared) |

**Upgrade skill (D10).** `skills/go-bananas-upgrade/SKILL.md` contains:

- Detection: `go list -m` for the current go-bananas version.
- A per-version checklist, starting with v1.0 → v1.1. For each item it gives a
  grep pattern, what to look for, and the fix.
- A final verify step: build, test, `govulncheck`.

It is linked from the release notes and the docs site.

## Phases

All branches are local (D11). Each branch is one reviewable PR in the stack.

1. **Critical fixes.** Works when: the regression tests for #1 and #2 fail on
   `main` and pass on the stack; `make test test-examples lint` is green.
   - 1a: CI Go 1.27.4 + `govulncheck` step.
   - 1b: `MutateMethod` fix + `FuzzMutateMethodCSRF`.
   - 1c: Filesystem key version validation + `FuzzFilesystemDecrypt`.
2. **Medium hardening.** Works when: each finding #3–#8 and #10–#14 has a test
   that fails before its fix; full checks are green.
   - 2a: middleware options (D2, D7, HSTS preload, D6 warning).
   - 2b: secrets/keys providers (#5, #6, #13, #14).
   - 2c: cookiestore (D4) + server timeouts (D5).
   - 2d: cache (#10, #12).
3. **Low fixes.** Works when: tests for #15–#21 fail first and then pass.
   - 3a: render/response/flash (#15–#17, #19, #20).
   - 3b: example app (#21) + its e2e test.
4. **Fuzz suite + CI.** Works when: `make fuzz FUZZTIME=10s` runs every target
   with zero crashers; seeds replay in `make test`; `fuzz.yml` passes
   `actionlint`.
   - 4a: Makefile `fuzz` target + `fuzz.yml`.
   - 4b–4d: remaining targets by package group.
5. **Docs, skill, release prep.** Works when:
   - the upgrade skill, run against a scratch copy of `examples/ssr-oidc`
     pinned to v1.0.1 usage, flags every affected call;
   - the docs site builds;
   - the release notes and advisory drafts are ready for owner review (the
     drafts are kept outside the repo).
   - 5a: docs pages + SECURITY.md + existing skill examples.
   - 5b: `go-bananas-upgrade` skill.
6. **v1.0.2 backport (D20).** Works when: branch `release/v1.0.2` off tag `v1.0.1` carries only the #1 and #2 commits (with D14's urlencoded-only and 64 KB cap) and their tests, and `make test test-examples lint` is green on it.

## Validation

- Harness (existing): `make test`, `make test-acc` (race), `make test-examples`
  (includes `examples/ssr-oidc/e2e_test.go`), `make lint`, `govulncheck`.
  New: `make fuzz`.
- Every branch: `make test test-examples lint` plus `go run
  golang.org/x/vuln/cmd/govulncheck@latest -tags=google ./...`.
- Fix branches: the new test is run against the parent commit first to prove
  it fails.

## Security

This section records the design-security-reviewer findings (2026-11-14) and how each was resolved.

| Rev | Finding | Resolution |
|---|---|---|
| H1 | `MutateMethod` runs before everything else, and `PostFormValue` parses up to 32 MB of multipart (plus unbounded temp files) before CSRF's cap applies. | D14 |
| M1 | v1.1.0 is the only patch for #1/#2, and it bundles behavior changes. A GHSA alone doesn't reach `govulncheck`. Fuzz crash inputs uploaded as artifacts on a public repo become public PoCs. | D20, D19 |
| M2 | With `X-Forwarded-Host` distrusted, the HTTPS 301 still uses `r.Host`. `WithTrustedHostHeaders` re-opens the original bug. | D15 |
| M3 | A warning (D6) doesn't stop `serverEndpoint` poisoning. | D16 (supersedes D6) |
| M4 | Cookie sessions re-save on every request, so a stolen cookie lives forever and logout can't revoke it. | D17. Added as finding #23 |
| M5 | D3 breaks `Decrypt` of existing Azure data that was encrypted while passing AAD. | D23 |
| M6 | `Secure` stays false. `New` mutates the caller's options. `SameSiteDefaultMode` isn't treated as unset. `None` without `Secure` gets dropped by browsers. Lax breaks `form_post`/SAML callbacks. | D18, D21 |
| M7 | A 32 MB CSRF cap equals today's in-memory limit, and the option is per instance. | D22 |
| M8 | Cross-origin POST → DELETE/PUT skips the CORS preflight. | Docs and the upgrade skill: any route reachable by `MutateMethod` must be behind `HandleCSRF` |
| L1 | Filesystem `Encrypt` can choose a non-numeric "latest" (e.g. `.DS_Store`), and versions are compared as text. | Fold into #2: same `ParseUint` filter in Encrypt/`SigningKeyVersions`, numeric compare |
| L2 | Azure `signingKeyVersion.Signer` passes a full Kid URL into `parseKeyID`. | Fold into #13: one helper for all five URL builders; return `VAULT/KEY/VERSION` IDs |
| L3 | The example OIDC callback deletes its flow keys in a `defer`, which runs after the session is already saved. | Added as finding #24, fixed in 3b: clear the keys before writing the response; document "session changes after the first byte are lost" |
| L4 | The example's admin check trusts an unverified email. | Added as finding #25, fixed in 3b: require `email_verified` |
| L5 | `toJSON`: also check `<!--` and U+2028/9. The text-template variant must return `string`. `safeHTML` is a footgun. | Fold into #17; document `safeHTML` |
| L6 | HEAD/OPTIONS bypass CSRF on routes that don't call `.Methods()`. | Added as finding #26: docs and the upgrade skill |
| L7 | A variadic parameter changes the func type. | Run `apidiff` in phase 5 and list the result in the release notes |
| L8 | Unpinned tools and actions. | D19 |
| L9 | Cache coalescing: no cached errors, and a panicking lookup must not hang waiters. | Fold into 2d (own per-key map, no new dependency); fix the stale doc comment |
| — | D11 means intermediate branches never get remote CI. | Accepted: the gate runs locally on every branch |

## Rollout

**Library / SDK**

- **Version:** v1.1.0 (minor, per D1). There is no CHANGELOG file; release
  notes go in the GitHub release and include a "Migration from v1.0.x"
  section that links the upgrade skill and the docs.
- **Compatibility:** behavior changes for #1, #2 (malformed ciphertext only),
  #3, #5, #6, #7, #8, #11, #18. Signatures change only by gaining variadic
  options on `SecureHeaders` and `HandleCSRF`. Opt-outs: the options above,
  `ServeHTTP` with a custom `http.Server`, and explicit `sessions.Options`.
  Nothing is deprecated, so there is no removal timeline.
- **Supported runtimes:** the `go 1.27` directive is unchanged. Go ≥ 1.27.4 is
  recommended (D13). Dependencies were updated in #49.
- **Downgrade:** no persisted formats change. Filesystem and cloud ciphertext,
  cookies and secrets are all compatible, so pinning v1.0.x still works (but
  stays vulnerable).
- **Disclosure:** v1.0.x is exploitable for #1 and #2. Push, merge, tag and
  release v1.0.2 and v1.1.0 in one sitting, then publish the GHSAs (D12) and
  submit the Go vuln DB reports (D20).
- **Migration callouts:** proxies that rewrite Host (D2); `form_post`/SAML
  callbacks under SameSite=Lax (D4); `Secure` breaking local HTTP on Safari or
  non-localhost hosts (D21); uploads over 1 MB (D22); Azure AAD (D23);
  `apidiff` output for the variadic signatures.

**Service / CLI:** n/a. This is a library.

## Open questions (agreed to stay open)

- Real AAD on Azure via Managed HSM A256GCM: this needs the `azkeys` SDK and a new ciphertext format, so it's a separate feature (agreed with D23).
- Migrating the Azure providers off the deprecated `azure-sdk-for-go v68` (`services/keyvault/v7.0`) and `go-autorest` to the `azsecrets`/`azkeys` SDKs: this is dependency work, not a security fix in this release (agreed 2026-11-14).
