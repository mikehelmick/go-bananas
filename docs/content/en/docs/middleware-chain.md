---
title: The middleware chain
weight: 2
description: Assemble the recommended middleware in the right order.
---

Every middleware constructor returns a `gorilla/mux.MiddlewareFunc`, so you
register them with `router.Use`. Most ordering is flexible, but a few have
prerequisites (noted below and in each function's GoDoc).

## Recommended order

```go
r := mux.NewRouter()
r.Use(middleware.Recovery(h))                              // outermost: catch panics
r.Use(middleware.PopulateRequestID(h))                     // assign a request id
r.Use(middleware.PopulateTraceID())                        // extract upstream trace id
r.Use(middleware.PopulateLogger(logging.DefaultLogger()))  // request-scoped logger
r.Use(middleware.LogRequests())                            // one Info line per request
r.Use(middleware.SecureHeaders(devMode, middleware.ServerTypeHTML))
r.Use(middleware.ProcessNonce())                           // per-request CSP nonce
r.Use(middleware.ContentSecurityPolicy(
	"default-src 'self'; script-src 'self' 'nonce-{{nonce}}'; object-src 'none'"))
r.Use(middleware.GzipResponse())
r.Use(middleware.RequireSession(store, nil, h))            // load/save the session
r.Use(middleware.CheckSessionIdleNoAuth(30*time.Minute, onIdle))
r.Use(middleware.HandleCSRF(h))                            // after RequireSession
r.Use(middleware.PopulateTemplateVariables(middleware.TemplateConfig{
	ServerName: "go-bananas",
	DevMode:    devMode,
}))
r.Use(middleware.InjectCurrentPath())
r.Use(middleware.ProcessLocale(locales))                   // i18n — see the i18n guide
```

Don't forget the static assets the renderer's SRI tags point at — serve them
with cache headers via `ConfigureStaticAssets`:

```go
static := middleware.ConfigureStaticAssets(devMode)
r.PathPrefix("/static/").Handler(static(http.FileServerFS(assets)))
```

## Why the order matters

- **`Recovery` is outermost** so it can turn a panic in any downstream handler or
  middleware into a clean 500.
- **`PopulateLogger` comes after `PopulateRequestID`/`PopulateTraceID`** so the
  request logger is tagged with those IDs, and **`LogRequests` after
  `PopulateLogger`** so each access-log line carries them too.
- **`ContentSecurityPolicy` comes after `ProcessNonce`** so the `{{nonce}}`
  placeholder in the policy is filled with the same per-request nonce templates
  read via `webctx.NonceFromContext`.
- **`HandleCSRF` comes after `RequireSession`** because the CSRF token is stored
  on the session.
- **`PopulateTemplateVariables` comes after the session middleware** so the flash
  data and CSRF helpers are already on the template map.

## The grab bag

Other middleware you can drop in where appropriate:

| Middleware | Purpose |
|---|---|
| `MutateMethod` | Let HTML forms emulate `PATCH`/`DELETE` via a `_method` field (install very early) |
| `RequireHeader` / `RequireHeaderValues` / `RequireHostHeader` | Reject requests lacking a required header/host |
| `OnlyIfEnabled` | Hide routes behind a 404 when a feature flag is off |
| `ProcessDebug` | Echo build info in response headers when `X-Debug` is set |
| `AddOperatingSystemFromUserAgent` | Infer the client OS for templates |

## Authentication

Gate protected routes with `RequireAuthenticated`, which uses the pluggable
`Authenticator` seam — see [Authenticator & OIDC](authenticator-oidc).

```go
me := r.NewRoute().Subrouter()
me.Use(middleware.RequireAuthenticated(myAuthenticator, h))
me.HandleFunc("/me", profileHandler)
```
