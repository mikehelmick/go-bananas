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

`New` returns `cache.ErrInvalidDuration` for a negative duration or one longer
than `cache.MaxExpireAfter` (100 years).

`WriteThruLookup` computes a missing value and caches it:

```go
v, err := c.WriteThruLookup("user:42", func() (User, error) {
	// Don't use one request's ctx here: other callers share this lookup.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(reqCtx), 5*time.Second)
	defer cancel()
	return loadUser(ctx, 42)
})
```

How it behaves under concurrency:

- **Coalescing is per key.** The first caller for a missing key (the owner) runs
  its lookup. Other callers for the same key wait and share its result; their
  own lookup function is not called. The lookup runs without holding the cache
  lock, so other keys, `Lookup`, `Set` and `Clear` are not blocked by a slow
  backend.
- **Errors are shared, not cached.** Whatever error the owner's lookup returns,
  including a context error, goes to the owner and every waiter. Nothing is
  stored, so the next call tries again.
- **Panics.** If the lookup panics, the panic reaches the owner and waiters get
  `cache.ErrLookupPanicked`.
- **Don't depend on one caller's context.** Because waiters share the owner's
  result, a lookup that uses the owner's request context fails every waiter when
  that request is cancelled. Run it on `context.WithoutCancel(ctx)` plus your own
  timeout, as above. Waiters block until the owner's lookup returns, so that
  timeout is also how long they can wait.
- **`Set` and `Clear` win.** If either runs for a key while its lookup is in
  flight, the lookup's result is returned to its callers but not stored. After
  that, a new miss can start a second lookup alongside the first.
- A lookup must not call `WriteThruLookup` for its own key, or it deadlocks.

It backs the secret-manager cacher, which uses the same detached-context
pattern. Its shared fetch always has a fixed 30-second timeout and ignores
every caller's deadline. A waiting caller's context is not honored, and providers must
not authorize per caller through context values (see
[Secrets & keys](secrets-and-keys#caching-and-cancellation)).

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
