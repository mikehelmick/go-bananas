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

package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mikehelmick/go-bananas/logging"
	"github.com/sethvargo/go-envconfig"
)

var csrfInputRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// newTestServer builds the app against a temp secrets dir in dev mode and serves
// it. It returns the server and a cookie-jar client.
func newTestServer(t *testing.T, env ...[2]string) (*httptest.Server, *http.Client) {
	t.Helper()
	t.Setenv("OIDC_ISSUER", "") // disable OIDC for a hermetic test
	_, srv := newTestApp(t, env...)
	jar, _ := cookiejar.New(nil)
	return srv, &http.Client{Jar: jar}
}

// newTestApp builds the app in dev mode against a temp secrets dir, applying
// env on top, and serves it. When OIDC is configured, the client's redirect URL
// is pointed at the served callback once the server's address is known.
func newTestApp(tb testing.TB, env ...[2]string) (*app, *httptest.Server) {
	tb.Helper()

	tb.Setenv("SECRETS_DIR", tb.TempDir())
	tb.Setenv("DEV_MODE", "true")
	for _, kv := range env {
		tb.Setenv(kv[0], kv[1])
	}

	// Process configuration from the environment exactly as the binary does, so
	// the Setenv values above take effect at app-construction time.
	ctx := context.Background()
	var cfg appConfig
	if err := envconfig.Process(ctx, &cfg); err != nil {
		tb.Fatalf("envconfig.Process: %v", err)
	}

	a, err := newApp(ctx, &cfg)
	if err != nil {
		tb.Fatalf("newApp: %v", err)
	}

	srv := httptest.NewServer(a.router())
	tb.Cleanup(srv.Close)
	if a.oidc != nil {
		a.oidc.oauth2.RedirectURL = srv.URL + "/auth/callback"
	}
	return a, srv
}

func get(t *testing.T, c *http.Client, url string) (int, string) {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// getHTML issues a request that announces it accepts HTML, the way a browser
// does. The response helpers negotiate on Accept, so this is what exercises the
// error templates rather than their plain-text fallback.
func getHTML(t *testing.T, c *http.Client, url string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest %s: %v", url, err)
	}
	req.Header.Set("Accept", "text/html")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestHomeRendersWithCSRF(t *testing.T) {
	srv, client := newTestServer(t)

	code, body := get(t, client, srv.URL+"/")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	for _, want := range []string{`<meta name="csrf-token"`, "go-bananas", `name="csrf_token"`, "sha512-"} {
		if !strings.Contains(body, want) {
			t.Errorf("home body missing %q", want)
		}
	}
}

func TestCSRFProtectedForm(t *testing.T) {
	srv, client := newTestServer(t)

	// Obtain a CSRF token from the home page.
	_, body := get(t, client, srv.URL+"/")
	m := csrfInputRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("could not find csrf_token in home page")
	}
	token := m[1]

	// POST without a token is rejected.
	resp, err := client.PostForm(srv.URL+"/submit", url.Values{"message": {"hi there"}})
	if err != nil {
		t.Fatalf("POST(no token): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST without token = %d, want 401", resp.StatusCode)
	}

	// POST with the token and a valid body succeeds; the success flash shows on
	// the redirected home page.
	resp, err = client.PostForm(srv.URL+"/submit", url.Values{
		"csrf_token": {token},
		"name":       {"Ada"},
		"email":      {"ada@example.com"},
		"message":    {"hello bananas"},
	})
	if err != nil {
		t.Fatalf("POST(token): %v", err)
	}
	body, _ = readClose(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST with token final status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "Thanks for your message, Ada!") {
		t.Errorf("expected success flash, got:\n%s", body)
	}

	// The flash is one-shot: a subsequent load no longer shows it.
	_, body2 := get(t, client, srv.URL+"/")
	if strings.Contains(body2, "Thanks for your message, Ada!") {
		t.Error("flash should have cleared after being read once")
	}
}

// csrfToken fetches the home page through client and extracts a CSRF token,
// also establishing the session cookie in the client's jar.
func csrfToken(t *testing.T, c *http.Client, baseURL string) string {
	t.Helper()
	_, body := get(t, c, baseURL+"/")
	m := csrfInputRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("could not find csrf_token in home page")
	}
	return m[1]
}

