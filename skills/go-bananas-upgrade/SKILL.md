---
name: go-bananas-upgrade
description: Use when upgrading an app that depends on go-bananas (github.com/mikehelmick/go-bananas) to a newer release — finding code affected by changed defaults, applying the fixes, and verifying the bump. Organized per version, starting with v1.0.x → v1.1.0. Triggers on "upgrade go-bananas", "migrate to v1.1", "go-bananas breaking changes", "bump go-bananas", "what changed in go-bananas v1.1".
---

# Upgrade a go-bananas app

Work in four steps: detect the version, scan for affected code, bump and verify,
then report. **Scan before you bump.** Several v1.1.0 changes compile cleanly and
only change runtime behavior, so the build alone won't catch them.

Run every `grep` from the app's module root. Inspect each hit in context
(`-A`/`-B`) before you decide; a hit is a lead, not a verdict. Reference docs:
<https://mikehelmick.github.io/go-bananas/docs/>.

## Step 1: detect the current version

```sh
go list -m github.com/mikehelmick/go-bananas
go version
```

- `v1.0.x` (or older): apply the **v1.0.x → v1.1.0** section.
- A `=> ../path` replace directive (common in monorepos and in the repo's own
  examples) means the version comes from that local checkout, not a tag. Ask the
  user which release it corresponds to; if unknown, assume v1.0.x and scan.
- Already `v1.1.0` or later: you can still run the scan to find v1.0-era
  patterns (for example `Secure: !devMode`) that now silently do nothing.

## Step 2: v1.0.x → v1.1.0

v1.1.0 is a security release. It changes defaults to safe values and adds
opt-ins. No persisted formats change, so a downgrade stays possible. Each entry
below gives **Grep**, **Affected if** and **Fix**. Entries marked
**[decision]** need a human choice; don't pick silently.

### Routing and CSRF

**1. `MutateMethod` is POST → PUT/PATCH/DELETE only, from a urlencoded body.**

- Grep: `grep -rn --include='*.go' 'MutateMethod' .` and
  `grep -rn '_method' --include='*.go' --include='*.html' --include='*.tmpl' --include='*.js' .`
- Affected if `_method` is sent in a query string, in a multipart form, in a
  urlencoded body over 64 KB, from a non-POST request, or targets anything other
  than PUT/PATCH/DELETE (for example GET). These are now **silently ignored**
  and the request stays POST. Also affected if it is installed with `r.Use` and
  you expect the override to change which route matches; under `r.Use` it only
  changes what later handlers see.
- Fix: send `_method` as a hidden field of a urlencoded `method="post"` form,
  wrap the router, and keep every overridable route behind `HandleCSRF`:

  ```go
  handler := middleware.MutateMethod()(r) // not r.Use(middleware.MutateMethod())
  return srv.ServeHTTPHandler(ctx, handler)
  ```

**2. State-changing routes must be POST (or PUT/PATCH/DELETE) with an explicit `.Methods`.**

`HandleCSRF` never checks GET/HEAD/OPTIONS. TRACE now **requires** a token.

- Grep (routes with no method restriction):
  `grep -rnE --include='*.go' '\.(HandleFunc|Handle|Handler)\(' . | grep -v '\.Methods('`
  A route whose `.Methods(` sits on a later line is a false positive; check with
  `-A3`. `PathPrefix(...).Handler(...)` hits (static files) are fine if the
  handler is read-only.
- Grep (state changes on GET, such as sign-out or delete links):
  `grep -rniE --include='*.go' '"/(logout|signout|sign-out|delete|remove|unsubscribe)[^"]*"' .`
  and `grep -rniE 'href="[^"]*(logout|signout|sign-out|delete|remove)' --include='*.html' --include='*.tmpl' .`
- Affected if a handler that changes state (logout included) is routed as
  `.Methods(http.MethodGet)`, or has no `.Methods` at all (HEAD and OPTIONS then
  reach it without a token). Also affected if any client sends TRACE.
