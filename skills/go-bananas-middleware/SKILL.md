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
r.Use(middleware.SecureHeaders(devMode, middleware.ServerTypeHTML))
r.Use(middleware.ProcessNonce())                           // per-request CSP nonce
r.Use(middleware.ContentSecurityPolicy(
	"default-src 'self'; script-src 'self' 'nonce-{{nonce}}'; object-src 'none'"))
r.Use(middleware.GzipResponse())
r.Use(middleware.RequireSession(store, nil, h))            // load/save session
r.Use(middleware.CheckSessionIdleNoAuth(30*time.Minute, onIdle))
r.Use(middleware.HandleCSRF(h))                            // AFTER RequireSession
r.Use(middleware.PopulateTemplateVariables(middleware.TemplateConfig{ServerName: "app", DevMode: devMode}))
r.Use(middleware.InjectCurrentPath())
r.Use(middleware.ProcessLocale(locales))                   // i18n.Load(...) — see go-bananas-scaffold
```

And the static assets the renderer's SRI tags point at:

```go
static := middleware.ConfigureStaticAssets(devMode)
r.PathPrefix("/static/").Handler(static(http.FileServerFS(assets)))
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
- **`PopulateTemplateVariables` after the session middleware** — flash and CSRF
  helpers must already be on the template map.

## Common failures

- **Static assets (CSS/JS) return 404:** the renderer only emits the SRI tags; you
  must serve the files. Register the `PathPrefix("/static/")` route shown above.
- **CSRF returns 401 on a POST:** the request lacks a valid token. Include
  `{{ .csrfField }}` in the form, or send the `X-CSRF-Token` header (read from
  `{{ .csrfMeta }}`). The dev login and every mutating POST need it.
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

`MutateMethod` (forms emulate PATCH/DELETE; install very early), `RequireHeader`
/ `RequireHeaderValues` / `RequireHostHeader`, `OnlyIfEnabled` (feature flag →
404), `ProcessDebug(buildID, buildTag)`, and
`AddOperatingSystemFromUserAgent`.

## Guarding routes

```go
me := r.NewRoute().Subrouter()
me.Use(middleware.RequireAuthenticated(myAuthenticator, h)) // see go-bananas-auth
me.HandleFunc("/me", profile)
```

Parent `r.Use` middleware also applies to subrouter routes, so the chain above
still runs before `RequireAuthenticated`.

Full API: https://pkg.go.dev/github.com/mikehelmick/go-bananas/middleware