func TestFormSubmission(t *testing.T) {
	srv, client := newTestServer(t)

	// Obtain a CSRF token (and session cookie) from the home page.
	token := csrfToken(t, client, srv.URL)

	// Invalid submission: a bad email and a too-short message. With a valid CSRF
	// token + session, this is a clean re-render at 422 with the user's valid
	// input preserved and the per-field errors shown.
	resp, err := client.PostForm(srv.URL+"/submit", url.Values{
		"csrf_token": {token},
		"name":       {"Grace Hopper"},
		"email":      {"not-an-email"},
		"message":    {"hi"},
	})
	if err != nil {
		t.Fatalf("POST(invalid): %v", err)
	}
	body, _ := readClose(resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("invalid submit status = %d, want 422", resp.StatusCode)
	}
	if !strings.Contains(body, "must be a valid email address") {
		t.Errorf("expected the email error message, got:\n%s", body)
	}
	if !strings.Contains(body, "is too short") {
		t.Errorf("expected the message length error, got:\n%s", body)
	}
	// Preserved input: the valid fields the user submitted are echoed back.
	if !strings.Contains(body, `value="Grace Hopper"`) {
		t.Errorf("expected the name to be preserved, got:\n%s", body)
	}
	if !strings.Contains(body, `value="not-an-email"`) {
		t.Errorf("expected the email to be preserved, got:\n%s", body)
	}

	// Valid submission: a 303 See Other redirect to "/". Use a client that does
	// not follow redirects so the redirect itself can be asserted.
	noFollow := &http.Client{
		Jar: client.Jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	// A fresh token: the previous request did not rotate it, but re-reading keeps
	// the flow identical to a real browser.
	token = csrfToken(t, client, srv.URL)
	resp, err = noFollow.PostForm(srv.URL+"/submit", url.Values{
		"csrf_token": {token},
		"name":       {"Grace Hopper"},
		"email":      {"grace@example.com"},
		"message":    {"hello there"},
	})
	if err != nil {
		t.Fatalf("POST(valid): %v", err)
	}
	_, _ = readClose(resp)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("valid submit status = %d, want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/" {
		t.Errorf("redirect Location = %q, want \"/\"", loc)
	}
}

func TestHealthJSON(t *testing.T) {
	srv, client := newTestServer(t)

	resp, err := client.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatalf("GET /api/health: %v", err)
	}
	body, _ := readClose(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q", ct)
	}
	if strings.TrimSpace(body) != `{"status":"ok"}` {
		t.Errorf("body = %q", body)
	}
}

// TestSessionCookieAttributes checks the store wiring: dev mode serves plain
// HTTP, so its cookie drops Secure; production cookies are always Secure.
// Both are HttpOnly and SameSite=Lax.
// TestSecureHeadersPinsAllowedHosts checks that, outside dev mode, a request
// with a Host not in ALLOWED_HOSTS is rejected with 400, on matched routes and
// on the 404 handler alike, while dev mode accepts any host.
func TestSecureHeadersPinsAllowedHosts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		devMode string
		host    string
		path    string
		https   bool
		want    int
		wantLoc string
	}{
		{name: "prod_spoofed_host", devMode: "false", host: "evil.example", path: "/healthz", https: true, want: http.StatusBadRequest},
		{name: "prod_spoofed_host_404", devMode: "false", host: "evil.example", path: "/nope", https: true, want: http.StatusBadRequest},
		{name: "prod_spoofed_host_http", devMode: "false", host: "evil.example", path: "/healthz", want: http.StatusBadRequest},
		{name: "prod_allowed_host", devMode: "false", host: "app.example.com", path: "/healthz", https: true, want: http.StatusOK},
		{name: "prod_redirect_pinned", devMode: "false", host: "app.example.com", path: "/healthz", want: http.StatusMovedPermanently, wantLoc: "https://canonical.example.com/healthz"},
		{name: "dev_spoofed_host", devMode: "true", host: "evil.example", path: "/healthz", want: http.StatusOK},
		{name: "dev_spoofed_host_404", devMode: "true", host: "evil.example", path: "/nope", want: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SECRETS_DIR", t.TempDir())
			t.Setenv("DEV_MODE", tc.devMode)
			t.Setenv("OIDC_ISSUER", "")
			t.Setenv("ALLOWED_HOSTS", "app.example.com")
			t.Setenv("SERVER_ENDPOINT", "https://canonical.example.com")

			ctx := context.Background()
			var cfg appConfig
			if err := envconfig.Process(ctx, &cfg); err != nil {
				t.Fatalf("envconfig.Process: %v", err)
			}
			a, err := newApp(ctx, &cfg)
			if err != nil {
				t.Fatalf("newApp: %v", err)
			}

			r := httptest.NewRequest(http.MethodGet, "http://"+tc.host+tc.path, nil)
			if tc.https {
				r.TLS = &tls.ConnectionState{}
			}
			w := httptest.NewRecorder()
			a.router().ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("GET %s with Host %s: status = %d, want %d", tc.path, tc.host, w.Code, tc.want)
			}
			if got := w.Header().Get("Location"); got != tc.wantLoc {
				t.Errorf("Location = %q, want %q", got, tc.wantLoc)
			}
		})
	}
}

