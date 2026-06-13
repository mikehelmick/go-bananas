// Copyright 2026 the go-bananas authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Command ssr-oidc is a runnable example that wires the go-bananas web and
// infrastructure layers into a small server-side-rendered application: a CSRF-
// protected form with flash messages, a JSON endpoint, an OIDC sign-in flow, and
// secure-cookie sessions whose keys are sourced from a secret manager.
//
// It is a separate Go module (see go.mod) so its OIDC and example-only
// dependencies never enter the core go-bananas module graph.
package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/mikehelmick/go-bananas/cookiestore"
	"github.com/mikehelmick/go-bananas/form"
	"github.com/mikehelmick/go-bananas/i18n"
	"github.com/mikehelmick/go-bananas/logging"
	"github.com/mikehelmick/go-bananas/middleware"
	"github.com/mikehelmick/go-bananas/render"
	"github.com/mikehelmick/go-bananas/response"
	"github.com/mikehelmick/go-bananas/secrets"
	"github.com/mikehelmick/go-bananas/server"
	gbsession "github.com/mikehelmick/go-bananas/session"
	"github.com/mikehelmick/go-bananas/webctx"
	"github.com/sethvargo/go-envconfig"
)

//go:embed templates static locales
var assets embed.FS

// csp is the Content-Security-Policy for every page. The nonce placeholder is
// filled per request by middleware.ContentSecurityPolicy from the ProcessNonce
// nonce, so any inline script tagged with the template nonce is trusted.
const csp = "default-src 'self'; script-src 'self' 'nonce-{{nonce}}'; " +
	"style-src 'self'; object-src 'none'; frame-ancestors 'none'"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := realMain(ctx); err != nil {
		logging.DefaultLogger().Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

// oidcConfig holds the OpenID Connect settings, env-tagged so go-envconfig can
// populate them as part of the composed appConfig. An empty Issuer disables the
// OIDC flow (the login routes then report that sign-in is unavailable).
type oidcConfig struct {
	Issuer       string `env:"OIDC_ISSUER"`
	ClientID     string `env:"OIDC_CLIENT_ID"`
	ClientSecret string `env:"OIDC_CLIENT_SECRET"`
	RedirectURL  string `env:"OIDC_REDIRECT_URL"`
}

// appConfig is the single, composed configuration for the example, processed
// once at startup via go-envconfig. Embedding the framework's own env-tagged
// config structs (logging.Config, secrets.Config) demonstrates how an
// application layers its settings on top of the building blocks.
type appConfig struct {
	Logging logging.Config
	Secrets secrets.Config
	OIDC    oidcConfig

	DevMode bool   `env:"DEV_MODE, default=false"`
	Port    string `env:"PORT, default=8080"`
	BuildID string `env:"BUILD_ID, default=dev"`
}

func realMain(ctx context.Context) error {
	var cfg appConfig
	if err := envconfig.Process(ctx, &cfg); err != nil {
		return fmt.Errorf("failed to process configuration: %w", err)
	}

	// secrets.Config defaults SecretsDir to the production path /var/run/secrets.
	// This runnable example keeps its cookie key locally instead, but only when
	// the operator did not set SECRETS_DIR — an explicit value (including
	// /var/run/secrets) is always honored.
	if _, ok := os.LookupEnv("SECRETS_DIR"); !ok {
		cfg.Secrets.SecretsDir = "./local-secrets"
	}

	logger := logging.NewLoggerFromConfig(cfg.Logging)
	ctx = logging.WithLogger(ctx, logger)

	app, err := newApp(ctx, &cfg)
	if err != nil {
		return err
	}

	srv, err := server.New(cfg.Port)
	if err != nil {
		return err
	}
	if app.devMode {
		logger.Warn("DEV MODE IS ENABLED: insecure cookies, password-less /dev/login, " +
			"and verbose error pages are active. Set DEV_MODE=false in production.")
	}
	logger.Info("server listening", "addr", srv.Addr(), "devMode", app.devMode)
	return srv.ServeHTTPHandler(ctx, app.router())
}

// app holds the wired dependencies shared by the HTTP handlers.
type app struct {
	renderer *render.Renderer
	store    sessions.Store
	locales  *i18n.LocaleMap
	devMode  bool
	buildID  string
	oidc     *OIDC // nil when OIDC is not configured
}

func newApp(ctx context.Context, cfg *appConfig) (*app, error) {
	devMode := cfg.DevMode

	renderer, err := render.New(assets,
		render.WithDevMode(devMode),
		render.WithBuildID(cfg.BuildID),
		render.WithLogger(logging.FromContext(ctx)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create renderer: %w", err)
	}

	// Source the secure-cookie keys from a secret manager, demonstrating the
	// infra and web layers composing. Any registered secrets provider works; the
	// filesystem provider keeps the example self-contained.
	entropy, err := cookieEntropy(ctx, cfg.Secrets)
	if err != nil {
		return nil, fmt.Errorf("failed to set up cookie entropy: %w", err)
	}
	store := cookiestore.New(entropy, &sessions.Options{
		Path:     "/",
		MaxAge:   3600,
		HttpOnly: true,
		Secure:   !devMode,
		SameSite: http.SameSiteLaxMode,
	})

	// Load the gettext translations; ProcessLocale serves the best match per
	// request and the templates read it via the "t"/"tDefault" functions.
	localesFS, err := fs.Sub(assets, "locales")
	if err != nil {
		return nil, err
	}
	locales, err := i18n.Load(localesFS)
	if err != nil {
		return nil, fmt.Errorf("failed to load locales: %w", err)
	}

	a := &app{renderer: renderer, store: store, locales: locales, devMode: devMode, buildID: cfg.BuildID}

	if cfg.OIDC.Issuer != "" {
		oidc, err := NewOIDC(ctx, renderer, cfg.OIDC.Issuer,
			cfg.OIDC.ClientID, cfg.OIDC.ClientSecret, cfg.OIDC.RedirectURL)
		if err != nil {
			return nil, err
		}
		a.oidc = oidc
	}

	return a, nil
}

// router builds the gorilla/mux router with the recommended middleware chain.
func (a *app) router() http.Handler {
	r := mux.NewRouter()

	r.Use(middleware.Recovery(a.renderer))
	r.Use(middleware.PopulateRequestID(a.renderer))
	r.Use(middleware.PopulateTraceID())
	r.Use(middleware.PopulateLogger(logging.DefaultLogger()))
	r.Use(middleware.LogRequests())
	r.Use(middleware.SecureHeaders(a.devMode, middleware.ServerTypeHTML))
	r.Use(middleware.ProcessNonce())
	r.Use(middleware.ContentSecurityPolicy(csp))
	r.Use(middleware.GzipResponse())
	r.Use(middleware.RequireSession(a.store, nil, a.renderer))
	// Expire sessions idle for more than 30 minutes: discard the stale session
	// (logging out any idle user) and send the visitor back to the home page,
	// which issues a fresh session.
	r.Use(middleware.CheckSessionIdleNoAuth(30*time.Minute, func(w http.ResponseWriter, r *http.Request) {
		resetSession(r)
		response.SeeOther(w, r, "/")
	}))
	r.Use(middleware.HandleCSRF(a.renderer))
	r.Use(middleware.PopulateTemplateVariables(middleware.TemplateConfig{
		ServerName: "go-bananas",
		BuildID:    a.buildID,
		DevMode:    a.devMode,
	}))
	r.Use(middleware.InjectCurrentPath())
	r.Use(middleware.ProcessLocale(a.locales))
	r.Use(a.injectPrincipal)

	// Serve the embedded static assets the renderer's SRI tags point at.
	static := middleware.ConfigureStaticAssets(a.devMode)
	r.PathPrefix("/static/").Handler(static(http.FileServerFS(assets)))

	// Liveness and readiness probes.
	r.Handle("/healthz", server.HealthzHandler()).Methods(http.MethodGet)
	r.Handle("/readyz", server.ReadyzHandler(map[string]func(context.Context) error{
		"sessions": func(context.Context) error { return nil }, // demo check
	})).Methods(http.MethodGet)

	r.HandleFunc("/", a.handleHome).Methods(http.MethodGet)
	r.HandleFunc("/submit", a.handleSubmit).Methods(http.MethodPost)
	r.HandleFunc("/api/health", a.handleHealth).Methods(http.MethodGet)
	r.HandleFunc("/logout", a.handleLogout).Methods(http.MethodGet)

	if a.oidc != nil {
		r.HandleFunc("/login", a.oidc.Login).Methods(http.MethodGet)
		r.HandleFunc("/auth/callback", a.oidc.Callback).Methods(http.MethodGet)
	} else {
		r.HandleFunc("/login", a.handleLoginUnavailable).Methods(http.MethodGet)
	}

	// In development, allow a password-less login so the protected route can be
	// exercised without a live identity provider.
	if a.devMode {
		r.HandleFunc("/dev/login", a.handleDevLogin).Methods(http.MethodPost)
	}

	// Protected route, gated by the pluggable Authenticator seam.
	me := r.NewRoute().Subrouter()
	me.Use(middleware.RequireAuthenticated(SessionAuthenticator{}, a.renderer))
	me.HandleFunc("/me", a.handleMe).Methods(http.MethodGet)

	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		response.NotFound(w, req, a.renderer)
	})

	return r
}

