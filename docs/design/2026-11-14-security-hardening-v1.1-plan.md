# Security hardening and fuzz suite (v1.1.0) Implementation Plan

> **For agentic workers:** Execution is `design-process:build`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix every finding in the design doc. Each fix gets a test that fails on v1.0.1. Add a checked-in native fuzz suite, CI fuzzing, docs, and an upgrade skill for v1.1.0.

**Architecture:** Fixes stay inside their existing packages. New configuration uses variadic functional options, so no exported signature breaks. Inline parsers in cloud providers are extracted into pure, unexported functions so they can be tested and fuzzed without credentials.

**Tech Stack:** Go 1.27 (CI 1.27.4), gorilla/mux, gorilla/sessions, unrolled/secure v1.18.0, native `go test -fuzz`, GitHub Actions.

**Spec:** `docs/design/2026-11-14-security-hardening-v1.1.md`

## Global Constraints

- No exported signature may change except by adding a trailing variadic option (D1). `SecureHeaders`, `HandleCSRF` and `cookiestore.New` are the only ones that gain one. New exported identifiers are allowed.
- Branches stay local. No `git push` and no `gh pr` until the owner approves at the end (D11).
- Each fix commit includes its test. Run the test on the parent commit and confirm it **fails** before writing the fix.
- Fuzz targets go in `<pkg>/fuzz_test.go` and are named `Fuzz<Thing>`. Seeds go via `f.Add`, and every regression input is also saved under `<pkg>/testdata/fuzz/Fuzz<Thing>/`.
- New `.go` files carry the repo's Apache copyright header; CI's copyright-check enforces it. HTML files contain no tabs.
- Per-branch gate: `make test test-examples lint` passes, plus `go run golang.org/x/vuln/cmd/govulncheck@latest -tags=google ./...` (exit 0 once CI and local are on 1.27.4).
- Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **`MutateMethod` mounted via `r.Use`:** a POST carrying `_method=GET` or `_method=delete` from a cross-site form must still be CSRF-validated. Covered in Task 1b (`TestMutateMethodUseChainCSRF`).
2. **Proxied apps after D2:** behind an LB that sets `X-Forwarded-Proto: https`, a request must not redirect, whether or not `X-Forwarded-Host` is present. Covered in Task 2a (`TestSecureHeadersProxyProtoNoLoop`).
3. **Legitimate uploads on CSRF routes:** a 2 MB multipart upload with a valid token gets 413 by default (1 MB, D22) and succeeds with `WithCSRFMaxBodyBytes(8<<20)`. Covered in Task 2a (`TestCSRFBodyCap`).
4. **Explicit `sessions.Options{HttpOnly:false, Secure:false}` callers:** cookies still come out `HttpOnly` and `Secure` (D4, D21), and the caller's struct is not mutated (D18). Covered in Task 2c (`TestNewCookieDefaults`).
5. **Concurrent `WriteThruLookup` for the same key when the lookup errors:** every waiting caller gets the error, nothing is cached, and the next call retries. Covered in Task 2d (`TestWriteThruLookupErrorNotCached`).

---

## Phase 0: Design (branch `security/v1.1-hardening`, off `main`)

### Task 0: Commit the design doc and plan

- [ ] `git add docs/design/2026-11-14-security-hardening-v1.1*.md && git commit -m "Add v1.1 security hardening design and plan"`

---

## Phase 1: Critical fixes

### Task 1a: CI on Go 1.27.4 with govulncheck (branch `sec/1a-ci`, from `security/v1.1-hardening`)

**Files:** Modify `.github/workflows/ci.yml` (lines 30 and 70, plus a new step in the `lint` job).

- [ ] Change both `go-version: '1.27.2'` to `'1.27.4'`. Add `permissions: contents: read` at the workflow level (D19).
- [ ] Add a `govulncheck` step after `go-lint`, pinned to an exact version (current release from `go list -m -versions golang.org/x/vuln`, D19): `go run golang.org/x/vuln/cmd/govulncheck@<ver> -tags=google ./...`, then the same command in `examples/ssr-oidc`.
- [ ] Locally: install go1.27.4 (`go install golang.org/dl/go1.27.4@latest && go1.27.4 download`) and run the gate with it. Expected: govulncheck reports no vulnerabilities.
- [ ] Run `actionlint .github/workflows/ci.yml` (`go run github.com/rhysd/actionlint/cmd/actionlint@latest`). Expected: no output.
- [ ] Commit `ci: build on Go 1.27.4 and run govulncheck`.

### Task 1b: MutateMethod cannot downgrade or bypass CSRF (branch `sec/1b-mutate-method`, from `sec/1a-ci`)

**Files:**
- Modify `middleware/method.go`.
- Create `middleware/method_test.go` and `middleware/fuzz_test.go`.

**Interfaces:**
- Produces: `MutateMethod() mux.MiddlewareFunc`, signature unchanged.
- Produces: a `switch` over PUT/PATCH/DELETE in the unexported `overrideMethod` (as built; replaces the planned map).