// TestHostPinningStartupWarnings checks that, outside dev mode, startup warns
// when SecureHeaders has no host pin, and when SERVER_ENDPOINT has no scheme.
func TestHostPinningStartupWarnings(t *testing.T) {
	const (
		unpinned = "HTTPS redirects are not host-pinned"
		noScheme = "SERVER_ENDPOINT has no scheme"
	)
	for _, tc := range []struct {
		name         string
		devMode      string
		allowedHosts string
		endpoint     string
		want, absent []string
	}{
		{name: "neither", devMode: "false", want: []string{unpinned}, absent: []string{noScheme}},
		{name: "endpoint_no_scheme", devMode: "false", endpoint: "app.example.com", want: []string{unpinned, noScheme}},
		{name: "endpoint_no_scheme_allowed_hosts", devMode: "false", allowedHosts: "app.example.com", endpoint: "app.example.com", want: []string{noScheme}, absent: []string{unpinned}},
		{name: "allowed_hosts", devMode: "false", allowedHosts: "app.example.com", absent: []string{unpinned, noScheme}},
		{name: "endpoint", devMode: "false", endpoint: "https://app.example.com", absent: []string{unpinned, noScheme}},
		{name: "dev_mode", devMode: "true", absent: []string{unpinned, noScheme}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SECRETS_DIR", t.TempDir())
			t.Setenv("DEV_MODE", tc.devMode)
			t.Setenv("OIDC_ISSUER", "")
			t.Setenv("ALLOWED_HOSTS", tc.allowedHosts)
			t.Setenv("SERVER_ENDPOINT", tc.endpoint)

			var buf bytes.Buffer
			ctx := logging.WithLogger(context.Background(), slog.New(slog.NewTextHandler(&buf, nil)))
			var cfg appConfig
			if err := envconfig.Process(ctx, &cfg); err != nil {
				t.Fatalf("envconfig.Process: %v", err)
			}
			if _, err := newApp(ctx, &cfg); err != nil {
				t.Fatalf("newApp: %v", err)
			}

			logs := buf.String()
			for _, w := range tc.want {
				if !strings.Contains(logs, w) {
					t.Errorf("startup log missing %q:\n%s", w, logs)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(logs, a) {
					t.Errorf("startup log unexpectedly contains %q:\n%s", a, logs)
				}
			}
		})
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	for _, tc := range []struct {
		devMode string
		secure  bool
	}{
		{devMode: "true", secure: false},
		{devMode: "false", secure: true},
	} {
		t.Run("dev_mode_"+tc.devMode, func(t *testing.T) {
			t.Setenv("SECRETS_DIR", t.TempDir())
			t.Setenv("DEV_MODE", tc.devMode)
			t.Setenv("OIDC_ISSUER", "")

			ctx := context.Background()
			var cfg appConfig
			if err := envconfig.Process(ctx, &cfg); err != nil {
				t.Fatalf("envconfig.Process: %v", err)
			}
			a, err := newApp(ctx, &cfg)
			if err != nil {
				t.Fatalf("newApp: %v", err)
			}

			r := httptest.NewRequest(http.MethodGet, "/", nil)
			s, err := a.store.New(r, "s")
			if err != nil {
				t.Fatalf("store.New: %v", err)
			}
			w := httptest.NewRecorder()
			if err := s.Save(r, w); err != nil {
				t.Fatalf("Save: %v", err)
			}
			cookies := w.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("got %d cookies, want 1", len(cookies))
			}
			c := cookies[0]
			if !c.HttpOnly {
				t.Error("session cookie is not HttpOnly")
			}
			if c.Secure != tc.secure {
				t.Errorf("Secure = %v, want %v", c.Secure, tc.secure)
			}
			if c.SameSite != http.SameSiteLaxMode {
				t.Errorf("SameSite = %v, want Lax", c.SameSite)
			}
		})
	}
}

func TestProtectedRouteRequiresAuth(t *testing.T) {
	srv, client := newTestServer(t)

	// Anonymous access to the protected route is rejected.
	code, body := get(t, client, srv.URL+"/me")
	if code != http.StatusUnauthorized {
		t.Fatalf("GET /me anonymous = %d, want 401", code)
	}

	// The dev login is itself a CSRF-protected POST, so obtain a token first.
	m := csrfInputRe.FindStringSubmatch(body)
	if m == nil {
		// The 401 page has no form; read the token from the home page instead.
		_, home := get(t, client, srv.URL+"/")
		m = csrfInputRe.FindStringSubmatch(home)
	}
	if m == nil {
		t.Fatal("could not find a csrf token")
	}

	// Sign in via the dev login (available in dev mode), then access succeeds.
	resp, err := client.PostForm(srv.URL+"/dev/login", url.Values{"csrf_token": {m[1]}})
	if err != nil {
		t.Fatalf("POST /dev/login: %v", err)
	}
	body, _ = readClose(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dev login final status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "Dev User") {
		t.Errorf("expected the profile page to show the dev user, got:\n%s", body)
	}
}