// injectPrincipal exposes the current user (if any) to templates as "principal".
func (a *app) injectPrincipal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := userFromSession(r); u != nil {
			ctx := webctx.WithPrincipal(r.Context(), u)
			m := webctx.TemplateMapFromContext(ctx)
			m["principal"] = u
			ctx = webctx.WithTemplateMap(ctx, m)
			r = r.Clone(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// messageForm is the home page's contact form. The "form" tags drive binding
// (and become the keys of any validation Errors); the "validate" tags drive
// declarative validation via form.Bind.
type messageForm struct {
	Name    string `form:"name"    validate:"required"`
	Email   string `form:"email"   validate:"required,email"`
	Message string `form:"message" validate:"required,min=3"`
}

// homeData builds the template data for the home page. Both the GET handler and
// the POST handler call it so the template always sees a uniform shape: a "Form"
// value of type messageForm and a non-nil "Errors". GET passes the zero form and
// an empty Errors; POST passes the submitted input and any validation errors.
func homeData(r *http.Request, in messageForm, errs form.Errors) webctx.TemplateMap {
	m := webctx.TemplateMapFromContext(r.Context())
	m.Title("Home")
	if errs == nil {
		errs = form.Errors{}
	}
	m["Form"] = in
	m["Errors"] = errs
	return m
}

func (a *app) handleHome(w http.ResponseWriter, r *http.Request) {
	a.renderer.RenderHTML(w, "home", homeData(r, messageForm{}, nil))
}

func (a *app) handleSubmit(w http.ResponseWriter, r *http.Request) {
	var in messageForm
	errs, err := form.Bind(r, &in)
	if err != nil {
		// Unprocessable request (oversized or malformed body): 400.
		response.BadRequest(w, r, a.renderer)
		return
	}

	if errs.Any() {
		// Invalid input: re-render the form with the user's input preserved and
		// the per-field errors shown inline, at 422 Unprocessable Entity — the
		// conventional status for a form that failed validation.
		a.renderer.RenderHTMLStatus(w, http.StatusUnprocessableEntity, "home", homeData(r, in, errs))
		return
	}

	// Valid: surface a one-shot success flash and post/redirect/get home.
	gbsession.Flash(webctx.SessionFromContext(r.Context())).
		Alert("Thanks for your message, %s!", in.Name)
	response.SeeOther(w, r, "/")
}

func (a *app) handleHealth(w http.ResponseWriter, r *http.Request) {
	a.renderer.RenderJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *app) handleMe(w http.ResponseWriter, r *http.Request) {
	m := webctx.TemplateMapFromContext(r.Context())
	m.Title("Profile")
	a.renderer.RenderHTML(w, "me", m)
}

func (a *app) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Clear the whole session (not just the user) at the privilege boundary, then
	// surface a confirmation flash on the fresh session.
	resetSession(r)
	gbsession.Flash(webctx.SessionFromContext(r.Context())).Alert("You have been signed out.")
	response.SeeOther(w, r, "/")
}

func (a *app) handleLoginUnavailable(w http.ResponseWriter, r *http.Request) {
	gbsession.Flash(webctx.SessionFromContext(r.Context())).
		Warning("OIDC sign-in is not configured. Set OIDC_ISSUER to enable it.")
	response.SeeOther(w, r, "/")
}

func (a *app) handleDevLogin(w http.ResponseWriter, r *http.Request) {
	resetSession(r) // fixation defense at the login boundary
	storeUser(r, &User{Subject: "dev|1", Email: "dev@example.com", Name: "Dev User"})
	gbsession.Flash(webctx.SessionFromContext(r.Context())).Alert("Signed in as the dev user.")
	response.SeeOther(w, r, "/me")
}

// cookieEntropy returns an EntropyFunc backed by a secret manager. On first run
// it generates a 64-byte key and persists it; thereafter the same key is read
// back, so sessions survive restarts.
//
// It consumes the framework's secrets.Config: the directory holding the cookie
// key is taken from cfg.SecretsDir (already resolved by the caller), and the
// filesystem secret manager is rooted at the same directory.
func cookieEntropy(ctx context.Context, cfg secrets.Config) (cookiestore.EntropyFunc, error) {
	secretsDir := cfg.SecretsDir
	if secretsDir == "" {
		secretsDir = "./local-secrets"
	}

	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		return nil, err
	}

	const keyName = "cookie-key"
	keyPath := filepath.Join(secretsDir, keyName)
	if _, err := os.Stat(keyPath); errors.Is(err, os.ErrNotExist) {
		raw := make([]byte, 64)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		if err := os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(raw)), 0o600); err != nil {
			return nil, err
		}
	}

	// Reuse the caller's secrets.Config but pin the filesystem root to the
	// resolved directory so the provider reads back the key written above.
	cfg.FilesystemRoot = secretsDir
	sm, err := secrets.NewFilesystem(ctx, &cfg)
	if err != nil {
		return nil, err
	}

	return func() ([][]byte, error) {
		v, err := sm.GetSecretValue(ctx, keyName)
		if err != nil {
			return nil, err
		}
		key, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return nil, fmt.Errorf("failed to decode cookie key: %w", err)
		}
		return [][]byte{key}, nil
	}, nil
}