- [ ] **Write the failing tests** in `method_test.go`:
  - `TestMutateMethod` is table-driven over (orig method, body, query) → final method:
    - POST + body `_method=delete` → `DELETE`
    - POST + body `_method=PATCH` → `PATCH`
    - POST + body `_method=GET` → `POST`
    - POST + body `_method=HEAD` → `POST`
    - POST + body `_method=OPTIONS` → `POST`
    - POST + query `?_method=DELETE` → `POST`
    - GET + query `?_method=DELETE` → `GET`
    - PUT + body `_method=DELETE` → `PUT`
    - POST + body `_method=` → `POST`
    - POST multipart with `_method=DELETE` → `POST` (D14)
  - `TestMutateMethodBodyCap`: a POST urlencoded body of 1 MB followed by `&_method=DELETE` → method stays `POST`, and the handler can still read a capped body without panic. A multipart POST of 100 MB → `r.MultipartForm` is nil after `MutateMethod` (not parsed).
  - `TestMutateMethodUseChainCSRF`: use the session/CSRF setup from `sessions_csrf_test.go`. Router: `r.Use(MutateMethod(), RequireSession(...), HandleCSRF(h))`, `r.Handle("/transfer", ok).Methods("POST")`. A POST with body `amount=1&_method=GET` and no token → status 401 and the handler is not called.