// devLogin signs in as the dev user (dev@example.com) and returns the body of
// the page the login redirects to.
func devLogin(t *testing.T, srv *httptest.Server, client *http.Client) string {
	t.Helper()

	_, home := get(t, client, srv.URL+"/")
	m := csrfInputRe.FindStringSubmatch(home)
	if m == nil {
		t.Fatal("could not find a csrf token on the home page")
	}

	resp, err := client.PostForm(srv.URL+"/dev/login", url.Values{"csrf_token": {m[1]}})
	if err != nil {
		t.Fatalf("POST /dev/login: %v", err)
	}
	body, _ := readClose(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dev login final status = %d, want 200", resp.StatusCode)
	}
	return body
}

// TestAdminRouteAuthorization exercises the 401-vs-403 distinction on a single
// route: no credentials is a 401, and a signed-in user who is not on the admin
// allow-list is a 403.
func TestAdminRouteAuthorization(t *testing.T) {
	srv, client := newTestServer(t)

	// Anonymous: authentication fails first, so 401.
	if code, _ := get(t, client, srv.URL+"/admin"); code != http.StatusUnauthorized {
		t.Fatalf("GET /admin anonymous = %d, want 401", code)
	}

	// Signed in, but not an admin (ADMIN_EMAILS is unset): authenticated, not
	// permitted, so 403 with the "403" template.
	devLogin(t, srv, client)
	code, body := getHTML(t, client, srv.URL+"/admin")
	if code != http.StatusForbidden {
		t.Fatalf("GET /admin as non-admin = %d, want 403", code)
	}
	if !strings.Contains(body, "<h1>403</h1>") {
		t.Errorf("expected the 403 template, got:\n%s", body)
	}

	// A client that does not accept HTML gets the negotiated plain-text form.
	code, body = get(t, client, srv.URL+"/admin")
	if code != http.StatusForbidden {
		t.Fatalf("GET /admin (no Accept) = %d, want 403", code)
	}
	if strings.TrimSpace(body) != "Forbidden" {
		t.Errorf("plain-text 403 body = %q, want %q", strings.TrimSpace(body), "Forbidden")
	}

	// The profile route is unaffected: authorization is per-route.
	if code, _ := get(t, client, srv.URL+"/me"); code != http.StatusOK {
		t.Errorf("GET /me as non-admin = %d, want 200", code)
	}
}

// TestAdminRouteAllowsAdmin is the positive half: the same user on the
// allow-list gets through.
func TestAdminRouteAllowsAdmin(t *testing.T) {
	srv, client := newTestServer(t, [2]string{"ADMIN_EMAILS", "dev@example.com"})

	devLogin(t, srv, client)
	code, body := get(t, client, srv.URL+"/admin")
	if code != http.StatusOK {
		t.Fatalf("GET /admin as admin = %d, want 200", code)
	}
	if !strings.Contains(body, "dev@example.com") {
		t.Errorf("expected the admin page to name the user, got:\n%s", body)
	}
}

// TestAdminAllowListIsCaseInsensitive guards the normalization in isAdmin:
// identity providers do not agree on email case.
func TestAdminAllowListIsCaseInsensitive(t *testing.T) {
	srv, client := newTestServer(t, [2]string{"ADMIN_EMAILS", " Dev@Example.COM "})

	devLogin(t, srv, client)
	if code, _ := get(t, client, srv.URL+"/admin"); code != http.StatusOK {
		t.Fatalf("GET /admin with differently-cased allow-list = %d, want 200", code)
	}
}

func readClose(resp *http.Response) (string, error) {
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func TestStaticAssetsServed(t *testing.T) {
	srv, client := newTestServer(t)

	cases := []struct {
		path     string
		wantType string
	}{
		{"/static/css/app.css", "text/css"},
		{"/static/js/app.js", "text/javascript"},
	}

	for _, tc := range cases {
		resp, err := client.Get(srv.URL + tc.path)
		if err != nil {
			t.Fatalf("GET %s: %v", tc.path, err)
		}
		body, _ := readClose(resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200 (the SRI tags point here)", tc.path, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, tc.wantType) {
			t.Errorf("%s content-type = %q, want prefix %q", tc.path, ct, tc.wantType)
		}
		if resp.Header.Get("Cache-Control") == "" {
			t.Errorf("%s missing Cache-Control header", tc.path)
		}
		if len(body) == 0 {
			t.Errorf("%s returned an empty body", tc.path)
		}
	}

	// Directory requests must 404 — never an auto-generated listing — and the
	// non-static embedded trees must not be reachable.
	for _, path := range []string{"/static/", "/static/css/", "/templates/home.html", "/locales/en/default.po"} {
		resp, err := client.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_, _ = readClose(resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, resp.StatusCode)
		}
	}
}

