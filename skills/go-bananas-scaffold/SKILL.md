---
name: go-bananas-scaffold
description: Use when scaffolding a new server-side-rendered web application or HTTP server with the go-bananas framework (github.com/mikehelmick/go-bananas) — wiring the renderer, the recommended middleware chain, sessions, and the graceful server. Triggers on requests like "new go-bananas app", "set up an SSR server with go-bananas", "bootstrap a go-bananas project".
---

# Scaffold a go-bananas SSR application

go-bananas (`github.com/mikehelmick/go-bananas`, Go 1.26+) is a lean SSR web
framework. Scaffold a new app by composing the renderer, the middleware chain, a
cookie session store, and the graceful server.

## Steps

1. **Add the dependency:** `go get github.com/mikehelmick/go-bananas@latest`
2. **Create an `embed.FS`** containing `templates/*.html` and `static/js`,
   `static/css`. Template names come from `{{ define "name" }}`. Always provide
   `400.html`, `401.html`, `404.html`, and `500.html` — the error helpers render
   them.
3. **Build the renderer** with options (dev mode, build id, logger).
4. **Build a session store** with `cookiestore.New` (see the
   `go-bananas-secrets` skill to source keys from a secret manager).
5. **Assemble the router** with the recommended middleware order (see the
   `go-bananas-middleware` skill).
6. **Serve** with `server.ServeHTTPHandler` under a signal-cancelled context.

## Reference wiring

```go
//go:embed templates static
var assets embed.FS

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	h, err := render.New(assets,
		render.WithDevMode(devMode),
		render.WithBuildID(buildID),
		render.WithLogger(logging.FromContext(ctx)),
	)
	if err != nil { /* handle */ }

	store := cookiestore.New(entropyFunc, &sessions.Options{
		Path: "/", MaxAge: 3600, HttpOnly: true, Secure: !devMode, SameSite: http.SameSiteLaxMode,
	})

	r := mux.NewRouter()
	r.Use(middleware.Recovery(h))
	r.Use(middleware.PopulateRequestID(h))
	r.Use(middleware.PopulateTraceID())
	r.Use(middleware.PopulateLogger(logging.DefaultLogger()))
	r.Use(middleware.LogRequests())
	r.Use(middleware.SecureHeaders(devMode, middleware.ServerTypeHTML))
	r.Use(middleware.ProcessNonce())
	r.Use(middleware.ContentSecurityPolicy(
		"default-src 'self'; script-src 'self' 'nonce-{{nonce}}'; object-src 'none'"))
	r.Use(middleware.GzipResponse())
	r.Use(middleware.RequireSession(store, nil, h))
	r.Use(middleware.HandleCSRF(h))
	r.Use(middleware.PopulateTemplateVariables(middleware.TemplateConfig{ServerName: "my-app", DevMode: devMode}))
	r.Use(middleware.InjectCurrentPath())

	// REQUIRED: serve the embedded static assets the SRI tags point at,
	// or every page's CSS/JS will 404.
	static := middleware.ConfigureStaticAssets(devMode)
	r.PathPrefix("/static/").Handler(static(http.FileServerFS(assets)))

	// Liveness/readiness probes.
	r.Handle("/healthz", server.HealthzHandler()).Methods(http.MethodGet)

	r.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		m := webctx.TemplateMapFromContext(req.Context())
		m.Title("Home")
		h.RenderHTML(w, "home", m)
	})
	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		response.NotFound(w, req, h)
	})

	srv, err := server.New("8080")
	if err != nil { /* handle */ }
	_ = srv.ServeHTTPHandler(ctx, r)
}
```

## Templates

In `home.html`, surface the framework-provided template values:

```html
{{ define "home" }}<!doctype html>
<title>{{ .title }}</title>
{{ .csrfMeta }}
{{ cssIncludeTag }}
{{ range .flash.Alerts }}<div class="flash">{{ . }}</div>{{ end }}
<form method="POST" action="/submit">{{ .csrfField }}<button>Go</button></form>
{{ jsIncludeTag }}{{ end }}
```

## Notes

- Keep cloud, OIDC, and other heavy deps out of the core binary by putting them
  in a **separate module** (as the `examples/ssr-oidc` app does).
- The complete, runnable reference is
  `github.com/mikehelmick/go-bananas/examples/ssr-oidc`.
- Full API: https://pkg.go.dev/github.com/mikehelmick/go-bananas