- [ ] Run `go test ./middleware -run 'TestMutateMethod' -count=1`. Expected: FAIL (GET row and chain test).
- [ ] **Implement (D14), as built after review:** only when `r.Method == http.MethodPost` and the media type is `application/x-www-form-urlencoded`. If `r.PostForm` is already set, use it. Otherwise read at most 64 KB+1 of the body into a buffer and restore `r.Body` (the bytes plus the rest of the original; Close reaches the original). Skip when `ContentLength` is over the cap. Parse with `url.ParseQuery` (an error means no override). On an override to PUT/PATCH/DELETE, set `r.PostForm` (net/http doesn't parse DELETE bodies) and then `r.Method`. Never call `ParseForm`, so downstream body semantics are unchanged. Doc comment must also say every route reachable through an override must be behind `HandleCSRF` (M8). Update the doc comment: the override is POST-only, from the body only, and limited to PUT/PATCH/DELETE. It must wrap the router (`MutateMethod()(router)`) to affect routing; under `r.Use` it changes only what handlers see.
- [ ] **Fuzz test** `FuzzMutateMethodCSRF(f)` takes inputs `(method string, body string, query string)`. Build the same `r.Use` chain as the chain test and send the request without a token. Properties:
  - (a) If the handler ran, the original method was GET, HEAD or OPTIONS.
  - (b) The method the handler saw is either the original or one of {PUT, PATCH, DELETE}.
  - Seed with every row from the table above.
- [ ] Run the tests and `go test ./middleware -run '^$' -fuzz '^FuzzMutateMethodCSRF$' -fuzztime 30s`. Expected: PASS.
- [ ] Commit `middleware: restrict MutateMethod to POST→PUT/PATCH/DELETE from body`.

### Task 1c: Filesystem Decrypt validates the key version (branch `sec/1c-fs-key-version`, from `sec/1b-mutate-method`)

**Files:** Modify `keys/filesystem.go:164-176`. Create `keys/fuzz_test.go`. Add a test to `keys/keys_test.go`.

- [ ] **Write the failing test** `TestFilesystemDecryptRejectsForeignVersion`:
  - Create encryption keys `a` and `b` with `CreateEncryptionKey` and `CreateKeyVersion`.
  - Encrypt `"secret"` with `b`, then rewrite the prefix to `../b/<ver>:`.
  - `Decrypt(ctx, aID, forged, nil)` must return an error, and the error must not contain `"failed to read"`.
  - Also check that prefixes `"abc:"`, `"/1:"` and `"1/2:"` all error.
  - `TestFilesystemIgnoresNonNumericVersions` (L1): drop a 32-byte file named `.DS_Store` and a version `9` next to a version `10` in the key dir. `Encrypt` uses `10` (numeric compare), and `SigningKeyVersions` skips `.DS_Store`.
- [ ] Run it. Expected: FAIL (the forged ciphertext decrypts).
- [ ] **Implement:** unexported `func parseVersion(name string) (uint64, bool)` wrapping `strconv.ParseUint(name, 10, 64)`. Decrypt rejects a prefix that fails it with `fmt.Errorf("invalid ciphertext: bad version")`. Encrypt's latest-version scan and `SigningKeyVersions` skip names that fail it and compare numerically.
- [ ] **Fuzz test** `FuzzFilesystemDecrypt(f)` takes inputs `(plaintext, aad []byte, flip uint16, prefix string)` and uses a single key created once per fuzz process via a `sync.Once` temp root. Properties:
  - Encrypt then Decrypt round-trips.
  - Flipping byte `flip % len(ct)` makes Decrypt error.
  - `Decrypt(prefix + ":" + rest)` with a non-digit prefix errors.
  - No call ever panics.
- [ ] Run the test and the fuzz target (30s). Expected: PASS.
- [ ] Commit `keys: reject non-numeric key versions in Filesystem.Decrypt`.

---

## Phase 2: Medium hardening

### Task 2a: Middleware options (branch `sec/2a-middleware-opts`, from `sec/1c-fs-key-version`)

**Files:**
- Modify `middleware/secure.go`, `middleware/csrf.go` and `middleware/template.go`.
- Create `middleware/options.go`.
- Tests go in `middleware/secure_test.go` (new), `middleware/sessions_csrf_test.go` and `middleware/simple_test.go`.

**Interfaces (Produces):**
- `type SecureOption func(*secure.Options)`
- `func WithTrustedHostHeaders(headers ...string) SecureOption`: sets `HostsProxyHeaders`.
- `func WithHSTSPreload() SecureOption`: sets `STSPreload=true`.
- `func WithSSLHost(host string) SecureOption`: sets `SSLHost` (D15).
- `func WithAllowedHosts(hosts ...string) SecureOption`: sets `AllowedHosts` (D15).
- `TemplateConfig.AllowedHosts []string` and `TemplateConfig.DevMode bool` (new fields, D16).
- `func SecureHeaders(devMode bool, serverType ServerType, opts ...SecureOption) mux.MiddlewareFunc`
- `type CSRFOption func(*csrfConfig)`, where `csrfConfig{maxBodyBytes int64}`.
- `const DefaultCSRFMaxBodyBytes int64 = 1 << 20` (D22)
- `func WithCSRFMaxBodyBytes(n int64) CSRFOption`
- `func HandleCSRF(h *render.Renderer, opts ...CSRFOption) mux.MiddlewareFunc`

Steps:
- [ ] **Failing tests:**
  - `TestSecureHeadersIgnoresForwardedHost`: with devMode false, `GET http://victim/login` with `X-Forwarded-Host: evil.example` → the Location host is `victim`.
  - `TestSecureHeadersTrustedHost`: with `WithTrustedHostHeaders("X-Forwarded-Host")` → the Location host is `evil.example`.
  - `TestSecureHeadersProxyProtoNoLoop`: `X-Forwarded-Proto: https` → no redirect, with and without `X-Forwarded-Host`.
  - `TestSecureHeadersHSTSPreload`: by default `Strict-Transport-Security` lacks `preload`; with `WithHSTSPreload()` it contains it. This needs an HTTPS-looking request (`X-Forwarded-Proto: https`).
  - `TestCSRFBodyCap`: a 2 MB multipart POST with a valid form token → 413 by default. With `WithCSRFMaxBodyBytes(8<<20)` → 200. A GET with a 2 MB body is not capped.
  - `TestCSRFTraceNotSafe`: TRACE without a token → 401.
  - `TestSecureHeadersAllowedHosts`: `WithAllowedHosts("victim")` + `Host: evil.example` → rejected (non-2xx, no Location to evil.example). `WithSSLHost("victim")` + `Host: evil.example` over HTTP → Location host `victim`.
  - `TestTemplateEndpointAllowedHosts` (D16), table over (DevMode, ServerEndpoint, AllowedHosts, Host) → serverEndpoint: (false, "", [a.com], a.com) → `https://a.com`; (false, "", [a.com], evil.com) → `https://a.com`; (true, "", nil, evil.com) → derived as today; (false, "https://x", any, any) → `https://x`.
  - `TestTemplateEndpointWarnsOnce`: with DevMode false, `ServerEndpoint: ""` and no AllowedHosts, two requests through a context logger from `logging.WithLogger(ctx, slog.New(slog.NewTextHandler(&buf, nil)))` → `buf` contains `"serverEndpoint derived from Host header"` exactly once, and serverEndpoint is empty.
- [ ] Run them. Expected: FAIL.
- [ ] **Implement:**
  - `secure.go`: drop `HostsProxyHeaders` and set `STSPreload: false` by default; apply opts after the defaults.
  - `csrf.go`: before `canSkipCSRFCheck`, for non-safe methods wrap `r.Body = http.MaxBytesReader(w, r.Body, cfg.maxBodyBytes)`. In `tokenFromRequest`, when the header token is absent, call `r.ParseMultipartForm(32<<20)` and map `*http.MaxBytesError` to `response` status 413 (`http.Error(w, http.StatusText(413), 413)`). Remove `MethodTrace` from `canSkipCSRFCheck`.
  - `template.go`: implement D16 in `serverEndpoint(r, cfg)`. When nothing is usable outside dev mode, return "" and log once via a per-middleware `sync.Once`: `logger.Warn("serverEndpoint derived from Host header; set TemplateConfig.ServerEndpoint in production")`.
  - `WithTrustedHostHeaders` doc comment: must be combined with `WithAllowedHosts`.
- [ ] **Fuzz tests** in `middleware/fuzz_test.go`:
  - `FuzzSecureHeadersRedirect(host, xfh, path string)` with `WithAllowedHosts("victim")`: the Location host, if any, is `victim`.
  - `FuzzCSRFToken(header, body string)`: `tokenFromRequest` never panics. An arbitrary value validates only if it equals mask(existing), and the round-trip `unmask(mask(x)) == x` holds.
- [ ] Run tests and both fuzz targets (30s each). Expected: PASS. Then run `go build ./...` in `examples/ssr-oidc`. Expected: compiles unchanged.
- [ ] Commit `middleware: distrust X-Forwarded-Host, host allowlists, opt-in HSTS preload, cap CSRF body, guard serverEndpoint`.

### Task 2b: Secrets and keys providers (branch `sec/2b-providers`, from `sec/2a-middleware-opts`)

**Files:** `secrets/vault/vault.go`, `secrets/aws/aws.go`, `secrets/azure/azure.go`, `keys/azure/azure.go`, `keys/vault/vault.go`, and a new `internal/azureauth/name.go`. Tests and `fuzz_test.go` go next to each file.

**Interfaces (Produces, all unexported unless noted):**
- `secrets/vault`: `func parseRef(name string) (path string, params map[string][]string, err error)`. `params` is nil when no version is given.
- `secrets/aws`: `func parseRef(name string) (id, version, stage string)`
- `internal/azureauth`: `func ValidateVaultName(name string) error`, which matches `^[A-Za-z0-9-]{3,24}$`.
- `keys/vault`: `func (s *Signer) signRequest(digest []byte) map[string]any`, which includes `"key_version": s.version`.

Steps:
- [ ] **Failing tests:**
  - `TestParseRefNoVersion`: `parseRef("secret/data/x")` → params nil.
  - `TestParseRefVersion`: `"secret/data/x?version=3"` → `{"version":{"3"}}`.
  - aws `TestParseRef`, table-driven: `"n@v#AWSPREVIOUS"` → `("n","v","AWSPREVIOUS")`; `"arn:aws:...:n"` → id only.
  - `TestValidateVaultName`: accepts `my-vault1`; rejects `evil.com#`, `a`, `a?b`, `""` and a 25-character name.
  - keys/azure `TestEncryptRejectsAAD`: `(&KeyVault{}).Encrypt(ctx, "v/k/1", p, []byte("x"))` errors with `"does not support additional authenticated data"`, without touching the client.
  - keys/azure `TestDecryptWarnsOnAAD` (D23): an unexported `warnAADOnce` hook (a `sync.Once` plus logger) fires once across two Decrypt calls with AAD; Decrypt with AAD does not return the AAD error.
  - keys/azure `TestSigningKeyVersionIDs` (L2): `SigningKeyVersions` returns IDs in `VAULT/KEY/VERSION` form, and `parseKeyID` accepts them.
  - keys/vault `TestSignRequestKeyVersion`: `(&Signer{name:"k", version:2}).signRequest(d)["key_version"] == 2`.
- [ ] Run them. Expected: FAIL or compile errors.
- [ ] **Implement:** wire each extracted function into its existing call site. Replace all five `fmt.Sprintf("https://%s.vault.azure.net", …)` sites (secrets/azure:77, keys/azure:78,117,149,170) with one helper `azureauth.VaultURL(name string) (string, error)` that calls `ValidateVaultName`. Update the Azure Encrypt/Decrypt doc comments to cite API 7.0 + RSA-OAEP-256 having no AAD.
- [ ] **Fuzz tests** for `parseRef` (vault and aws: no panic, stable parse) and `ValidateVaultName` (if it returns nil, then `url.Parse("https://"+n+".vault.azure.net").Host == n+".vault.azure.net"`).
- [ ] Run the gate. Commit `secrets,keys: fetch latest Vault version, Azure AAD handling, validate vault names, pin Vault sign version`.

### Task 2c: Cookie defaults, session max age and server timeouts (branch `sec/2c-cookies-server`, from `sec/2b-providers`)

**Files:** `cookiestore/cookiestore.go:33-51`, a new `cookiestore/options.go`, `session/session.go`, `middleware/sessions.go`, `server/server.go:77-80`, and tests in `cookiestore/cookiestore_test.go`, `session/session_test.go`, `middleware/sessions_csrf_test.go` and `server/server_test.go`.

**Interfaces (Produces):**
- `type Option func(*config)`, `func WithInsecureCookies() Option`, `func New(fn EntropyFunc, opts *sessions.Options, o ...Option) sessions.Store` (D21).
- `session.StoreCreatedAt(*sessions.Session, time.Time)`, `session.CreatedAt(*sessions.Session) time.Time`.
- `middleware.CheckSessionMaxAge(maxAge time.Duration, onExpired http.HandlerFunc) mux.MiddlewareFunc` (D17). `RequireSession` stamps created-at on new sessions.
- In `server`, the unexported `func newHTTPServer(h http.Handler) *http.Server`, used by `ServeHTTPHandler`.

- [ ] **Failing tests:**
  - `TestNewCookieDefaults` saves a session through `httptest`, parses `Set-Cookie`, and checks:
    - (nil opts) → HttpOnly, Secure, SameSite=Lax.
    - `&sessions.Options{Path:"/"}` → HttpOnly, Secure, Lax.
    - `{HttpOnly:false, Secure:false}` → still HttpOnly and Secure, and the caller's struct still has both false (copy, D18).
    - `{SameSite: http.SameSiteDefaultMode}` → Lax.
    - `{SameSite: http.SameSiteNoneMode}` → None and Secure.
    - `WithInsecureCookies()` → not Secure.
    - `WithInsecureCookies()` + `SameSite=None` → `Save` returns an error containing `"SameSite=None requires Secure"`.
  - `TestCheckSessionMaxAge`: created-at 2h ago with maxAge 1h → `onExpired` runs and the session is cleared; created-at 10m ago → next runs; a session missing created-at is treated as created now and stamped.
  - `TestNewHTTPServerTimeouts`: `ReadHeaderTimeout==10s`, `ReadTimeout==5*time.Minute`, `IdleTimeout==120*time.Second`, `WriteTimeout==0`.
- [ ] Run them. Expected: FAIL.
- [ ] **Implement:** in `New`, copy `*opts` into a new struct, then set `HttpOnly = true`, `Secure = !cfg.insecure`, and SameSite Lax when it is 0 or `SameSiteDefaultMode`. The `SameSite=None` without `Secure` case errors from `Save` (the gorilla `Store` interface gives `New` no error return). Add `StoreCreatedAt`/`CreatedAt` and `CheckSessionMaxAge`. Extract `newHTTPServer`.
- [ ] **Fuzz test** `FuzzHotCodecDecode(name, value string)` with a static 64-byte entropy func:
  - No panic.
  - `Encode(n, v)` then `Decode(n, …)` round-trips.
  - Decode with any other name, or with a single byte flipped, errors.
- [ ] Run `make test-examples`. Expected: the example still passes; it sets `Secure` itself, and dev mode must add `WithInsecureCookies()` in this commit.
- [ ] Gate. Commit `cookiestore,session,server: secure cookie defaults, absolute session age, connection timeouts`.

### Task 2d: Cache per-key coalescing, TTL bound and purge (branch `sec/2d-cache`, from `sec/2c-cookies-server`)

**Files:** `cache/cache.go`, `cache/cache_test.go`, `cache/fuzz_test.go`.

**Interfaces:**
- `const MaxExpireAfter = 100 * 365 * 24 * time.Hour` (exported, so the error is explainable).
- An unexported in-flight map, `inflight map[string]*call[T]`, where `call[T]{wg sync.WaitGroup; val T; err error}`.

- [ ] **Failing tests:**
  - `TestWriteThruLookupDoesNotBlockOtherKeys`: the lookup for `"a"` blocks on a channel; `Lookup("b")` and `Set("b")` finish within 100 ms.
  - `TestWriteThruLookupCoalesces`: 50 goroutines on `"a"` → the lookup runs once and every caller gets the same value.
  - `TestWriteThruLookupErrorNotCached`: concurrent callers all get the error, `Size()==0`, and the next call runs the lookup again.
  - `TestWriteThruLookupPanicReleasesWaiters` (L9): the lookup panics; the owner's panic propagates, waiters return a non-nil error within 100 ms, and the next call runs the lookup again.
  - `TestNewRejectsHugeTTL`: `New[int](math.MaxInt64)` → `ErrInvalidDuration`.
  - `TestExpiredHitPurgesOnce`: 100 lookups of one expired key start at most one purge. Check this with an unexported purge counter hook.
- [ ] Run them. Expected: FAIL.
- [ ] **Implement:**
  - Run `primaryLookup` outside `c.mu`. Under the lock, check the cache and the in-flight map. A waiter on an existing call does `wg.Wait()`. The owner stores the result only when `err == nil`.
  - `New`: reject `expireAfter > MaxExpireAfter`.
  - Track `purging map[string]struct{}` so only one purge per key is scheduled.
  - The owner removes the in-flight entry and calls `wg.Done()` in a `defer`, with a recovered panic turned into `errLookupPanicked` for waiters before re-panicking.
  - Fix the `WriteThruLookup` doc comment to say coalescing is per key.
- [ ] **Fuzz tests:**
  - `FuzzCacheOps(ops []byte)`: decode it into Set/Lookup/WriteThruLookup/Clear over 8 keys and check against a `map[string]int` model with a 1h TTL.
  - `FuzzCacheTTL(d int64)`: `New` errors, or a Set followed by a Lookup hits when `d > int64(time.Second)`.
- [ ] Run the tests with `-race`, plus the gate. Commit `cache: per-key write-through coalescing, bound TTL, single purge`.

---

## Phase 3: Low fixes

### Task 3a: Render, response and flash (branch `sec/3a-render-response`, from `sec/2d-cache`)

**Files:** `render/csv.go:70`, `render/json.go` (`escapeJSON`), `render/funcs.go:47`, `response/response.go:42-70`, `flash/flash.go:110-135`, plus the matching tests and `fuzz_test.go` per package.

**Interfaces (Produces):** in render, the unexported `func toJSON(v any) (template.JS, error)`, which replaces `json.Marshal` in the FuncMap.

- [ ] **Failing tests:**
  - `TestRenderCSVFilenameQuoted`: filename `x.csv; filename*=UTF-8''evil.html` → `mime.ParseMediaType(header)` gives params `{"filename": that exact string}` and no `filename*`.
  - `TestRenderJSONErrorValid`: dev-mode error text `a\b"c\n` → `json.Valid(body)`.
  - `TestToJSONInScript`: the template `<script>var x = {{toJSON .}};</script>` with `{"a":"</script><b>"}` → the output contains no `</script><b>`, and the extracted JS literal parses as JSON equal to the input.
  - `TestTextToJSONIsString` (L5): the text-template FuncMap (`render/funcs.go:85`) `toJSON` returns `string`.
  - `TestBackRejectsNonHTTPReferer`: Referer `javascript://host/x` on the same host → Location `/`.
  - `TestFlashNonMapValue`: `New(map[any]any{flashKeyError: "str"})` → `Errors()` returns empty with no panic, and `Error("x")` then works.
- [ ] Run them. Expected: FAIL.
- [ ] **Implement:**
  - CSV: `mime.FormatMediaType("attachment", map[string]string{"filename": filename})`.
  - `escapeJSON`: `json.Marshal(s)`, stripping the surrounding quotes to fit the existing Printf templates.
  - `toJSON`: `json.Marshal` (which already escapes `<`, `>` and `&`) then `template.JS(b)`.
  - `Back`: require `u.Scheme` to be `http` or `https`.
  - `flash`: comma-ok type assertion in `add` and `get`; reset on mismatch.
- [ ] **Fuzz tests:**
  - `FuzzRenderJSON(msg string)`: `json.Valid`.
  - `FuzzRenderCSVFilename(name string)`: when `name` is valid UTF-8 without control characters, the header round-trips.
  - `FuzzToJSON(s string)`: the output parses as JSON and contains no `</script`, `<!--`, U+2028 or U+2029.
  - `FuzzBack(referer, host string)`: Location is `/` or a same-host http(s) URL, never `//` or `/\`.
  - `FuzzFlashSession(kind uint8, s string)`: no panic for any value type stored under the flash keys.
- [ ] Gate. Commit `render,response,flash: escape CSV filename and JSON, fix toJSON, harden Back and flash`.

### Task 3b: Example app (branch `sec/3b-example`, from `sec/3a-render-response`)

**Files:** `examples/ssr-oidc/main.go:261`, `examples/ssr-oidc/auth.go:183`, the logout link in the templates, `examples/ssr-oidc/e2e_test.go`, and a new `examples/ssr-oidc/fuzz_test.go`.

- [ ] **Failing tests:**
  - `TestLogoutRequiresPOST`: GET `/logout` → 405, and the session survives. POST with the CSRF token → logged out.
  - `TestCallbackFailureClearsFlowState` (#24): a callback with the wrong state → 400, and the next request's session has no state, nonce or verifier.
  - `TestAdminRequiresVerifiedEmail` (#25): a fake IdP token with an admin email and `email_verified:false` → not admin.
- [ ] **Implement:**
  - Route `/logout` with `.Methods("POST")`.
  - The template logout becomes a `<form method="post">` containing `{{.csrfField}}`.
  - The state compare becomes `subtle.ConstantTimeCompare`.
  - Delete the flow keys before any response is written, not in a `defer`.
  - Require `email_verified == true` in the admin check.
  - Set `ServerEndpoint` and `AllowedHosts` in the example's `TemplateConfig`.
- [ ] **Fuzz test** `FuzzCallbackState(state, code string)`, built on the `e2e_test.go` scaffolding (fake IdP, fixed session state):
  - Any state that doesn't match → 400.
  - The fake token endpoint hit count stays 0.
  - The session has no flow keys afterwards.
- [ ] `make test-examples`. Commit `examples/ssr-oidc: POST logout, clear flow state early, verified-email admin, constant-time state`.

---

## Phase 4: Fuzz suite and CI

### Task 4a: `make fuzz` and the nightly workflow (branch `sec/4a-fuzz-ci`, from `sec/3b-example`)

**Files:** `Makefile`, a new `.github/workflows/fuzz.yml`.

- [ ] **Makefile** `fuzz` target, with `FUZZTIME ?= 30s`. For each package in `go list ./...` plus `./examples/ssr-oidc`, for each name in `go test -list '^Fuzz' $pkg | grep '^Fuzz'`, run `go test -run '^$' -fuzz "^$name$" -fuzztime $(FUZZTIME) $pkg || exit 1`. Add `.PHONY: fuzz`.
- [ ] **`fuzz.yml`:**
  - Triggers: `schedule: cron '17 6 * * *'` and `workflow_dispatch`.
  - Steps: Go 1.27.4, `GOTOOLCHAIN: local`, then `make fuzz FUZZTIME=60s`. No artifact upload (D19).
  - Permissions: `contents: read`. Actions pinned by full commit SHA with a version comment.
- [ ] Run `make fuzz FUZZTIME=5s` (expected: every target from Phases 1–3 runs, exit 0) and `actionlint` (expected: clean).
- [ ] Commit `ci: add make fuzz and nightly fuzz workflow`.

### Task 4b: Fuzz targets for util, secrets and keys (branch `sec/4b-fuzz-secrets`, from `sec/4a-fuzz-ci`)

- [ ] `internal/util/fuzz_test.go`:
  - `FuzzSafeJoin(name)`, root `t.TempDir()`: on success, `filepath.Rel(root, out)` doesn't start with `..` and isn't absolute.
  - `FuzzBase64Decode(b []byte)`: decoding the Std, RawStd, URL and RawURL encodings of `b` all returns `b`.
  - `FuzzTrimSpace(s)`: the function is idempotent, and its result has no leading or trailing `unicode.IsSpace`.
- [ ] `secrets/fuzz_test.go`:
  - `FuzzFilesystemSecretName(name)`: never reads a sentinel file placed outside the root.
  - `FuzzResolver(value)` with `NewInMemoryFromMap` and a temp SecretsDir: values without `secret://` pass through unchanged, and `target=file` creates files only under SecretsDir.
  - `FuzzJSONExpander(json, path string)`: no panic; the result is a value or an error.
- [ ] `keys/fuzz_test.go`: `FuzzParseECDSAPublicKey(b)`: no panic; non-nil results are P-256.
- [ ] `keys/azure/fuzz_test.go`:
  - `FuzzConvert1363ToASN1(b)`: no panic, and successful output passes `asn1.Unmarshal` into `struct{R, S *big.Int}`.
  - `FuzzParseP256PublicKey(x, y)`: no panic; off-curve points error.
- [ ] `make test`, `make fuzz FUZZTIME=10s`. Commit `test: fuzz util, secrets and keys parsers`.

### Task 4c: Fuzz targets for middleware, form and i18n (branch `sec/4c-fuzz-web`, from `sec/4b-fuzz-secrets`)

- [ ] `middleware/fuzz_test.go` additions:
  - `FuzzTraceID(header)`: the stored ID matches `^[A-Za-z0-9_-]{1,128}$` or is generated.
  - `FuzzProcessLocale(lang, acceptLang)` with a stub provider: `textDirection` ∈ {ltr, rtl}.
  - `FuzzRequireHostHeader(host)` with allow `["example.com"]` and stripPort true: 200 only when the host minus port equals example.com, compared case-insensitively.
  - `FuzzStaticPath(path)`: never 200 for paths ending in `/`.
  - `FuzzCSP(policy)`: with no nonce set, the output contains no `'nonce-'`.
- [ ] `form/fuzz_test.go` `FuzzBind(contentType string, body []byte)`. The destination struct has int, `[]string`, nested-struct, `*int` and `time.Time` fields. Properties:
  - No panic.
  - `len(body) > MaxBytes` → error.
  - A returned error never comes with non-nil field errors.
- [ ] `i18n/fuzz_test.go` `FuzzLocaleLookup(lang, accept)` over an `fstest.MapFS` holding `en` and `es` locales: the returned language is the default or a loaded key.
- [ ] `response/fuzz_test.go` `FuzzRealHost(host, uri string)`: no panic, and `url.Parse` of the result succeeds.
- [ ] Run the gate and `make fuzz FUZZTIME=10s`. Commit `test: fuzz middleware, form, i18n and response`.

### Task 4d: Fuzz targets for logging and render HTML (branch `sec/4d-fuzz-misc`, from `sec/4c-fuzz-web`)

- [ ] `logging/fuzz_test.go` `FuzzLevelFromString(s)`: the result is one of Debug, Info, Warn or Error.
- [ ] `render/fuzz_test.go` `FuzzRenderHTMLEscaping(s)`: render a test template `<p>{{.}}</p>` with `s`. The body between `<p>` and `</p>` contains no raw `<`.
- [ ] Also fuzz `toStringSlice`, `joinStrings` and `toSentence` (no panic).
- [ ] Run the gate and a full `make fuzz FUZZTIME=30s`. Expected: zero crashers. Any crasher found is a finding: fix it with its input saved under `testdata/fuzz`. Commit `test: fuzz logging and render helpers`.

---

## Phase 5: Docs, skill and release prep

### Task 5a: Docs and existing skills (branch `sec/5a-docs`, from `sec/4d-fuzz-misc`)

**Files:**
- `SECURITY.md`: supported versions become "latest v1.x", the pre-1.0 wording is removed, and a Go ≥ 1.27.4 note is added.
- Docs pages:
  - `docs/content/en/docs/middleware-chain.md`: `MutateMethod` wraps the router; the `SecureHeaders` options; `X-Forwarded-Host` opt-in; HSTS preload.
  - `sessions-and-csrf.md`: the CSRF body cap and option; `ClearCSRFToken` on login; cookie defaults and `Secure`.
  - `server.md`: the timeouts and the custom `http.Server` escape hatch.
  - `secrets-and-keys.md`: Vault latest version; Azure AAD error.
  - `rendering-templates.md`: `toJSON`; `serverEndpoint` and the `RenderJSON` error exposure.
  - `cache.md`: `MaxExpireAfter`.
- Skills:
  - `skills/go-bananas-scaffold/SKILL.md` and `skills/go-bananas-secrets/SKILL.md`: the `cookiestore.New` examples set `Secure: !devMode`.
  - `skills/go-bananas-middleware/SKILL.md`: the `MutateMethod` placement; state-changing routes must set `.Methods(...)` and be behind `HandleCSRF` (M8, #26).
- `sessions-and-csrf.md` also covers: `CheckSessionMaxAge`, the fact that cookie sessions can't be revoked except by rotating keys, and that session writes after the first response byte are lost.
- `rendering-templates.md` also covers the `safeHTML` warning.

- [ ] Make the edits.
- [ ] `cd docs && npm ci && npx hugo --minify`. Expected: builds with no errors.
- [ ] `make tabcheck`.
- [ ] Run `go run golang.org/x/exp/cmd/apidiff` between `v1.0.1` and HEAD for each package (L7). Expected: only the documented variadic-signature changes and additions. Save the output for the release notes.
- [ ] Commit `docs: document v1.1 security defaults and options`.

### Task 5b: The `go-bananas-upgrade` skill and release drafts (branch `sec/5b-upgrade-skill`, from `sec/5a-docs`)

**Files:** a new `skills/go-bananas-upgrade/SKILL.md`. Release and advisory drafts go in the scratchpad, not the repo.

- [ ] **Write the skill:**
  - Frontmatter: `name: go-bananas-upgrade`. The description triggers on "upgrade go-bananas", "migrate to v1.1" and "go-bananas breaking changes".
  - Step 1: detect the version with `go list -m github.com/mikehelmick/go-bananas`.
  - Step 2: a "v1.0.x → v1.1.0" section with one entry per behavior change: #1, #3/D15, #4/D16, #5, D21 (`Secure` and `WithInsecureCookies` for dev), D4 (Lax vs `form_post`/SAML), #8, D22 (uploads over 1 MB, multipart forms and `FormFile`), D23 (Azure AAD), #18, #26 (routes without `.Methods`), plus the variadic signature note. Each entry has a `grep -rn` pattern, a "you are affected if" line, and the fix (the exact option or code).
  - Step 3: bump with `go get github.com/mikehelmick/go-bananas@v1.1.0`, then build, test, and run `govulncheck`.
  - Step 4: report to the user what changed and what still needs a human decision (for example, whether to opt in to `WithTrustedHostHeaders`).
- [ ] **Validate:**
  - Copy `examples/ssr-oidc` from tag v1.0.1 (`git archive v1.0.1 examples/ssr-oidc`) into the scratchpad.
  - Dispatch a fresh subagent with only the skill text against that copy.
  - Expected: it flags the logout route, the `cookiestore.New` options, and `SecureHeaders` behind a proxy, and recommends `Secure`.
- [ ] **Draft** in the scratchpad:
  - `release-notes-v1.1.0.md`: summary, security fixes, a "Migration from v1.0.x" section linking the skill and docs, and the Go 1.27.4 recommendation.
  - `advisory-csrf-mutatemethod.md` and `advisory-keys-filesystem-version.md`: affected `< 1.0.2`, patched `1.0.2` and `1.1.0`, CWE-352 / CWE-22, description, workarounds.
  - `vulndb-mutatemethod.md` and `vulndb-filesystem-decrypt.md`: Go vuln DB report fields (module, package, symbols `MutateMethod` / `Filesystem.Decrypt`, versions, GHSA link).
- [ ] Commit `skills: add go-bananas-upgrade migration skill`.

---

## Phase 6: v1.0.2 backport (D20)

### Task 6: Cherry-pick #1 and #2 onto v1.0.1 (branch `release/v1.0.2`, from tag `v1.0.1`)

- [ ] `git checkout -b release/v1.0.2 v1.0.1`
- [ ] `git cherry-pick` the Task 1b and Task 1c commits. Resolve conflicts by keeping v1.0.1's surrounding code; carry only the fixes and their tests. `fuzz_test.go` files come along with their seeds.
- [ ] Bump CI `go-version` to `'1.27.4'` on this branch as well.
- [ ] Gate on this branch: `make test test-examples lint` + govulncheck. Expected: green; `git diff v1.0.1 --stat` touches only `middleware/method*`, `middleware/fuzz_test.go`, `keys/filesystem.go`, `keys/*_test.go`, `testdata/fuzz/**` and `ci.yml`.

## Finish (owner-gated)

- [ ] Goal-level code review and security review across the whole stack (`design-process:build`).
- [ ] Present the stack, `release/v1.0.2`, the release-notes draft, the advisory drafts and the vuln DB drafts to the owner. **Push, open PRs, tag v1.0.2 and v1.1.0, and publish advisories only on explicit approval** (D11, D12, D20; Rollout: do it in one sitting).