func TestContentSecurityPolicyHeader(t *testing.T) {
	srv, client := newTestServer(t)

	resp, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = readClose(resp)

	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("CSP = %q, want default-src 'self'", csp)
	}
	if !strings.Contains(csp, "'nonce-") || strings.Contains(csp, "{{nonce}}") {
		t.Errorf("CSP = %q, want a substituted per-request nonce", csp)
	}
}

func TestHealthEndpoints(t *testing.T) {
	srv, client := newTestServer(t)

	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := client.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, _ := readClose(resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, resp.StatusCode)
		}
		if !strings.Contains(body, `"status":"ok"`) {
			t.Errorf("%s body = %q", path, body)
		}
	}
}

func TestLocaleSwitching(t *testing.T) {
	srv, client := newTestServer(t)

	// Default English.
	_, body := get(t, client, srv.URL+"/")
	if !strings.Contains(body, "Leave a message") {
		t.Errorf("default page missing English label:\n%s", body)
	}

	// ?lang=es switches to the Spanish translation from locales/es/default.po.
	_, body = get(t, client, srv.URL+"/?lang=es")
	if !strings.Contains(body, "Deja un mensaje") {
		t.Errorf("?lang=es page missing Spanish label:\n%s", body)
	}
}

// fakeIDP is a minimal OpenID Connect provider for hermetic tests: discovery,
// an authorize endpoint that immediately redirects back with a code, a token
// endpoint that issues an RS256 ID token carrying the flow's nonce, and JWKS.
type fakeIDP struct {
	srv    *httptest.Server
	key    *rsa.PrivateKey
	claims map[string]any // extra ID token claims (email, email_verified, ...)

	// tokenHits counts calls to the token endpoint.
	tokenHits atomic.Int64

	mu     sync.Mutex
	nonces map[string]string // code -> nonce
}

const fakeClientID = "test-client"

// fakeIDPKey is the signing key shared by every fake IdP; generating a 2048-bit
// RSA key per test is the slowest part of the setup.
var fakeIDPKey = sync.OnceValue(func() *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(fmt.Sprintf("rsa.GenerateKey: %v", err))
	}
	return key
})

func newFakeIDP(tb testing.TB, claims map[string]any) *fakeIDP {
	tb.Helper()

	idp := &fakeIDP{key: fakeIDPKey(), claims: claims, nonces: map[string]string{}}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", idp.discovery)
	mux.HandleFunc("GET /authorize", idp.authorize)
	mux.HandleFunc("POST /token", idp.token)
	mux.HandleFunc("GET /jwks", idp.jwks)
	idp.srv = httptest.NewServer(mux)
	tb.Cleanup(idp.srv.Close)
	return idp
}

func (idp *fakeIDP) discovery(w http.ResponseWriter, _ *http.Request) {
	u := idp.srv.URL
	writeJSON(w, map[string]any{
		"issuer":                                u,
		"authorization_endpoint":                u + "/authorize",
		"token_endpoint":                        u + "/token",
		"jwks_uri":                              u + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (idp *fakeIDP) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	code := rand.Text()
	idp.mu.Lock()
	idp.nonces[code] = q.Get("nonce")
	idp.mu.Unlock()

	back, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	back.RawQuery = url.Values{"code": {code}, "state": {q.Get("state")}}.Encode()
	http.Redirect(w, r, back.String(), http.StatusFound)
}

func (idp *fakeIDP) token(w http.ResponseWriter, r *http.Request) {
	idp.tokenHits.Add(1)
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	code := r.PostForm.Get("code")
	idp.mu.Lock()
	nonce, ok := idp.nonces[code]
	delete(idp.nonces, code)
	idp.mu.Unlock()
	if !ok {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}

	now := time.Now()
	claims := map[string]any{
		"iss":   idp.srv.URL,
		"sub":   "user-1",
		"aud":   fakeClientID,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
		"nonce": nonce,
		"name":  "Ada Lovelace",
	}
	for k, v := range idp.claims {
		claims[k] = v
	}
	idToken, err := idp.sign(claims)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"access_token": "access-" + rand.Text(),
		"token_type":   "Bearer",
		"expires_in":   3600,
		"id_token":     idToken,
	})
}

