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
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/sethvargo/go-envconfig"
)

var csrfInputRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// newTestServer builds the app against a temp secrets dir in dev mode and serves
// it. It returns the server and a cookie-jar client.
func newTestServer(t *testing.T) (*httptest.Server, *http.Client) {
	t.Helper()

	t.Setenv("SECRETS_DIR", t.TempDir())
	t.Setenv("DEV_MODE", "true")
	t.Setenv("OIDC_ISSUER", "") // disable OIDC for a hermetic test

	// Process configuration from the environment exactly as the binary does, so
	// the t.Setenv values above take effect at app-construction time.
	ctx := context.Background()
	var cfg appConfig
	if err := envconfig.Process(ctx, &cfg); err != nil {
		t.Fatalf("envconfig.Process: %v", err)
	}

	a, err := newApp(ctx, &cfg)
	if err != nil {
		t.Fatalf("newApp: %v", err)
	}

	srv := httptest.NewServer(a.router())
	t.Cleanup(srv.Close)

	jar, _ := cookiejar.New(nil)
	return srv, &http.Client{Jar: jar}
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
