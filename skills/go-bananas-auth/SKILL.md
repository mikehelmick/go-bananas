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
me.HandleFunc("/me", profile)
```

On success the principal is stored on the context; read it anywhere with
`webctx.PrincipalFromContext(ctx)` (type-assert to your own user type).

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
2. **`GET /auth/callback`** — verify `state`, exchange the code, verify the ID
   token via `provider.Verifier(...).Verify`, check `nonce`, extract claims,
   store your `*User` on the session, redirect.
3. **`Authenticate`** reads that user back (as above).

The full, working implementation is in
`github.com/mikehelmick/go-bananas/examples/ssr-oidc/auth.go`. Mirror its
structure. Note that mutating endpoints (including any dev-login POST) are
CSRF-protected, so they need `{{ .csrfField }}`.

Full API: https://pkg.go.dev/github.com/mikehelmick/go-bananas/middleware#Authenticator