func (idp *fakeIDP) jwks(w http.ResponseWriter, _ *http.Request) {
	b64 := base64.RawURLEncoding.EncodeToString
	writeJSON(w, map[string]any{"keys": []map[string]any{{
		"kty": "RSA",
		"kid": "k1",
		"alg": "RS256",
		"use": "sig",
		"n":   b64(idp.key.N.Bytes()),
		"e":   b64(big.NewInt(int64(idp.key.E)).Bytes()),
	}}})
}

// sign returns claims as a compact RS256 JWS.
func (idp *fakeIDP) sign(claims map[string]any) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "k1"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	b64 := base64.RawURLEncoding.EncodeToString
	input := b64(header) + "." + b64(payload)
	sum := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, idp.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return input + "." + b64(sig), nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// newOIDCTestApp serves the app with its OIDC flow pointed at idp.
func newOIDCTestApp(tb testing.TB, idp *fakeIDP, env ...[2]string) (*app, *httptest.Server) {
	tb.Helper()
	oidcEnv := make([][2]string, 0, 3+len(env))
	oidcEnv = append(oidcEnv,
		[2]string{"OIDC_ISSUER", idp.srv.URL},
		[2]string{"OIDC_CLIENT_ID", fakeClientID},
		[2]string{"OIDC_CLIENT_SECRET", "test-secret"},
	)
	return newTestApp(tb, append(oidcEnv, env...)...)
}

// noFollowClient returns a cookie-jar client that does not follow redirects.
func noFollowClient(jar http.CookieJar) *http.Client {
	if jar == nil {
		jar, _ = cookiejar.New(nil)
	}
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// sessionValues decodes the session cookie that client holds for srv.
func sessionValues(tb testing.TB, a *app, srv *httptest.Server, client *http.Client) map[any]any {
	tb.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		tb.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, srv.URL+"/", nil)
	for _, c := range client.Jar.Cookies(u) {
		req.AddCookie(c)
	}
	s, err := a.store.Get(req, "go-bananas-session")
	if err != nil {
		tb.Fatalf("decode session: %v", err)
	}
	return s.Values
}

// flowKeys are the session keys that hold the in-flight OIDC flow state.
var flowKeys = []string{sessionStateKey, sessionNonceKey, sessionVerifierKey}

// redirectTarget GETs target on a non-following client, checks the status, and
// returns the parsed Location.
func redirectTarget(tb testing.TB, client *http.Client, target string, wantCode int) *url.URL {
	tb.Helper()
	resp, err := client.Get(target)
	if err != nil {
		tb.Fatalf("GET %s: %v", target, err)
	}
	_, _ = readClose(resp)
	if resp.StatusCode != wantCode {
		tb.Fatalf("GET %s = %d, want %d", target, resp.StatusCode, wantCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		tb.Fatalf("parse Location: %v", err)
	}
	return loc
}

// startLogin begins the OIDC flow on a non-following client and returns the
// provider authorization URL the app redirected to; its "state" parameter is
// the state the app issued.
func startLogin(tb testing.TB, srv *httptest.Server, client *http.Client) *url.URL {
	tb.Helper()
	return redirectTarget(tb, client, srv.URL+"/login", http.StatusSeeOther)
}

// authorizeAt follows authURL to the fake IdP and returns the callback URL,
// with a real code and the issued state, that the IdP redirects back to.
func authorizeAt(tb testing.TB, client *http.Client, authURL *url.URL) *url.URL {
	tb.Helper()
	return redirectTarget(tb, client, authURL.String(), http.StatusFound)
}

// assertNoFlowKeys fails if the client's session still holds OIDC flow state.
func assertNoFlowKeys(tb testing.TB, a *app, srv *httptest.Server, client *http.Client) {
	tb.Helper()
	vals := sessionValues(tb, a, srv, client)
	for _, k := range flowKeys {
		if _, ok := vals[k]; ok {
			tb.Errorf("after the callback the session still has %q", k)
		}
	}
}

// TestLogoutRequiresPOST guards against CSRF logout: GET /logout is not a route,
// and the POST form is CSRF-protected.
func TestLogoutRequiresPOST(t *testing.T) {
	srv, client := newTestServer(t)

	profile := devLogin(t, srv, client)
	if !strings.Contains(profile, `<form method="post" action="/logout"`) {
		t.Errorf("profile page has no POST logout form:\n%s", profile)
	}

	// GET is 405, rendered through the renderer with the security headers
	// (mux skips r.Use middleware when no route matches).
	noFollow := noFollowClient(client.Jar)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/logout", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/html")
	resp, err := noFollow.Do(req)
	if err != nil {
		t.Fatalf("GET /logout: %v", err)
	}
	body, _ := readClose(resp)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /logout = %d, want 405", resp.StatusCode)
	}
	if !strings.Contains(body, "<h1>405</h1>") {
		t.Errorf("expected the 405 template, got:\n%s", body)
	}
	for _, h := range []string{"X-Frame-Options", "X-Content-Type-Options", "Content-Security-Policy"} {
		if resp.Header.Get(h) == "" {
			t.Errorf("405 response missing %s", h)
		}
	}
	if code, _ := get(t, client, srv.URL+"/me"); code != http.StatusOK {
		t.Fatalf("GET /me after GET /logout = %d, want 200 (session should survive)", code)
	}

	// A POST without the CSRF token is rejected and does not sign out.
	resp, err = noFollow.PostForm(srv.URL+"/logout", nil)
	if err != nil {
		t.Fatalf("POST /logout (no token): %v", err)
	}
	_, _ = readClose(resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST /logout without token = %d, want 401", resp.StatusCode)
	}
	if code, _ := get(t, client, srv.URL+"/me"); code != http.StatusOK {
		t.Fatalf("GET /me after rejected POST /logout = %d, want 200", code)
	}

	// With the token, the user is signed out.
	token := csrfToken(t, client, srv.URL)
	resp, err = client.PostForm(srv.URL+"/logout", url.Values{"csrf_token": {token}})
	if err != nil {
		t.Fatalf("POST /logout: %v", err)
	}
	body, _ = readClose(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /logout final status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "You have been signed out.") {
		t.Errorf("expected the sign-out flash, got:\n%s", body)
	}
	if code, _ := get(t, client, srv.URL+"/me"); code != http.StatusUnauthorized {
		t.Fatalf("GET /me after logout = %d, want 401", code)
	}
}

