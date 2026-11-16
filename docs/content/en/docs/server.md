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

## Timeouts

`ServeHTTPHandler` builds an `http.Server` with connection-level timeouts, so
slow or idle clients can't hold connections open indefinitely:

| Field | Value | Effect |
|---|---|---|
| `ReadHeaderTimeout` | 10 s | Time to read the request headers. |
| `ReadTimeout` | 5 min | Time to read the whole request, including the body. An upload that takes longer is cut off. |
| `IdleTimeout` | 120 s | How long a keep-alive connection may sit idle. |
| `WriteTimeout` | none | Not set, so streaming and long-polling responses keep working. |

If you need different limits (or TLS config, `MaxHeaderBytes`, an observability
wrapper around the handler), build your own `*http.Server` and call `ServeHTTP`
instead. Set the timeouts yourself — a zero value means no limit:

```go
return srv.ServeHTTP(ctx, &http.Server{
	ReadHeaderTimeout: 10 * time.Second,
	ReadTimeout:       30 * time.Minute, // allow slow, large uploads
	IdleTimeout:       120 * time.Second,
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
