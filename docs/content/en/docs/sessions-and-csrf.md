---
title: Sessions, flash & CSRF
weight: 4
description: Secure-cookie sessions, one-shot flash messages, and CSRF protection.
---

## Sessions

go-bananas uses [`gorilla/sessions`](https://pkg.go.dev/github.com/gorilla/sessions)
for storage. The [`cookiestore`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/cookiestore)
package provides a secure-cookie store whose HMAC and encryption keys are
**hot-reloadable**: they come from an `EntropyFunc` consulted on every request,
so keys can be rotated (or sourced from a [secret/key manager](secrets-and-keys))
without a restart.

```go
var storeOpts []cookiestore.Option
if devMode {
	storeOpts = append(storeOpts, cookiestore.WithInsecureCookies()) // plain-HTTP dev only
}
store := cookiestore.New(entropy, &sessions.Options{
	Path:   "/",
	MaxAge: 3600,
}, storeOpts...)
```

### Cookie defaults

`cookiestore.New(fn, opts, ...Option)` copies `opts` (your struct is never
modified) and applies these defaults to the copy:

| Attribute | Value |
|---|---|
| `HttpOnly` | Always `true`. JavaScript cannot read the session cookie. |
| `Secure` | Always `true`, unless `WithInsecureCookies()` is passed. |
| `SameSite` | `Lax` when unset or `http.SameSiteDefaultMode`; an explicit `Strict` or `None` is kept. |
| `Path` | `"/"` when unset. |
| `MaxAge` | 30 days when unset. |

- **Local development over plain HTTP:** pass `cookiestore.WithInsecureCookies()`,
  and only in dev mode. Without it, Safari (and browsers on hosts other than
  `localhost`) won't send the cookie over `http://`.
- **`SameSite=None` always forces `Secure`**, even with `WithInsecureCookies()`,
  because browsers drop `None` cookies that are not `Secure`.
- **Cross-site POST callbacks need `None`.** `Lax` cookies are not sent on a
  cross-site POST, so OIDC `response_mode=form_post` and SAML POST bindings won't
  see the session. Set `SameSite: http.SameSiteNoneMode` for those apps.
- **The defaults apply only at construction.** A handler that edits
  `session.Options`, or code that edits the store's `CookieStore.Options` later,
  is not checked and can still weaken the cookie.

`New` returns a `sessions.Store` whose concrete type is still
`*sessions.CookieStore`, so existing `store.(*sessions.CookieStore)` type
assertions keep working.

### Loading and saving

Install `RequireSession` to load and save the session per request:

```go
r.Use(middleware.RequireSession(store, nil, h))
```

Read and write typed values through the
[`session`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/session) package
(`CSRFToken`, `LastActivity`, `CreatedAt`, `Nonce`, `Region`, and `Flash`), all of
which are nil-safe.

**Session writes must happen before the response starts.** `RequireSession`
saves the session when the handler writes the status line or the first byte of
the body. Changes made after that, including in a `defer` that runs after the
response is written, are lost. Make every session change (including deleting
one-time values like an OAuth `state`) before you render, redirect, or write.

### Split values

The second argument to `RequireSession` names session keys whose values are
large enough to need their own cookie (browsers cap a cookie at about 4 KB). Each
such key is stored in a companion `_gbs-<key>` cookie that is **bound to the main
session** by a random ID: it is joined only alongside the session that issued it,
and resetting or deleting the main session unbinds or expires it. The ID is kept
in the session values under the string key `"_gbs_binding"`, which is reserved:
don't use that key for your own values. One session
can split several keys. The companion cookie name doesn't include the session
name, so two named sessions (`RequireNamedSession`) on the same router must not
split the same key.

Split cookies written by v1.0.x carry no binding and are dropped once on upgrade.
During a rolling deploy, a visitor who moves from a v1.0.x instance back to a v1.1
instance loses split values each time, so finish the rollout before relying on
them.

### Downgrading to v1.0.x

v1.0.x can still read v1.1 session cookies: the cookie format is unchanged and
the values v1.1 adds use types v1.0.x already registers. v1.0.x ignores the
created-at time that `CheckSessionMaxAge` reads.

Split values are **kept** on a downgrade: v1.0.x joins split cookies without
checking the binding, so after rolling back it is exposed again to the replay
bug the binding fixes (a captured `_gbs-<key>` cookie replayed alone is joined
into a fresh session). v1.0.x also rewrites split cookies without the binding,
so returning to v1.1 drops split values once.

### Idle timeout

`CheckSessionIdleNoAuth` enforces an idle timeout on routes that have no other
auth check (the [example app](https://github.com/mikehelmick/go-bananas/tree/main/examples/ssr-oidc)
wires a 30-minute timeout). You supply the handler to run when the session is
idle:

```go
r.Use(middleware.CheckSessionIdleNoAuth(30*time.Minute, func(w http.ResponseWriter, req *http.Request) {
	response.SeeOther(w, req, "/login")
}))
```

### Absolute session lifetime

An idle timeout alone lets an active (or stolen) cookie live forever.
`CheckSessionMaxAge` bounds the total lifetime: once a session is older than
`maxAge`, its values are cleared and `onExpired` runs instead of the rest of the
chain.

```go
r.Use(middleware.RequireSession(store, nil, h))
r.Use(middleware.CheckSessionMaxAge(12*time.Hour, func(w http.ResponseWriter, req *http.Request) {
	response.SeeOther(w, req, "/login") // redirect; don't render
}))
r.Use(middleware.HandleCSRF(h))
```

- Install it **after `RequireSession` and before `HandleCSRF`**, so an expired
  session is cleared before its CSRF token is checked or rendered.
- Make `onExpired` **redirect**. `HandleCSRF` hasn't run, so a page rendered
  there has no CSRF token and its forms would fail.
- **Reset at the login boundary.** The age counts from when `RequireSession`
  first stamped the session, usually the visitor's first anonymous request.
  At login, assign a fresh `session.Values` map (which also defends against
  session fixation and rotates the CSRF token), or call
  `session.StoreCreatedAt(s, time.Now())`.
- Sessions from v1.0.x have no created-at time. They are accepted and their
  lifetime starts at their first request under v1.1.
- A `maxAge` of zero or less disables the check.

### Revocation

Cookie sessions **cannot be revoked one at a time**: the whole session lives in
the cookie, so a copied cookie stays valid until it goes unused for `MaxAge`
(`RequireSession` re-saves the cookie on every request), or until
`CheckSessionMaxAge` expires it. Rotating keys alone revokes nothing: cookies
are decoded against every key your `EntropyFunc` returns, so keeping an old key
for decoding keeps its sessions valid. To revoke **every** session, remove all
old keys from what the `EntropyFunc` returns. If
you need per-session revocation, keep a server-side session record and check it
in your `Authenticator`.

## Flash messages

Flash messages are stored on the session, surfaced once on the next render, then
automatically cleared. They survive a redirect, making the post/redirect/get
pattern clean:

```go
func submit(w http.ResponseWriter, r *http.Request) {
	flash := session.Flash(webctx.SessionFromContext(r.Context()))
	flash.Alert("Saved!")             // also Error, Warning
	response.SeeOther(w, r, "/")      // redirect; the flash shows on the next page
}
```

In a template:

```html
{{ range .flash.Alerts }}<div class="flash">{{ . }}</div>{{ end }}
```

Duplicate messages are de-duplicated, so adding the same text twice shows it once.

## CSRF

Install `HandleCSRF` **after** `RequireSession`. It stores a per-session token,
exposes masked helpers on the template map, and verifies the token on every
request except GET, HEAD and OPTIONS. TRACE needs a token like any other
mutating method.

```go
r.Use(middleware.HandleCSRF(h))
```

In your forms, include the hidden field; for JavaScript, read the meta tag and
send the token in the `X-CSRF-Token` header:

```html
<form method="POST" action="/submit">
  {{ .csrfField }}
  <button>Submit</button>
</form>

{{ .csrfMeta }}  <!-- <meta name="csrf-token" content="…"> -->
```

A request without a valid token receives a 401. The token is masked with a random
pad on every render to defeat BREACH-style attacks.

Because safe methods skip the check, every route must declare its methods with
`.Methods(...)`; see [the middleware chain](middleware-chain#routes-must-set-methods).

### Request body limit

When the token must come from the form (a non-safe request with no
`X-CSRF-Token` header), `HandleCSRF` reads the body to find it. Before parsing,
it limits the body to `middleware.DefaultCSRFMaxBodyBytes` (1 MiB):

- A urlencoded or multipart form over the limit gets **413 Request Entity Too
  Large**.
- The limit stays on `r.Body`, so the handler of a form-token request is bounded
  by it too (including `form.Bind` and `r.FormFile`).
- Requests that send the token in the `X-CSRF-Token` header are **not** limited
  by `HandleCSRF`, so JSON and streaming APIs are unaffected.
- Multipart temporary files created by this parse are removed when the request
  finishes.

To accept larger form uploads, give just those routes their own `HandleCSRF`
with a higher limit, rather than raising it everywhere. A mux subrouter also runs
its parent's `r.Use` middleware, so `HandleCSRF` must not be on the root router
in this layout; install it on two subrouters instead:

```go
r := mux.NewRouter()
r.Use(middleware.Recovery(h))
// ... logging, SecureHeaders, CSP, gzip ...
r.Use(middleware.RequireSession(store, nil, h))
r.Use(middleware.CheckSessionMaxAge(12*time.Hour, onExpired))

// Upload routes: 64 MiB limit. Register this subrouter first.
upload := r.PathPrefix("/upload").Subrouter()
upload.Use(middleware.HandleCSRF(h, middleware.WithCSRFMaxBodyBytes(64<<20)))
upload.Use(middleware.PopulateTemplateVariables(tmplConfig))
upload.HandleFunc("", handleUpload).Methods(http.MethodPost)

// Everything else: the 1 MiB default.
app := r.NewRoute().Subrouter()
app.Use(middleware.HandleCSRF(h))
app.Use(middleware.PopulateTemplateVariables(tmplConfig))
app.HandleFunc("/", home).Methods(http.MethodGet)
```

Middleware that must run after `HandleCSRF` (such as
`PopulateTemplateVariables`) moves onto each subrouter too.

### Rotate the token on login

The CSRF token lives as long as the session. When privileges change (login,
logout, role change), rotate it: either reset `session.Values` (which also clears
the token) or call `session.ClearCSRFToken(s)`. `HandleCSRF` issues a new token on
the next request.
