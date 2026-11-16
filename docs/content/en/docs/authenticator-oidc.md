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
me.HandleFunc("/me", profile).Methods(http.MethodGet)
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

1. **`GET /login`** — generate `state` and `nonce`, store them on the session,
   and redirect to the provider's authorization endpoint
   (`oauth2.Config.AuthCodeURL`).
2. **`GET /auth/callback`** — read `state`, `nonce` (and any PKCE verifier) into
   locals and **delete them from the session first**, before writing anything.
   Then compare `state` in constant time (`subtle.ConstantTimeCompare`), handle
   an OAuth `error` parameter without calling the token endpoint, exchange the
   code, verify the ID token and `nonce`, and extract claims.
3. **Reset the session at login** — assign a fresh `session.Values` map before
   storing your user. This defends against session fixation, rotates the CSRF
   token, and restarts the `CheckSessionMaxAge` clock.
4. **`Authenticate`** reads that user back, as above.
5. **`POST /logout`** — log out with a POST form that carries `{{ .csrfField }}`,
   routed with `.Methods(http.MethodPost)` behind `HandleCSRF`. A `GET /logout`
   lets any site sign your users out.

Things to get right:

- **Session writes after the first byte are lost.** `RequireSession` saves the
  session when the response starts, so deleting flow state in a `defer` (which
  runs after the response is written) never reaches the cookie, and a failed
  callback could be retried with the same state.
- **Only trust verified emails.** If you authorize by email address, require the
  `email_verified` claim to be the JSON boolean `true`. Better still, key grants
  on the subject (`sub`).

The runnable
[`examples/ssr-oidc`](https://github.com/mikehelmick/go-bananas/tree/main/examples/ssr-oidc)
application implements this end to end with
[`coreos/go-oidc`](https://pkg.go.dev/github.com/coreos/go-oidc/v3/oidc) and
[`golang.org/x/oauth2`](https://pkg.go.dev/golang.org/x/oauth2) — and keeps those
dependencies out of the core module by being a separate module.
