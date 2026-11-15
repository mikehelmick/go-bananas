---
name: go-bananas-middleware
description: Use when adding, ordering, or troubleshooting go-bananas (github.com/mikehelmick/go-bananas) middleware — the gorilla/mux chain for recovery, request/trace IDs, logging, secure headers, gzip, sessions, CSRF, template variables, locale, and route guards. Triggers on "add go-bananas middleware", "middleware order", "CSRF not working", "session middleware".
---

# Wire go-bananas middleware

All middleware constructors in `github.com/mikehelmick/go-bananas/middleware`
return a `gorilla/mux.MiddlewareFunc`. Register them with `router.Use`. Order is
mostly flexible, but the prerequisites below are load-bearing.

## Recommended order (and why)

```go
r.Use(middleware.Recovery(h))                              // outermost: catch panics → 500
r.Use(middleware.PopulateRequestID(h))                     // assign request id
r.Use(middleware.PopulateTraceID())                        // extract upstream trace id
r.Use(middleware.PopulateLogger(logging.DefaultLogger()))  // AFTER request/trace id
r.Use(middleware.LogRequests())                            // access log; AFTER PopulateLogger
r.Use(middleware.SecureHeaders(devMode, middleware.ServerTypeHTML,
	middleware.WithSSLHost("app.example.com")))            // see "SecureHeaders options"
r.Use(middleware.ProcessNonce())                           // per-request CSP nonce
r.Use(middleware.ContentSecurityPolicy(
	"default-src 'self'; script-src 'self' 'nonce-{{nonce}}'; object-src 'none'"))
r.Use(middleware.GzipResponse())
r.Use(middleware.RequireSession(store, nil, h))            // load/save session
r.Use(middleware.CheckSessionIdleNoAuth(30*time.Minute, onIdle))
r.Use(middleware.CheckSessionMaxAge(12*time.Hour, onExpired)) // AFTER RequireSession, BEFORE HandleCSRF
r.Use(middleware.HandleCSRF(h))                            // AFTER RequireSession
r.Use(middleware.PopulateTemplateVariables(middleware.TemplateConfig{
	ServerName:     "app",
	ServerEndpoint: "https://app.example.com",             // or AllowedHosts; "" otherwise outside dev
	DevMode:        devMode,
}))
r.Use(middleware.InjectCurrentPath())
r.Use(middleware.ProcessLocale(locales))                   // i18n.Load(...) — see go-bananas-scaffold

r.HandleFunc("/", home).Methods(http.MethodGet)            // EVERY route sets .Methods

handler := middleware.MutateMethod()(r)                    // optional; WRAPS the router, not r.Use
// srv.ServeHTTPHandler(ctx, handler)
```

And the static assets the renderer's SRI tags point at:

```go
static := middleware.ConfigureStaticAssets(devMode)
r.PathPrefix("/static/").Handler(static(http.FileServerFS(assets))).Methods(http.MethodGet, http.MethodHead)
```

Hard ordering rules:
- **`Recovery` first** so it wraps everything downstream.
- **`PopulateLogger` after the ID middleware** so logs are tagged with request/trace id;
  **`LogRequests` after `PopulateLogger`** so access logs carry them too.
- **`ContentSecurityPolicy` after `ProcessNonce`** — the `{{nonce}}` placeholder is
  filled with the same nonce templates read from the context.
- **`HandleCSRF` after `RequireSession`** — the CSRF token lives on the session.
  `HandleCSRF` only validates the token; the `form` package
  (`github.com/mikehelmick/go-bananas/form`) handles the parse → validate →
  re-render loop for a POST and **coexists** with it — forms still need
  `{{ .csrfField }}`. See the `go-bananas-scaffold` skill for the loop.
- **`CheckSessionMaxAge` after `RequireSession` and before `HandleCSRF`** — an
  expired session is cleared before its token is checked or rendered. Its
  `onExpired` must **redirect**, not render (no CSRF token exists yet). Reset
  `session.Values` (or call `session.StoreCreatedAt`) at login so the lifetime
  starts there.
- **`PopulateTemplateVariables` after the session middleware** — flash and CSRF
  helpers must already be on the template map.
- **`MutateMethod` wraps the router** (`middleware.MutateMethod()(r)`) so the
  overridden method is used for routing. Under `r.Use` it runs after the route
  matched and only changes what later handlers see. It overrides only a `POST`
  with a urlencoded body (at most 64 KB) whose `_method` is `PUT`, `PATCH` or
  `DELETE`; query-string and multipart `_method` are ignored. Every route
  reachable through an override must be behind `HandleCSRF`.
