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

Configuration comes from the environment via
[go-envconfig](https://github.com/sethvargo/go-envconfig): there is no config
package. Each framework package that needs settings exposes an env-tagged
`Config` struct (`logging.Config`, `secrets.Config`, `keys.Config`); the app
**composes** them into one struct and calls `envconfig.Process` **once** at
startup.

## Reference wiring

```go
//go:embed templates static
var assets embed.FS

// appConfig composes the per-package env-tagged Config structs with the app's
// own settings, processed once via go-envconfig.
type appConfig struct {
	Logging logging.Config // LOG_LEVEL, LOG_MODE
	Secrets secrets.Config // SECRETS_DIR, ...

	DevMode bool   `env:"DEV_MODE, default=false"`
	Port    string `env:"PORT, default=8080"`
	BuildID string `env:"BUILD_ID, default=dev"`
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var cfg appConfig
	if err := envconfig.Process(ctx, &cfg); err != nil { /* handle */ }
	ctx = logging.WithLogger(ctx, logging.NewLoggerFromConfig(cfg.Logging))

	h, err := render.New(assets,
		render.WithDevMode(cfg.DevMode),
		render.WithBuildID(cfg.BuildID),
		render.WithLogger(logging.FromContext(ctx)),
	)
	if err != nil { /* handle */ }
	devMode := cfg.DevMode

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
		h.RenderHTML(w, "home", homeData(req, messageForm{}, nil))
	}).Methods(http.MethodGet)
	r.HandleFunc("/submit", handleSubmit(h)).Methods(http.MethodPost)
	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		response.NotFound(w, req, h)
	})

	srv, err := server.New(cfg.Port)
	if err != nil { /* handle */ }
	_ = srv.ServeHTTPHandler(ctx, r)
}
```

## Form POST loop

The `form` package (`github.com/mikehelmick/go-bananas/form`) runs the SSR
parse → validate → re-render loop. Bind the request into a struct whose fields
carry a `form:"…"` binding tag and a `validate:"…"` tag; errors are keyed by the
form-tag name.

```go
// messageForm: the "form" tags drive binding (and key any Errors); the
// "validate" tags drive declarative validation.
type messageForm struct {
	Name    string `form:"name"    validate:"required"`
	Email   string `form:"email"   validate:"required,email"`
	Message string `form:"message" validate:"required,min=3"`
}

// homeData gives the template a uniform shape: a "Form" (the bound struct, so
// input is preserved on re-render) and a non-nil "Errors".
func homeData(r *http.Request, in messageForm, errs form.Errors) webctx.TemplateMap {
	m := webctx.TemplateMapFromContext(r.Context())
	m.Title("Home")
	if errs == nil {
		errs = form.Errors{}
	}
	m["Form"], m["Errors"] = in, errs
	return m
}

func handleSubmit(h *render.Renderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in messageForm
		errs, err := form.Bind(r, &in) // optionally form.WithMaxBodyBytes / WithMessages
		if err != nil {
			// Unprocessable request (bad content-type / oversized body): 400.
			response.BadRequest(w, r, h)
			return
		}
		if errs.Any() {
			// Invalid input: re-render with input + inline errors at 422.
			h.RenderHTMLStatus(w, http.StatusUnprocessableEntity, "home", homeData(r, in, errs))
			return
		}
		// Valid: flash + post/redirect/get.
		gbsession.Flash(webctx.SessionFromContext(r.Context())).
			Alert("Thanks for your message, %s!", in.Name)
		response.SeeOther(w, r, "/")
	}
}
```

`Bind` returns `(form.Errors, error)` with **two distinct failure modes**: a
non-nil **`error`** means the request is unprocessable (respond 400);
**`errs.Any()`** means the input is invalid (re-render at 422 with the input
preserved). `RenderHTMLStatus` accepts 422 (it is in the renderer's allowed-code
set). The form coexists with CSRF — the template still needs `{{ .csrfField }}`.

In the template, render preserved input with `.Form.*` and inline errors with
the `invalidIf` func plus `.Errors.Get`/`.Errors.Has`:

```html
<form method="POST" action="/submit" novalidate>
  {{ .csrfField }}
  <input name="email" value="{{ .Form.Email }}"
    class="{{ invalidIf (.Errors.Has "email") }}">
  {{ range .Errors.Get "email" }}<span class="error">{{ . }}</span>{{ end }}
  <button type="submit">Submit</button>
</form>
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
- Configure via env: compose the per-package `Config` structs into one app
  config and `envconfig.Process` it **once** — don't hand-roll `os.Getenv`
  helpers. `logging.NewLoggerFromConfig(cfg.Logging)` builds the logger from
  `LOG_LEVEL`/`LOG_MODE`.
- The complete, runnable reference is
  `github.com/mikehelmick/go-bananas/examples/ssr-oidc`.
- Full API: https://pkg.go.dev/github.com/mikehelmick/go-bananas
