---
title: Documentation
linkTitle: Docs
menu: {main: {weight: 20}}
weight: 20
---

go-bananas is a server-side-rendering web framework plus a small
application-infrastructure layer for Go. It is assembled from focused, composable
packages rather than a single monolith, so you import only what you need.

## Packages at a glance

**Web layer**

| Package | Purpose |
|---|---|
| [`render`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/render) | `html/template` + `text/template` renderer with a rich FuncMap and SRI asset tags |
| [`middleware`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/middleware) | Composable `gorilla/mux` middleware + the `Authenticator` seam |
| [`session`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/session) · [`flash`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/flash) · [`cookiestore`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/cookiestore) | Typed session accessors, one-shot flash messages, hot-reloadable cookie store |
| [`webctx`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/webctx) · [`response`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/response) | Request-scoped context helpers and HTTP response/error helpers |

**Infrastructure layer**

| Package | Purpose |
|---|---|
| [`server`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/server) | Gracefully-stoppable HTTP server |
| [`secrets`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/secrets) · [`keys`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/keys) | Pluggable secret and key managers (filesystem/in-memory core; cloud opt-in) |
| [`cache`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/cache) | Generic, TTL-based in-memory cache |
| [`logging`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/logging) | Context-scoped `log/slog` logger |

These guides walk through each area. For the complete, always-current API, see
[pkg.go.dev](https://pkg.go.dev/github.com/mikehelmick/go-bananas) — every
headline API there has a runnable example.
