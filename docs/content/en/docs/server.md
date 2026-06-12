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

## Health endpoints

The package ships Kubernetes-style probe handlers:

```go
r.Handle("/healthz", server.HealthzHandler())   // liveness: always 200 {"status":"ok"}

r.Handle("/readyz", server.ReadyzHandler(map[string]func(context.Context) error{
	"database": func(ctx context.Context) error { return db.PingContext(ctx) },
}))
```

`ReadyzHandler` runs every named check with the request context. If all pass it
responds `200 {"status":"ok"}`; otherwise `503` listing the failing check
names — error details are logged server-side and never returned in the body, so
a publicly-reachable probe can't leak internal hostnames or paths:

```json
{"status":"unavailable","failed":["database"]}
```

A check that panics is treated as failed rather than crashing the probe. Checks
should be fast and side-effect free; wrap slow checks in your own caching (the
[`cache`](cache) package works well) if probes are frequent.
