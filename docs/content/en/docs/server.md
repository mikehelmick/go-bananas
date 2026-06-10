---
title: HTTP server
weight: 7
description: A small, gracefully-stoppable HTTP server.
---

The [`server`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/server)
package wraps the standard library with graceful shutdown. The listener is bound
at construction, so the address is available immediately, but serving does not
begin until you call `ServeHTTP`/`ServeHTTPHandler` — which block until the
context is cancelled.

```go
srv, err := server.New("8080") // empty string lets the OS choose a port
if err != nil {
	return err
}
log.Printf("listening on %s", srv.Addr())

ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer cancel()

// Blocks until ctx is cancelled, then shuts down with a 5s grace period.
return srv.ServeHTTPHandler(ctx, handler)
```

`ServeHTTPHandler` builds an `http.Server` with a sensible
`ReadHeaderTimeout`. If you need full control (custom timeouts, TLS config, an
observability wrapper around the handler), build your own `*http.Server` and call
`ServeHTTP` instead:

```go
return srv.ServeHTTP(ctx, &http.Server{
	ReadHeaderTimeout: 5 * time.Second,
	Handler:           myWrappedHandler,
})
```

Observability is intentionally **not** built in — wrap your handler with whatever
tracing/metrics middleware you use before passing it in. Shutdown errors are
aggregated with the standard library's `errors.Join`.
