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
r.Use(middleware.SecureHeaders(devMode, middleware.ServerTypeHTML))
r.Use(middleware.GzipResponse())
r.Use(middleware.RequireSession(store, nil, h))            // load/save session
r.Use(middleware.HandleCSRF(h))                            // AFTER RequireSession
r.Use(middleware.PopulateTemplateVariables(middleware.TemplateConfig{ServerName: "app", DevMode: devMode}))
r.Use(middleware.InjectCurrentPath())
r.Use(middleware.ProcessLocale(localeProvider))            // optional i18n
```

Hard ordering rules:
- **`Recovery` first** so it wraps everything downstream.
- **`PopulateLogger` after the ID middleware** so logs are tagged with request/trace id.
- **`HandleCSRF` after `RequireSession`** — the CSRF token lives on the session.
- **`PopulateTemplateVariables` after the session middleware** — flash and CSRF
  helpers must already be on the template map.

## Common failures

- **CSRF returns 401 on a POST:** the request lacks a valid token. Include
  `{{ .csrfField }}` in the form, or send the `X-CSRF-Token` header (read from
  `{{ .csrfMeta }}`). The dev login and every mutating POST need it.
- **`MissingSession` / nil session:** `HandleCSRF` or a handler ran without
  `RequireSession` ahead of it.
- **Flash never appears:** flashes are read-once and cleared; render them on the
  page you redirect to, and ensure `PopulateTemplateVariables` runs after
  `RequireSession`.

## Optional middleware

`MutateMethod` (forms emulate PATCH/DELETE; install very early), `RequireHeader`
/ `RequireHeaderValues` / `RequireHostHeader`, `OnlyIfEnabled` (feature flag →
404), `ProcessDebug(buildID, buildTag)`, `ProcessNonce` (CSP),
`AddOperatingSystemFromUserAgent`, and `CheckSessionIdleNoAuth(ttl, onIdle)`.

## Guarding routes

```go
me := r.NewRoute().Subrouter()
me.Use(middleware.RequireAuthenticated(myAuthenticator, h)) // see go-bananas-auth
me.HandleFunc("/me", profile)
```

Parent `r.Use` middleware also applies to subrouter routes, so the chain above
still runs before `RequireAuthenticated`.

Full API: https://pkg.go.dev/github.com/mikehelmick/go-bananas/middleware
