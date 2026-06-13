# 🍌 go-bananas

**The application framework for Go that is so simple, it's Bananas.**

[![CI](https://github.com/mikehelmick/go-bananas/actions/workflows/ci.yml/badge.svg)](https://github.com/mikehelmick/go-bananas/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/mikehelmick/go-bananas.svg)](https://pkg.go.dev/github.com/mikehelmick/go-bananas)
[![Go Report Card](https://goreportcard.com/badge/github.com/mikehelmick/go-bananas)](https://goreportcard.com/report/github.com/mikehelmick/go-bananas)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](./LICENSE)

go-bananas is a lean, server-side-rendered (SSR) web framework plus a small
application-infrastructure layer for Go. It is assembled from focused,
composable packages — a template renderer, composable middleware, CSRF,
secure-cookie sessions, flash messages, form binding/validation, a graceful HTTP
server, and pluggable secrets/keys — so you import only what you need. A single
pluggable `Authenticator` seam makes OIDC a wiring exercise, not a framework
dependency, and configuration is plain env vars via
[go-envconfig](https://github.com/sethvargo/go-envconfig).

It is extracted and generalized from Google's
[`exposure-notifications-verification-server`](https://github.com/google/exposure-notifications-verification-server)
and [`exposure-notifications-server`](https://github.com/google/exposure-notifications-server).

## 📚 Documentation

- **Guides & getting started:** **<https://mikehelmick.github.io/go-bananas>**
- **API reference:** <https://pkg.go.dev/github.com/mikehelmick/go-bananas>
- **Runnable example:** [`examples/ssr-oidc`](./examples/ssr-oidc) — a complete SSR + OIDC app.

## Install

go-bananas requires **Go 1.26** or newer.

```sh
go get github.com/mikehelmick/go-bananas@latest
```

## Quick start

```go
package main

import (
	"context"
	"embed"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mikehelmick/go-bananas/middleware"
	"github.com/mikehelmick/go-bananas/render"
	"github.com/mikehelmick/go-bananas/server"
	"github.com/mikehelmick/go-bananas/webctx"
)

//go:embed templates static
var assets embed.FS

func main() {
	ctx := context.Background()

	h, err := render.New(assets, render.WithDevMode(true))
	if err != nil {
		panic(err)
	}

	r := mux.NewRouter()
	r.Use(middleware.Recovery(h))
	r.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		m := webctx.TemplateMapFromContext(req.Context())
		m.Title("Home")
		h.RenderHTML(w, "home", m)
	})

	// Serve the embedded static assets the renderer's SRI tags point at.
	static := middleware.ConfigureStaticAssets(true)
	r.PathPrefix("/static/").Handler(static(http.FileServerFS(assets)))

	srv, err := server.New("8080")
	if err != nil {
		panic(err)
	}
	_ = srv.ServeHTTPHandler(ctx, r)
}
```

See the [getting-started guide](https://mikehelmick.github.io/go-bananas/docs/getting-started/)
for the full picture, including the recommended middleware chain.

## Packages

**Web layer**

| Package | Purpose |
|---|---|
| [`render`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/render) | `html`/`text` template renderer with a rich FuncMap and SRI asset tags |
| [`middleware`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/middleware) | Composable `gorilla/mux` middleware + the `Authenticator` seam |
| [`form`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/form) | SSR form binding + validation (`form`/`validate` tags) for the parse → validate → re-render loop |
| [`session`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/session) · [`flash`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/flash) · [`cookiestore`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/cookiestore) | Typed session accessors, one-shot flash messages, hot-reloadable cookie store |
| [`webctx`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/webctx) · [`response`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/response) | Request-scoped context helpers and HTTP response/error helpers |
| [`i18n`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/i18n) | gettext (`.po`) translations with Accept-Language matching |

**Infrastructure layer**

| Package | Purpose |
|---|---|
| [`server`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/server) | Gracefully-stoppable HTTP server |
| [`secrets`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/secrets) · [`keys`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/keys) | Pluggable secret/key managers (filesystem + in-memory core; cloud opt-in) |
| [`cache`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/cache) | Generic, TTL-based in-memory cache |
| [`logging`](https://pkg.go.dev/github.com/mikehelmick/go-bananas/logging) | Context-scoped [`log/slog`](https://pkg.go.dev/log/slog) logger + `logging.Config` (`LOG_LEVEL`/`LOG_MODE`) |

### Opt-in cloud providers

The core `secrets` and `keys` packages have **no cloud dependencies**. Cloud
providers are self-registering sub-packages you blank-import, so their SDKs are
only linked when used:

```go
import _ "github.com/mikehelmick/go-bananas/secrets/gcp"   // or aws, azure, vault
import _ "github.com/mikehelmick/go-bananas/keys/gcp"      // or aws, azure, vault
```

### Configuration

There is no config package. Packages that need settings expose an env-tagged
`Config` struct (`logging.Config`, `secrets.Config`, `keys.Config`); an app
**composes** them into its own config and processes everything once with
[go-envconfig](https://github.com/sethvargo/go-envconfig):

```go
type appConfig struct {
	Logging logging.Config // LOG_LEVEL, LOG_MODE
	Secrets secrets.Config

	DevMode bool   `env:"DEV_MODE, default=false"`
	Port    string `env:"PORT, default=8080"`
}

var cfg appConfig
if err := envconfig.Process(ctx, &cfg); err != nil { /* handle */ }
logger := logging.NewLoggerFromConfig(cfg.Logging)
```

## Claude Code skills

This repo ships [Claude Code](https://claude.com/claude-code) skills that help
developers and agents build go-bananas apps (scaffolding, the middleware chain,
authentication, and secrets/keys). Install them via the
[plugin marketplace](./.claude-plugin/marketplace.json):

```
/plugin marketplace add mikehelmick/go-bananas
```

## Contributing

Issues and pull requests are welcome. Run `make test` and `make lint` before
submitting.

## License

Apache 2.0 — see [LICENSE](./LICENSE). Significant portions are extracted from
the Exposure Notifications projects (Google LLC), also Apache 2.0.
