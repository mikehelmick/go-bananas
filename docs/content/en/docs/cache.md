---
title: Cache & logging
weight: 8
description: A generic TTL cache and a context-scoped slog logger.
---

## Cache

The [`cache`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/cache) package
is a generic, thread-safe, in-memory cache where every entry expires after the
same duration. A background goroutine purges expired entries; call `Stop` when
you are done.

```go
c, err := cache.New[string](time.Minute)
if err != nil {
	return err
}
defer c.Stop()

c.Set("greeting", "hello")
if v, ok := c.Lookup("greeting"); ok {
	fmt.Println(v)
}
```

`WriteThruLookup` computes a missing value and caches it, coalescing concurrent
callers so the (possibly expensive) lookup runs at most once:

```go
v, err := c.WriteThruLookup("user:42", func() (User, error) {
	return loadUser(ctx, 42)
})
```

It backs the secret-manager cacher (see [Secrets & keys](secrets-and-keys)).

## Logging

The [`logging`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/logging)
package is a thin layer over the standard library's `log/slog`. A logger is
carried on the context; retrieve it with `FromContext`, which returns a sensible
default when none is set, so callers never have to nil-check.

```go
logger := logging.NewLogger(slog.LevelInfo, devMode) // JSON in prod, text in dev
ctx = logging.WithLogger(ctx, logger)

// Deeper in the stack:
logging.FromContext(ctx).Info("handled request", "path", "/")

// Components identify themselves:
logging.Named(logger, "middleware.CSRF").Debug("validated token")
```

The `PopulateLogger` middleware installs a request-scoped logger tagged with the
request and trace IDs, so every line within a request is correlated.