// TestCallbackFailureClearsFlowState checks that a failed callback removes the
// flow state from the saved session, so the same state, nonce and verifier
// cannot be used again.
func TestCallbackFailureClearsFlowState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		claims map[string]any
		// callback builds the callback URL from the provider authorization URL
		// the app issued.
		callback     func(t *testing.T, client *http.Client, authURL *url.URL) *url.URL
		wantCode     int
		wantLoc      string // checked when non-empty
		wantExchange bool   // whether the token endpoint is called (oauth2 may retry it)
		wantFlash    string // checked on the redirect target when non-empty
	}{
		{
			name: "wrong state",
			callback: func(_ *testing.T, _ *http.Client, authURL *url.URL) *url.URL {
				return callbackURL(authURL, url.Values{"state": {"wrong"}, "code": {"code"}})
			},
			wantCode: http.StatusBadRequest,
		},
		{
			// RFC 6749 §4.1.2.1: the provider reports a denied or failed
			// authorization with an error parameter and no code.
			name: "provider error",
			callback: func(_ *testing.T, _ *http.Client, authURL *url.URL) *url.URL {
				return callbackURL(authURL, url.Values{
					"state": {authURL.Query().Get("state")},
					"error": {"access_denied"},
				})
			},
			wantCode:  http.StatusSeeOther,
			wantLoc:   "/",
			wantFlash: "Sign-in was not completed.",
		},
		{
			name: "exchange failure",
			callback: func(_ *testing.T, _ *http.Client, authURL *url.URL) *url.URL {
				return callbackURL(authURL, url.Values{
					"state": {authURL.Query().Get("state")},
					"code":  {"unknown-code"},
				})
			},
			wantCode:     http.StatusInternalServerError,
			wantExchange: true,
		},
		{
			name:   "nonce mismatch",
			claims: map[string]any{"nonce": "not-the-issued-nonce"},
			callback: func(t *testing.T, client *http.Client, authURL *url.URL) *url.URL {
				return authorizeAt(t, client, authURL)
			},
			wantCode:     http.StatusBadRequest,
			wantExchange: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idp := newFakeIDP(t, tc.claims)
			a, srv := newOIDCTestApp(t, idp)
			client := noFollowClient(nil)

			authURL := startLogin(t, srv, client)
			vals := sessionValues(t, a, srv, client)
			for _, k := range flowKeys {
				if _, ok := vals[k]; !ok {
					t.Fatalf("after /login the session has no %q", k)
				}
			}

			resp, err := client.Get(tc.callback(t, client, authURL).String())
			if err != nil {
				t.Fatalf("GET /auth/callback: %v", err)
			}
			_, _ = readClose(resp)
			if resp.StatusCode != tc.wantCode {
				t.Fatalf("callback = %d, want %d", resp.StatusCode, tc.wantCode)
			}
			if tc.wantLoc != "" {
				if loc := resp.Header.Get("Location"); loc != tc.wantLoc {
					t.Errorf("callback Location = %q, want %q", loc, tc.wantLoc)
				}
			}
			if n := idp.tokenHits.Load(); (n > 0) != tc.wantExchange {
				t.Errorf("token endpoint hit %d times, want exchange = %v", n, tc.wantExchange)
			}
			assertNoFlowKeys(t, a, srv, client)
			if tc.wantFlash != "" {
				if _, body := get(t, client, srv.URL+tc.wantLoc); !strings.Contains(body, tc.wantFlash) {
					t.Errorf("expected flash %q, got:\n%s", tc.wantFlash, body)
				}
			}
		})
	}
}

