---
title: The middleware chain
weight: 2
description: Assemble the recommended middleware in the right order.
---

Every middleware constructor returns a `gorilla/mux.MiddlewareFunc`, so you
register most of them with `router.Use`. Most ordering is flexible, but a few have
prerequisites (noted below and in each function's GoDoc).

## Recommended order

```go
r := mux.NewRouter()
r.Use(middleware.Recovery(h))                              // outermost: catch panics
r.Use(middleware.PopulateRequestID(h))                     // assign a request id
r.Use(middleware.PopulateTraceID())                        // extract upstream trace id
r.Use(middleware.PopulateLogger(logging.DefaultLogger()))  // request-scoped logger
r.Use(middleware.LogRequests())                            // one Info line per request
r.Use(middleware.SecureHeaders(devMode, middleware.ServerTypeHTML,
	middleware.WithSSLHost("app.example.com")))            // options: see below
r.Use(middleware.ProcessNonce())                           // per-request CSP nonce
r.Use(middleware.ContentSecurityPolicy(
	"default-src 'self'; script-src 'self' 'nonce-{{nonce}}'; object-src 'none'"))
r.Use(middleware.GzipResponse())
r.Use(middleware.RequireSession(store, nil, h))            // load/save the session
r.Use(middleware.CheckSessionIdleNoAuth(30*time.Minute, onIdle))
r.Use(middleware.CheckSessionMaxAge(12*time.Hour, onExpired)) // before HandleCSRF
r.Use(middleware.HandleCSRF(h))                            // after RequireSession
r.Use(middleware.PopulateTemplateVariables(middleware.TemplateConfig{
	ServerName:     "go-bananas",
	ServerEndpoint: "https://app.example.com", // or AllowedHosts; see below
	DevMode:        devMode,
}))
r.Use(middleware.InjectCurrentPath())
r.Use(middleware.ProcessLocale(locales))                   // i18n — see the i18n guide

// Every route names its methods.
r.HandleFunc("/", home).Methods(http.MethodGet)
r.HandleFunc("/notes/{id}", deleteNote).Methods(http.MethodDelete)

// MutateMethod wraps the router; it is not installed with r.Use.
handler := middleware.MutateMethod()(r)
```

Pass `handler` (not `r`) to the [server](server). Don't forget the static assets
the renderer's SRI tags point at — serve them with cache headers via
`ConfigureStaticAssets`:

```go
static := middleware.ConfigureStaticAssets(devMode)
r.PathPrefix("/static/").Handler(static(http.FileServerFS(assets))).Methods(http.MethodGet, http.MethodHead)
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
  on the session. It limits form-token request bodies to 1 MiB; for upload
  routes that need more, see
  [the request body limit](sessions-and-csrf#request-body-limit).
- **`CheckSessionMaxAge` comes after `RequireSession` and before `HandleCSRF`**,
  so an expired session is cleared before its CSRF token is checked or handed
  to templates. Its `onExpired` handler should redirect, not render: `HandleCSRF`
  has not run yet, so a page rendered there has no CSRF token. See
  [Sessions & CSRF](sessions-and-csrf#absolute-session-lifetime).
- **`PopulateTemplateVariables` comes after the session middleware** so the flash
  data and CSRF helpers are already on the template map.
- **`MutateMethod` wraps the router** so the overridden method is what mux uses
  to match the route. Installed with `r.Use`, it runs after a route has already
  matched and only changes the method later handlers see.

## Routes must set `.Methods`

A mux route without `.Methods(...)` matches every method. `HandleCSRF` lets
GET, HEAD and OPTIONS through without a token, so a state-changing handler on a
route without `.Methods` can be reached by a HEAD or OPTIONS request that
carries no token. Give every route an explicit method list, and
use `POST` (or `PUT`/`PATCH`/`DELETE`) for anything that changes state —
including logout.

TRACE is not a safe method: `HandleCSRF` requires a token for it like any other
mutating request.

## SecureHeaders options

`SecureHeaders(devMode, serverType, opts ...SecureOption)` sets HSTS, nosniff, a
referrer policy, and (for `ServerTypeHTML`) `X-Frame-Options: DENY`. Outside dev
mode it redirects plain-HTTP requests to HTTPS on the request's `Host`.

Defaults:

- `X-Forwarded-Proto: https` is trusted to mark a request as HTTPS, so an app
  behind a TLS-terminating proxy doesn't redirect forever.
- `X-Forwarded-Host` is **not** trusted. A client could otherwise choose the
  host the HTTPS redirect points at.
- HSTS is sent with `includeSubDomains`, but without `preload`.

| Option | Effect |
|---|---|
| `WithSSLHost(host)` | Redirect HTTP to HTTPS on this fixed host instead of the request's host. The simplest safe choice behind a proxy. |
| `WithAllowedHosts(hosts...)` | Reject a request whose host is not listed (case-insensitive, port must match exactly) with **400 Bad Request**. Skipped in dev mode. This includes load-balancer and Kubernetes probes that send an IP as the host: allowlist the probe host, or mount `/healthz` outside `SecureHeaders`. |
| `WithTrustedHostHeaders(headers...)` | Take the host from the first non-empty header (for example `"X-Forwarded-Host"`). **Must be combined with `WithAllowedHosts` or `WithSSLHost`**; otherwise a forged header controls the redirect target, and a warning is logged at startup. |
| `WithHSTSPreload()` | Add `preload` to `Strict-Transport-Security`. Only use it once the domain and every subdomain are HTTPS-only and you intend to submit it to the preload list. Sites already on the list should add this option. |

Behind a proxy that rewrites `Host`, either pin the host or allowlist it:

```go
// Pin the redirect host (preferred).
r.Use(middleware.SecureHeaders(devMode, middleware.ServerTypeHTML,
	middleware.WithSSLHost("app.example.com")))

// Or trust X-Forwarded-Host, checked against an allowlist.
r.Use(middleware.SecureHeaders(devMode, middleware.ServerTypeHTML,
	middleware.WithTrustedHostHeaders("X-Forwarded-Host"),
	middleware.WithAllowedHosts("app.example.com", "www.example.com")))
```

## Template variables and `serverEndpoint`

`PopulateTemplateVariables` exposes `serverEndpoint` to templates for building
absolute links. It is resolved as follows:

1. A non-empty `TemplateConfig.ServerEndpoint` always wins. Set it in production.
2. In dev mode, it is derived from the request (`response.RealHostFromRequest`).
3. Otherwise, `r.Host` is matched case-insensitively against
   `TemplateConfig.AllowedHosts`. A match gives `"https://"` plus the listed
   entry; no match falls back to `"https://"` plus the first entry.
   Forwarding headers and the request scheme are ignored.
4. With neither set, `serverEndpoint` is `""` and a warning is logged once, so a
   client-chosen `Host` never ends up in rendered links.

`AllowedHosts` entries are bare hosts, optionally with a port
(`"app.example.com"`, `"app.example.com:8443"`). Entries that are empty or
contain `/` or whitespace (such as `"https://app.example.com"`) are skipped with
a warning.

## The grab bag

Other middleware you can drop in where appropriate:

| Middleware | Purpose |
|---|---|
| `MutateMethod` | Let HTML forms emulate `PUT`/`PATCH`/`DELETE` via a `_method` field (wraps the router; see below) |
| `RequireHeader` / `RequireHeaderValues` / `RequireHostHeader` | Reject requests lacking a required header/host |
| `OnlyIfEnabled` | Hide routes behind a 404 when a feature flag is off |
| `ProcessDebug` | Echo build info in response headers when `X-Debug` is set |
| `AddOperatingSystemFromUserAgent` | Infer the client OS for templates |

### MutateMethod

`MutateMethod` overrides the method only when **all** of these hold:

- the original method is `POST`;
- the `Content-Type` is `application/x-www-form-urlencoded`;
- `_method` is in the request **body** (never the query string), and the body
  is at most 64 KB and parses strictly with `url.ParseQuery`;
- the value, case-insensitively, is `PUT`, `PATCH` or `DELETE`.

Anything else is ignored and the request stays `POST`. Multipart bodies are
never read. The body is left intact for later handlers, and on an override
`r.PostForm` holds the parsed body, so the CSRF field and other form values
remain readable.

```html
<form method="POST" action="/notes/42">
  {{ .csrfField }}
  <input type="hidden" name="_method" value="DELETE">
  <button>Delete</button>
</form>
```

An override turns a cross-origin form POST into a `PUT`, `PATCH` or `DELETE`
without a CORS preflight, so every route reachable through an override must be
behind `HandleCSRF`.

## Authentication

Gate protected routes with `RequireAuthenticated`, which uses the pluggable
`Authenticator` seam — see [Authenticator & OIDC](authenticator-oidc).

```go
me := r.NewRoute().Subrouter()
me.Use(middleware.RequireAuthenticated(myAuthenticator, h))
me.HandleFunc("/me", profileHandler).Methods(http.MethodGet)
```
