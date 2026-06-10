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
store := cookiestore.New(entropy, &sessions.Options{
	Path:     "/",
	MaxAge:   3600,
	HttpOnly: true,
	Secure:   true,
})
```

Install `RequireSession` to load and save the session per request:

```go
r.Use(middleware.RequireSession(store, nil, h))
```

Read and write typed values through the
[`session`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/session) package
(`CSRFToken`, `LastActivity`, `Nonce`, `Region`, and `Flash`), all of which are
nil-safe.

### Idle timeout

`CheckSessionIdleNoAuth` enforces an idle timeout on routes that have no other
auth check. You supply the handler to run when the session is idle:

```go
r.Use(middleware.CheckSessionIdleNoAuth(30*time.Minute, func(w http.ResponseWriter, req *http.Request) {
	response.SeeOther(w, req, "/login")
}))
```

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
exposes masked helpers on the template map, and verifies the token on mutating
requests (anything other than GET/HEAD/OPTIONS/TRACE):

```go
r.Use(middleware.HandleCSRF(h))
```

In your forms, include the hidden field; for JavaScript, read the meta tag:

```html
<form method="POST" action="/submit">
  {{ .csrfField }}
  <button>Submit</button>
</form>

{{ .csrfMeta }}  <!-- <meta name="csrf-token" content="…"> -->
```

A POST without a valid token receives a 401. The token is masked with a random
pad on every render to defeat BREACH-style attacks.
