---
title: Authenticator & OIDC
weight: 5
description: The pluggable auth seam, and wiring OIDC with it.
---

go-bananas has exactly one authentication seam — the `Authenticator` interface —
and no dependency on any identity provider. You implement it; the framework gates
routes with it.

```go
type Authenticator interface {
	// Authenticate returns the principal, or (nil, nil) for an anonymous request.
	// A non-nil error results in a 500.
	Authenticate(r *http.Request) (principal any, err error)
}
```

Gate routes with `RequireAuthenticated`:

```go
me := r.NewRoute().Subrouter()
me.Use(middleware.RequireAuthenticated(myAuth, h))
me.HandleFunc("/me", profile)
```

On success the principal is stored on the context; retrieve it anywhere with
`webctx.PrincipalFromContext(ctx)`. An anonymous request gets a 401; an error
gets a 500.

## A session-backed authenticator

The simplest implementation reads a value an earlier login flow stored on the
session:

```go
type SessionAuthenticator struct{}

func (SessionAuthenticator) Authenticate(r *http.Request) (any, error) {
	s := webctx.SessionFromContext(r.Context())
	if s == nil {
		return nil, nil
	}
	u, _ := s.Values["user"].(*User)
	return u, nil // nil when anonymous
}
```

## Wiring OIDC

OIDC is a wiring exercise on top of the framework, not a framework dependency.
The plumbing it needs — sessions, CSRF, secure headers, idle expiry — already
ships here; the OIDC client itself lives in your application (or, as in the
example, a separate module). A typical flow:

1. **`/login`** — generate `state` and `nonce`, store them on the session, and
   redirect to the provider's authorization endpoint
   (`oauth2.Config.AuthCodeURL`).
2. **`/auth/callback`** — validate `state`, exchange the code, verify the ID
   token and `nonce`, extract claims, and store your user on the session.
3. **`Authenticate`** reads that user back, as above.

The runnable
[`examples/ssr-oidc`](https://github.com/mikehelmick/go-bananas/tree/main/examples/ssr-oidc)
application implements this end to end with
[`coreos/go-oidc`](https://pkg.go.dev/github.com/coreos/go-oidc/v3/oidc) and
[`golang.org/x/oauth2`](https://pkg.go.dev/golang.org/x/oauth2) — and keeps those
dependencies out of the core module by being a separate module.