- Fix: restrict the route and post a form carrying the CSRF field:

  ```go
  r.HandleFunc("/logout", a.handleLogout).Methods(http.MethodPost)
  ```
  ```html
  <form method="post" action="/logout">{{.csrfField}}<button type="submit">Sign out</button></form>
  ```

  Docs: [Routes must set `.Methods`](https://mikehelmick.github.io/go-bananas/docs/middleware-chain/#routes-must-set-methods).

**3. `HandleCSRF` caps form bodies at 1 MiB.**

- Grep: `grep -rn --include='*.go' -E 'FormFile|ParseMultipartForm|MultipartReader|HandleCSRF\(' .`
  and `grep -rn 'multipart/form-data' --include='*.html' --include='*.tmpl' .`
- Affected if any non-safe request **without** an `X-CSRF-Token` header (a
  normal HTML form, including uploads and large textareas) can exceed 1 MiB. It
  now gets **413**, and the limit carries into the handler (`form.Bind`,
  `r.FormFile`). Header-token JSON and streaming requests are not capped.
- Fix: give upload routes their own `HandleCSRF` on a subrouter. `HandleCSRF`
  must then come **off the root router**, since subrouters also run parent
  middleware:

  ```go
  upload := r.PathPrefix("/upload").Subrouter() // register first
  upload.Use(middleware.HandleCSRF(h, middleware.WithCSRFMaxBodyBytes(64<<20)))
  upload.HandleFunc("", handleUpload).Methods(http.MethodPost)

  app := r.NewRoute().Subrouter()
  app.Use(middleware.HandleCSRF(h)) // 1 MiB default
  app.Use(middleware.PopulateTemplateVariables(tmplCfg)) // middleware after CSRF moves too
  ```

  **[decision]** the upload limit. Docs: [Request body limit](https://mikehelmick.github.io/go-bananas/docs/sessions-and-csrf/#request-body-limit).

### Proxies and hosts

**4. `SecureHeaders` no longer trusts `X-Forwarded-Host`, and HSTS preload is opt-in.**

- Grep: `grep -rn --include='*.go' 'SecureHeaders(' .`, plus deploy config:
  `grep -rniIE 'x-forwarded-host|proxy_set_header +host|preservehost|hstspreload|ingress|load.?balancer' --exclude-dir=vendor --exclude-dir=node_modules .`
- Affected if the app runs behind a proxy, load balancer or CDN that puts the
  public host in `X-Forwarded-Host` and sends an internal `Host`. The HTTP→HTTPS
  301 now goes to the request's `Host`. (`X-Forwarded-Proto: https` is still
  trusted, so there is no redirect loop.) Also affected if the domain is on the
  HSTS preload list, because `preload` is no longer sent.
- Fix, **[decision]** pick one:

  ```go
  // Preferred: pin the redirect host.
  middleware.SecureHeaders(devMode, middleware.ServerTypeHTML,
      middleware.WithSSLHost("www.example.com"))

  // Or trust the header, but only with an allowlist (other hosts get 400).
  middleware.SecureHeaders(devMode, middleware.ServerTypeHTML,
      middleware.WithTrustedHostHeaders("X-Forwarded-Host"),
      middleware.WithAllowedHosts("www.example.com"))

  // Preloaded domains only:
  middleware.SecureHeaders(devMode, middleware.ServerTypeHTML, middleware.WithHSTSPreload())
  ```

  With `WithAllowedHosts`, health checks or internal callers that use an IP or
  internal hostname also get 400, so list them or keep probes off this chain.
  Never use `WithTrustedHostHeaders` without `WithAllowedHosts` or `WithSSLHost`.
  If no deploy config is in the repo, **ask the user** whether the app runs
  behind a proxy and whether the domain is preloaded; don't assume either way.
  Docs: [SecureHeaders options](https://mikehelmick.github.io/go-bananas/docs/middleware-chain/#secureheaders-options).

**5. The template `serverEndpoint` is empty in production unless configured.**

- Grep: `grep -rn --include='*.go' -A6 'TemplateConfig{' .` and
  `grep -rn 'serverEndpoint' --include='*.html' --include='*.tmpl' --include='*.txt' .`
- Affected if `TemplateConfig` sets neither `ServerEndpoint` nor `AllowedHosts`.
  Outside dev mode `serverEndpoint` is now `""` (it used to come from the
  attacker-controlled `Host` header), and a warning is logged once. Absolute
  links, canonical/OG tags and email links built from it break. Flag this even
  if no template uses it yet.
- Fix:

  ```go
  middleware.PopulateTemplateVariables(middleware.TemplateConfig{
      ServerName:     "myapp",
      DevMode:        devMode,
      ServerEndpoint: cfg.ServerEndpoint, // e.g. "https://www.example.com"
      // or AllowedHosts: []string{"www.example.com"}, // bare hosts; result is always https://
  })
  ```

  Docs: [`serverEndpoint`](https://mikehelmick.github.io/go-bananas/docs/middleware-chain/#template-variables-and-serverendpoint).

**6. `response.Back` only follows same-host http(s) referrers.**

- Grep: `grep -rn --include='*.go' 'response.Back(' .`
- Affected if you rely on redirecting back to another host or subdomain, or a
  proxy rewrites `Host` so the public `Referer` host never equals `r.Host`
  (every `Back` then goes to `/`). Userinfo is stripped.
- Fix: preserve `Host` at the proxy, or write an explicit redirect.

**7. `RealHostFromRequest` keeps IPv6 brackets.**

- Grep: `grep -rn --include='*.go' 'RealHostFromRequest' .`
- Affected if tests or code compare its output for IPv6 hosts. It now returns
  `https://[::1]:8443`, not `https://::1:8443`. Other hosts are unchanged.
- Fix: update expectations.

### Cookies and sessions

**8. `cookiestore.New` forces `HttpOnly` and `Secure`, and defaults `SameSite` to Lax.**

- Grep: `grep -rn --include='*.go' -A8 'cookiestore.New(' .`
- Affected if you use `cookiestore` at all. In particular:
  - `Secure: !devMode` (the v1.0 pattern) is now **ignored**: cookies are always
    Secure. Local HTTP dev breaks on Safari and on any host other than
    `localhost`/`127.0.0.1` (LAN IPs, Docker hostnames).
  - JavaScript can no longer read the session cookie.
- Fix: drop `HttpOnly`/`Secure`/`SameSite` (they are overridden) and opt out
  only in dev:

  ```go
  var copts []cookiestore.Option
  if devMode {
      copts = append(copts, cookiestore.WithInsecureCookies())
  }
  store := cookiestore.New(entropy, &sessions.Options{Path: "/", MaxAge: 3600}, copts...)
  ```

  The defaults apply only at `New`. Later edits to `session.Options` are not
  checked. Also fix any log line or doc that promises "insecure cookies in dev":
  it stays true only once `WithInsecureCookies()` is passed. Docs: [Cookie defaults](https://mikehelmick.github.io/go-bananas/docs/sessions-and-csrf/#cookie-defaults).

**9. SameSite=Lax breaks cross-site POST callbacks, and SameSite=None forces Secure.**

- Grep: `grep -rniE --include='*.go' 'form_post|response_mode|saml|SameSite' .`
- Affected if an identity provider POSTs back to you cross-site (OIDC
  `response_mode=form_post`, SAML HTTP-POST binding): the session cookie isn't
  sent under Lax, so state checks fail. Also affected if you set
  `SameSiteNoneMode`: it now forces `Secure`, **even with
  `WithInsecureCookies()`**, so it only works over HTTPS (including local dev).
- Fix, **[decision]** (None is weaker defense in depth; `HandleCSRF` still applies):

  ```go
  store := cookiestore.New(entropy, &sessions.Options{Path: "/", SameSite: http.SameSiteNoneMode})
  ```

**10. Split-value cookies are bound to their session.**

- Grep: `grep -rn --include='*.go' 'RequireSession(' .`. Affected only if the
  second argument is non-nil.
- Affected if you use split values: `_gbs-<key>` cookies from v1.0.x are
  **dropped once at upgrade**, so users lose that value (for example a stored ID
  token, which may force a re-login). During a **rolling deploy**, a visitor who
  moves between v1.0.x and v1.1 instances loses it each time. Only one split key
  per router is supported. Deleting the session (`Options.MaxAge = -1`) now also
  expires its split cookies.
- Fix: make handlers tolerate a missing split value, and **[decision]** finish
  the rollout quickly (or drain old instances) before relying on split values.
  Docs: [Split values](https://mikehelmick.github.io/go-bananas/docs/sessions-and-csrf/#split-values).

**11. `CheckSessionMaxAge` is new and optional. Recommend it.**

- Grep: `grep -rn --include='*.go' 'CheckSessionMaxAge' .`. Only this name
  counts; `CheckSessionIdleNoAuth` is an idle timeout, not an absolute lifetime.
- Affected if it's absent: nothing breaks, but a stolen session cookie stays
  valid forever (cookie sessions can't be revoked except by rotating keys).
- Fix: install it after `RequireSession` and before `HandleCSRF`. It clears the
  session itself; `onExpired` should redirect, not render. Reset the session (or
  call `session.StoreCreatedAt(s, time.Now())`) at login so age counts from
  sign-in. v1.0.x cookies are accepted and start their clock at the first v1.1
  request.

  ```go
  r.Use(middleware.RequireSession(store, nil, h))
  r.Use(middleware.CheckSessionMaxAge(12*time.Hour, func(w http.ResponseWriter, r *http.Request) {
      response.SeeOther(w, r, "/")
  }))
  r.Use(middleware.HandleCSRF(h))
  ```

  **[decision]** the max age. Docs: [Absolute session lifetime](https://mikehelmick.github.io/go-bananas/docs/sessions-and-csrf/#absolute-session-lifetime).

### Server

**12. `ServeHTTPHandler` adds `ReadTimeout` 5m and `IdleTimeout` 120s.**

- Grep: `grep -rn --include='*.go' -E 'ServeHTTPHandler\(|ServeHTTP\(ctx' .`
- Affected if a request body can take longer than 5 minutes to arrive (large
  uploads on slow links, streamed request bodies). Responses are not limited
  (no `WriteTimeout`), so SSE and streaming downloads keep working.
- Fix: for other limits, pass your own server:

  ```go
  return srv.ServeHTTP(ctx, &http.Server{
      Handler:           handler,
      ReadHeaderTimeout: 10 * time.Second,
      ReadTimeout:       30 * time.Minute,
      IdleTimeout:       120 * time.Second,
  })
  ```

### Secrets and keys

**13. Vault secrets read the latest version by default.**

- Grep: `grep -rniE 'secrets/vault|keys/vault|SECRET_MANAGER|KEY_MANAGER|version=' --include='*.go' --include='*.yaml' --include='*.yml' --include='*.env' .`
- Affected if a Vault reference has no `?version=N`: it read version 1 before,
  and now reads the **latest**. References with a URL scheme, host or fragment
  are rejected. For Vault signing: `Sign` is pinned to the signer's own key
  version, `SigningKeyVersions` is now **newest first** (was oldest first), and
  key IDs with a non-numeric version (`k@`, `k@abc`) are rejected.
- Fix: pin `?version=1` only if you really depended on v1. Fix any code that
  took `SigningKeyVersions()[0]` as the oldest.

**14. Azure Key Vault: AAD is rejected, and key IDs changed.**

- Grep: `grep -rn --include='*.go' -E 'keys/azure|secrets/azure' .` and
  `grep -rn --include='*.go' -E '\.Encrypt\(' .`
- Affected if you call `Encrypt` with non-empty AAD on Azure: it now **returns
  an error** (AAD was silently ignored before). `Decrypt` with AAD still works and
  logs a warning once. `SigningKeyVersions` now works, returns `VAULT/KEY/VERSION`
  IDs (not URLs), and omits disabled versions. Vault names must match
  `^[A-Za-z0-9-]{3,24}$`.
- Fix, **[decision]**: pass `nil` AAD for Azure and accept that it was never
  bound, or move AAD-dependent data to a provider that supports it. Update code
  that parsed key IDs as URLs.

**15. Filesystem keys ignore non-numeric version files.**

- Grep: `grep -rn --include='*.go' -E 'keys\.(NewFilesystem|Filesystem)|KEY_FILESYSTEM_ROOT' .`
- Affected only if you placed version files by hand with non-digit names. They
  are now ignored by `Encrypt` and `SigningKeyVersions`, and `Decrypt` rejects
  ciphertext that names them (`invalid ciphertext: bad version`). Versions made
  by `CreateKeyVersion` are unaffected. (The fix closes a path-traversal bug.)
- Fix: rename such files to decimal digits.

**16. `secrets` Cacher fetches are detached from request cancellation.**

- Grep: `grep -rn --include='*.go' -E 'WrapCacher|SecretCacheTTL|SECRET_CACHE_TTL' .`
- Affected if you rely on a cancelled request aborting a slow secret fetch. The
  shared fetch now runs up to 30s (or the owner's later deadline), and a waiting
  caller's own cancellation isn't honored while it's in flight.
- Fix: usually none. Set backend timeouts if 30s is too long.

### Cache

**17. `cache.WriteThruLookup` coalesces per key.**

- Grep: `grep -rn --include='*.go' -E 'WriteThruLookup|cache\.New\[|ErrInvalidDuration' .`
- Affected if:
  - the `primaryLookup` closure captures a **request** ctx (`r.Context()`): one
    cancelled caller now fails every caller coalesced with it;
  - you expect each caller to run its own lookup after a failure: concurrent
    callers now share the owner's result **and error** (errors aren't cached;
    the next call retries);
  - a lookup can panic: waiters get `cache.ErrLookupPanicked`;
  - you pass a huge TTL as "forever" (such as `math.MaxInt64`): `New` returns
    `ErrInvalidDuration` above `cache.MaxExpireAfter` (100 years). The error's
    message changed; `==`/`errors.Is` still work.
- Fix:

  ```go
  v, err := c.WriteThruLookup(key, func() (T, error) {
      fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
      defer cancel()
      return fetch(fctx, key)
  })
  if errors.Is(err, cache.ErrLookupPanicked) { /* treat as a backend failure */ }
  ```

### Rendering

**18. `toJSON` returns real JSON.**

- Grep: `grep -rn 'toJSON' --include='*.html' --include='*.tmpl' --include='*.txt' --include='*.go' .`
- Affected if templates use `toJSON`. HTML templates used to get a base64 string
  and now get the JSON value (as `template.JS`, padded with spaces). Text
  templates used to print a byte list and now print the JSON string.
- Fix: drop workarounds (`printf "%s"`, `safeJS`, `atob`, base64 decoding):
  `const data = {{ toJSON .Data }};`

**19. `RenderCSV` `Content-Disposition` format.**

- Grep: `grep -rn --include='*.go' -E 'RenderCSV|attachment;filename' .`
- Affected if tests or clients match the exact header. It was
  `attachment;filename=x`; it is now `attachment; filename=x`, quoted when needed
  (`filename="a b.csv"`), with `filename*=utf-8''…` for non-ASCII.
- Fix: parse it with `mime.ParseMediaType` instead of string matching.

**20. `joinStrings`/`toSentence` with nil render `""`.** Before, a nil value
gave a 500. Grep: `grep -rn -E 'joinStrings|toSentence' --include='*.html' --include='*.tmpl' .`.
No fix needed; check that nothing relied on the error.

### API shape

**21. Variadic options changed three function types.**

`apidiff` reports these as incompatible:
`cookiestore.New(fn, opts, ...Option)`,
`middleware.HandleCSRF(h, ...CSRFOption)` and
`middleware.SecureHeaders(devMode, serverType, ...SecureOption)`.

- Grep: `grep -rnE --include='*.go' 'func\(bool, *middleware\.ServerType\)|func\(\*render\.Renderer\) *mux\.MiddlewareFunc|func\(cookiestore\.EntropyFunc, \*sessions\.Options\)' .`
  and `grep -rnE --include='*.go' '= *(middleware\.(SecureHeaders|HandleCSRF)|cookiestore\.New) *$' .`
- Affected if code stores these functions in a variable, field or parameter of
  the old func type. Every direct call still compiles.
- Fix: add the variadic parameter to the type, or wrap it in a closure:
  `func(dev bool, t middleware.ServerType) mux.MiddlewareFunc { return middleware.SecureHeaders(dev, t) }`.

### Patterns copied from `examples/ssr-oidc` or the auth skill

v1.1.0 also hardened the example app. Apps that copied it should check:

- **GET logout:** see entry 2.
- **Flow state cleared in a `defer`:** session changes made after the first
  response byte are lost, so a failed callback leaves state/nonce/verifier
  retryable.
  Grep: `grep -rn --include='*.go' -A8 'defer func' . | grep 'Values'` (the
  `-` or `:` after the file name gives the real line).
  Fix: read the values into locals and `delete` them before checking anything
  or writing the response.
- **Non-constant-time state compare:** `grep -rniE --include='*.go' '(state|nonce)"?\)? *!= ' .`
  (matches `Get("state") != want` and `idToken.Nonce != want`).
  Fix: `subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1`, and
  reject an empty value.
- **Authorization by unverified email:** `grep -rniE --include='*.go' --exclude='*_test.go' 'admin.*email|email.*admin|\.Email\)' .`,
  then `grep -rn --include='*.go' 'email_verified\|EmailVerified' .`; a hit in
  the first with nothing in the second is affected.
  Fix: require the `email_verified` claim to be the JSON boolean `true`, or
  key grants on the subject. **[decision]** Users whose IdP omits the claim lose
  access, and sessions created before the change must sign in again.
- **CSRF token not rotated at login:** `grep -rnE --include='*.go' 'ClearCSRFToken|Values *= *(map|make)' .`.
  Affected if login/logout handlers do neither. Fix: reset `session.Values`, or
  call `session.ClearCSRFToken(s)`, when privileges change.
- **`RenderJSON` with `err.Error()`:** `grep -rn --include='*.go' -A2 'RenderJSON(' . | grep 'err'`.
  It sends error text to clients; use `RenderJSON500` for unexpected errors.

## Step 3: bump and verify

```sh
go get github.com/mikehelmick/go-bananas@v1.1.0
go mod tidy
go build ./...
go vet ./...
go test ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

- Use **Go ≥ 1.27.4** (GO-2026-6859 in `net/http` is reachable on older 1.27.x).
  The module's `go` directive stays `1.27`, so bump CI and build images
  yourself.
- With a replace directive, update the replaced checkout instead of `go get`.
- If the build fails, it is almost certainly entry 21.
- Apply the non-decision fixes from Step 2, then re-run the block above.

## Step 4: report

Tell the user, in short lists:

1. **Version:** from → to, and the Go toolchain in use.
2. **Changed:** each fix you applied, with file:line.
3. **Needs a human decision:** every **[decision]** item that applies. Typical
   ones: `WithSSLHost` vs `WithTrustedHostHeaders`+`WithAllowedHosts`; whether to
   send `WithHSTSPreload`; SameSite=None for `form_post`/SAML; the
   `CheckSessionMaxAge` duration; the upload body limit; Azure AAD; the deploy
   strategy for split cookies; `email_verified` policy.
4. **Behavior changes with no code change:** for example, Vault now reads the
   latest version, `ServeHTTPHandler` timeouts, and dropped v1.0 split cookies.
5. **Verification:** build, test and `govulncheck` results.

Don't claim an item is clear without having grepped for it.
