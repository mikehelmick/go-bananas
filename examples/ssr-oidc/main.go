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
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/leonelquinteros/gotext"
	"github.com/mikehelmick/go-bananas/cookiestore"
	"github.com/mikehelmick/go-bananas/logging"
	"github.com/mikehelmick/go-bananas/middleware"
	"github.com/mikehelmick/go-bananas/render"
	"github.com/mikehelmick/go-bananas/response"
	"github.com/mikehelmick/go-bananas/secrets"
	"github.com/mikehelmick/go-bananas/server"
	gbsession "github.com/mikehelmick/go-bananas/session"
	"github.com/mikehelmick/go-bananas/webctx"
)

//go:embed templates static
var assets embed.FS

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := realMain(ctx); err != nil {
		logging.DefaultLogger().Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

func realMain(ctx context.Context) error {
	logger := logging.NewLogger(logging.LevelFromString(env("LOG_LEVEL", "info")), envBool("DEV_MODE", true))
	ctx = logging.WithLogger(ctx, logger)

	app, err := newApp(ctx)
	if err != nil {
		return err
	}

	srv, err := server.New(env("PORT", "8080"))
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
	devMode  bool
	oidc     *OIDC // nil when OIDC is not configured
}

func newApp(ctx context.Context) (*app, error) {
	devMode := envBool("DEV_MODE", true)

	renderer, err := render.New(assets,
		render.WithDevMode(devMode),
		render.WithBuildID(env("BUILD_ID", "dev")),
		render.WithLogger(logging.FromContext(ctx)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create renderer: %w", err)
	}

	// Source the secure-cookie keys from a secret manager, demonstrating the
	// infra and web layers composing. Any registered secrets provider works; the
	// filesystem provider keeps the example self-contained.
	entropy, err := cookieEntropy(ctx, env("SECRETS_DIR", "./local-secrets"))
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

	a := &app{renderer: renderer, store: store, devMode: devMode}

	if issuer := env("OIDC_ISSUER", ""); issuer != "" {
		oidc, err := NewOIDC(ctx, renderer, issuer,
			env("OIDC_CLIENT_ID", ""), env("OIDC_CLIENT_SECRET", ""), env("OIDC_REDIRECT_URL", ""))
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
	r.Use(middleware.SecureHeaders(a.devMode, middleware.ServerTypeHTML))
	r.Use(middleware.GzipResponse())
	r.Use(middleware.RequireSession(a.store, nil, a.renderer))
	r.Use(middleware.HandleCSRF(a.renderer))
	r.Use(middleware.PopulateTemplateVariables(middleware.TemplateConfig{
		ServerName: "go-bananas",
		BuildID:    env("BUILD_ID", "dev"),
		DevMode:    a.devMode,
	}))
	r.Use(middleware.InjectCurrentPath())
	r.Use(middleware.ProcessLocale(localeProvider{}))
	r.Use(a.injectPrincipal)

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

func (a *app) handleHome(w http.ResponseWriter, r *http.Request) {
	m := webctx.TemplateMapFromContext(r.Context())
	m.Title("Home")
	a.renderer.RenderHTML(w, "home", m)
}

func (a *app) handleSubmit(w http.ResponseWriter, r *http.Request) {
	session := webctx.SessionFromContext(r.Context())
	flash := gbsession.Flash(session)

	msg := strings.TrimSpace(r.FormValue("message"))
	if msg == "" {
		flash.Error("Message cannot be empty.")
	} else {
		flash.Alert("Thanks for your message: %q", msg)
	}

	// Post/redirect/get: the flash survives the redirect and shows once.
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

// localeProvider is a trivial LocaleProvider that derives the language from the
// request hints and returns an empty translator (so tDefault falls back to its
// default strings).
type localeProvider struct{}

func (localeProvider) Lookup(hints ...string) (gotext.Translator, string) {
	lang := "en"
	for _, h := range hints {
		if h != "" {
			lang = h
			break
		}
	}
	return gotext.NewPo(), lang
}

// cookieEntropy returns an EntropyFunc backed by a secret manager. On first run
// it generates a 64-byte key and persists it; thereafter the same key is read
// back, so sessions survive restarts.
func cookieEntropy(ctx context.Context, secretsDir string) (cookiestore.EntropyFunc, error) {
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

	sm, err := secrets.NewFilesystem(ctx, &secrets.Config{FilesystemRoot: secretsDir})
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

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v == "1" || strings.EqualFold(v, "true")
}
