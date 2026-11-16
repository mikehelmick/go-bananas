---
name: go-bananas-auth
description: Use when adding authentication to a go-bananas (github.com/mikehelmick/go-bananas) app — implementing the Authenticator interface, gating routes with RequireAuthenticated, or wiring an OIDC authorization-code flow. Triggers on "go-bananas auth", "protect a route", "implement Authenticator", "add OIDC/login to go-bananas".
---

# Authentication in go-bananas

go-bananas has exactly one auth seam: the `middleware.Authenticator` interface.
The framework depends on no identity provider — you implement the seam and gate
routes with it.

```go
type Authenticator interface {
	// (nil, nil) = anonymous → 401; non-nil error → 500; non-nil principal → allowed.
	Authenticate(r *http.Request) (principal any, err error)
}
```

## Gate routes

```go
me := r.NewRoute().Subrouter()
me.Use(middleware.RequireAuthenticated(myAuth, h))
me.HandleFunc("/me", profile).Methods(http.MethodGet)
```

On success the principal is stored on the context; read it anywhere with
`webctx.PrincipalFromContext(ctx)` (type-assert to your own user type).

## Authorization (403 vs 401)

`RequireAuthenticated` only answers "who is this?" — permission checks belong in
your handlers or your own middleware. Once a principal exists, deny with 403,
not 401:

```go
if !user.CanAdminister(course) {
	response.Forbidden(w, r, h) // renders the "403" template / {"error":"forbidden"}
	return
}
```

Use `response.Unauthorized` only for missing or invalid credentials. Ship a
`403.html` template alongside `401.html`, or use `response.NotFound` when the
existence of the resource is itself privileged (cross-tenant lookups).

## Session-backed authenticator

The simplest implementation reads a user that a login flow stored on the session:

```go
type SessionAuthenticator struct{}

func (SessionAuthenticator) Authenticate(r *http.Request) (any, error) {
	s := webctx.SessionFromContext(r.Context())
	if s == nil {
		return nil, nil
	}
	u, _ := s.Values["user"].(*User) // gob.Register(&User{}) once, in init()
	return u, nil // nil ⇒ anonymous
}
```

Register your principal type with `gob.Register(&User{})` so it survives the
secure-cookie session.

## OIDC (authorization-code flow)

Keep the OIDC client (`github.com/coreos/go-oidc/v3` + `golang.org/x/oauth2`) in
a **separate module** so it never enters the core module graph. The framework
already provides the session, CSRF, secure-header, and idle-timeout plumbing.

1. **`GET /login`** — generate `state` + `nonce`, store on the session, redirect
   to `oauth2Config.AuthCodeURL(state, oidc.Nonce(nonce))`.
2. **`GET /auth/callback`** — read `state`/`nonce`/verifier into locals and
   **delete them from the session before writing any response** (session writes
   after the first byte, including in a `defer`, are lost). Compare `state` and
   `nonce` with `subtle.ConstantTimeCompare` and reject empty values. If the
   provider sent an `error` parameter, flash and redirect without calling the
   token endpoint. Then exchange the code, verify the ID token via
   `provider.Verifier(...).Verify`, extract claims, **reset the session**
   (assign a fresh `session.Values` map: fixation defense, new CSRF token,
   restarts `CheckSessionMaxAge`), store your `*User`, redirect.
3. **`Authenticate`** reads that user back (as above).
4. **`POST /logout`** — never GET. Route it with `.Methods(http.MethodPost)`
   behind `HandleCSRF`, and use a form:
   `<form method="post" action="/logout">{{ .csrfField }}<button>Sign out</button></form>`.

**Authorize on verified identity.** If you grant access by email, require the
`email_verified` claim to be the JSON boolean `true` (decode into `any` and
check `v, _ := claims.EmailVerified.(bool)`); better, key grants on `sub`.

The full, working implementation is in
`github.com/mikehelmick/go-bananas/examples/ssr-oidc/auth.go`. Mirror its
structure. Note that mutating endpoints (including any dev-login POST) are
CSRF-protected, so they need `{{ .csrfField }}`.

Full API: https://pkg.go.dev/github.com/mikehelmick/go-bananas/middleware#Authenticator