- **Every route sets `.Methods(...)`** — GET, HEAD and OPTIONS skip CSRF, so a
  state-changing handler on a route with no method list can be reached by a
  HEAD or OPTIONS request that carries no token. Static assets use
  `.Methods(http.MethodGet, http.MethodHead)`. TRACE needs a token.

## SecureHeaders options

`SecureHeaders(devMode, serverType, opts...)`. Defaults: `X-Forwarded-Proto:
https` is trusted; **`X-Forwarded-Host` is not**; HSTS has no `preload`.

- `WithSSLHost(host)` — pin the HTTPS redirect host. Preferred behind a proxy
  that rewrites `Host`.
- `WithAllowedHosts(hosts...)` — reject other hosts with 400 (skipped in dev).
- `WithTrustedHostHeaders("X-Forwarded-Host")` — only together with
  `WithAllowedHosts` or `WithSSLHost`; alone, a forged header picks the redirect
  target (a warning is logged).
- `WithHSTSPreload()` — opt in to `preload` (sites already on the list).

## CSRF body limit

When the token comes from the form (no `X-CSRF-Token` header), `HandleCSRF`
limits the body to `middleware.DefaultCSRFMaxBodyBytes` (1 MiB) and returns
**413** above it; the limit carries through to the handler. Header-token
(JSON/streaming) requests are not limited. For large uploads, don't put
`HandleCSRF` on the root router (subrouters inherit parent middleware); install
it on an upload subrouter with `middleware.WithCSRFMaxBodyBytes(n)` and on a
catch-all `r.NewRoute().Subrouter()` with the default.

## Common failures

- **Static assets (CSS/JS) return 404:** the renderer only emits the SRI tags; you
  must serve the files. Register the `PathPrefix("/static/")` route shown above.
- **CSRF returns 401 on a POST:** the request lacks a valid token. Include
  `{{ .csrfField }}` in the form, or send the `X-CSRF-Token` header (read from
  `{{ .csrfMeta }}`). The dev login and every mutating POST need it.
- **413 on a form POST:** the form exceeded `HandleCSRF`'s 1 MiB limit. Raise it
  for that route with `WithCSRFMaxBodyBytes` (see "CSRF body limit").
- **`_method` override ignored:** `MutateMethod` is under `r.Use` instead of
  wrapping the router, the value is in the query string or a multipart body, or
  the target isn't PUT/PATCH/DELETE.
- **HTTPS redirect goes to the internal host behind a proxy:** `X-Forwarded-Host`
  is no longer trusted; add `WithSSLHost(...)`.
- **`serverEndpoint` is empty in production:** set
  `TemplateConfig.ServerEndpoint` (or `AllowedHosts`).
- **Session change didn't stick:** session writes made after the response
  started (e.g. in a `defer`) are lost; make them before writing.
- **`MissingSession` / nil session:** `HandleCSRF` or a handler ran without
  `RequireSession` ahead of it.
- **Flash never appears:** flashes are read-once and cleared; render them on the
  page you redirect to, and ensure `PopulateTemplateVariables` runs after
  `RequireSession`.
- **CSP header contains `{{nonce}}` literally:** `ContentSecurityPolicy` was
  installed before `ProcessNonce` (or `ProcessNonce` is missing).
- **`form.Bind` failure handled wrong:** it returns `(form.Errors, error)` with
  two distinct modes — a non-nil **`error`** is an unprocessable request (bad
  content-type / oversized body) → respond **400**; **`errs.Any()`** is invalid
  input → re-render at **422**. Don't collapse them into one branch.
- **Unchecked checkbox (accept-terms) never errors:** an unchecked HTML checkbox
  sends no value, so a `bool` field stays `false` and tag validation can't flag
  it. Enforce it with a `Validate() form.Errors` method on the struct
  (the `form.Validator` interface) instead.

## Optional middleware

`MutateMethod` (forms emulate PUT/PATCH/DELETE; wraps the router), `RequireHeader`
/ `RequireHeaderValues` / `RequireHostHeader`, `OnlyIfEnabled` (feature flag →
404), `ProcessDebug(buildID, buildTag)`, and
`AddOperatingSystemFromUserAgent`.

## Guarding routes

```go
me := r.NewRoute().Subrouter()
me.Use(middleware.RequireAuthenticated(myAuthenticator, h)) // see go-bananas-auth
me.HandleFunc("/me", profile).Methods(http.MethodGet)
```

Parent `r.Use` middleware also applies to subrouter routes, so the chain above
still runs before `RequireAuthenticated`.

Full API: https://pkg.go.dev/github.com/mikehelmick/go-bananas/middleware