// callbackURL returns the app's callback URL (the redirect_uri in authURL) with
// query q.
func callbackURL(authURL *url.URL, q url.Values) *url.URL {
	u, _ := url.Parse(authURL.Query().Get("redirect_uri"))
	u.RawQuery = q.Encode()
	return u
}

// TestCallbackReplayRejected replays a callback that already succeeded: the
// flow state is gone, so it is a 400, and the signed-in user is unaffected.
func TestCallbackReplayRejected(t *testing.T) {
	idp := newFakeIDP(t, map[string]any{"email": "ada@example.com"})
	a, srv := newOIDCTestApp(t, idp)
	client := noFollowClient(nil)

	callback := authorizeAt(t, client, startLogin(t, srv, client))
	if loc := redirectTarget(t, client, callback.String(), http.StatusSeeOther); loc.Path != "/me" {
		t.Fatalf("callback redirected to %q, want /me", loc)
	}

	resp, err := client.Get(callback.String())
	if err != nil {
		t.Fatalf("replay GET /auth/callback: %v", err)
	}
	_, _ = readClose(resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("replayed callback = %d, want 400", resp.StatusCode)
	}
	if n := idp.tokenHits.Load(); n != 1 {
		t.Errorf("token endpoint hit %d times, want 1", n)
	}
	assertNoFlowKeys(t, a, srv, client)
	if code, _ := get(t, client, srv.URL+"/me"); code != http.StatusOK {
		t.Errorf("GET /me after replay = %d, want 200 (user should stay signed in)", code)
	}
}

// TestLoginRotatesCSRFToken checks that the session reset at sign-in discards
// the pre-login CSRF token, so a token captured before login is useless after.
func TestLoginRotatesCSRFToken(t *testing.T) {
	idp := newFakeIDP(t, map[string]any{"email": "ada@example.com"})
	_, srv := newOIDCTestApp(t, idp)
	client := noFollowClient(nil)

	oldToken := csrfToken(t, client, srv.URL)
	callback := authorizeAt(t, client, startLogin(t, srv, client))
	redirectTarget(t, client, callback.String(), http.StatusSeeOther)

	form := url.Values{
		"name":    {"Ada"},
		"email":   {"ada@example.com"},
		"message": {"hello bananas"},
	}
	form.Set("csrf_token", oldToken)
	resp, err := client.PostForm(srv.URL+"/submit", form)
	if err != nil {
		t.Fatalf("POST /submit (old token): %v", err)
	}
	_, _ = readClose(resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST with the pre-login token = %d, want 401", resp.StatusCode)
	}

	form.Set("csrf_token", csrfToken(t, client, srv.URL))
	resp, err = client.PostForm(srv.URL+"/submit", form)
	if err != nil {
		t.Fatalf("POST /submit (new token): %v", err)
	}
	_, _ = readClose(resp)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST with the post-login token = %d, want 303", resp.StatusCode)
	}
}

// TestAdminRequiresVerifiedEmail checks that an allow-listed email grants admin
// only when the provider asserts email_verified as the JSON boolean true.
func TestAdminRequiresVerifiedEmail(t *testing.T) {
	const email = "ada@example.com"
	for _, tc := range []struct {
		name     string
		verified any // the email_verified claim; nil omits it
		want     int
	}{
		{name: "verified", verified: true, want: http.StatusOK},
		{name: "unverified", verified: false, want: http.StatusForbidden},
		{name: "missing", verified: nil, want: http.StatusForbidden},
		{name: "string", verified: "true", want: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := map[string]any{"email": email}
			if tc.verified != nil {
				claims["email_verified"] = tc.verified
			}
			idp := newFakeIDP(t, claims)
			_, srv := newOIDCTestApp(t, idp, [2]string{"ADMIN_EMAILS", email})
			jar, _ := cookiejar.New(nil)
			client := &http.Client{Jar: jar}

			// Follow the whole flow: /login -> IdP -> /auth/callback -> /me.
			code, body := get(t, client, srv.URL+"/login")
			if code != http.StatusOK || !strings.Contains(body, email) {
				t.Fatalf("sign-in ended with %d, want 200 on the profile page:\n%s", code, body)
			}
			if code, _ := get(t, client, srv.URL+"/admin"); code != tc.want {
				t.Errorf("GET /admin = %d, want %d", code, tc.want)
			}
		})
	}
}
