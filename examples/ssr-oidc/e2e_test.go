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
)

var csrfInputRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// newTestServer builds the app against a temp secrets dir in dev mode and serves
// it. It returns the server and a cookie-jar client.
func newTestServer(t *testing.T) (*httptest.Server, *http.Client) {
	t.Helper()

	t.Setenv("SECRETS_DIR", t.TempDir())
	t.Setenv("DEV_MODE", "true")
	t.Setenv("OIDC_ISSUER", "") // disable OIDC for a hermetic test

	a, err := newApp(context.Background())
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
	resp, err := client.PostForm(srv.URL+"/submit", url.Values{"message": {"hi"}})
	if err != nil {
		t.Fatalf("POST(no token): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST without token = %d, want 401", resp.StatusCode)
	}

	// POST with the token succeeds and the flash shows on the redirected page.
	resp, err = client.PostForm(srv.URL+"/submit", url.Values{
		"csrf_token": {token},
		"message":    {"hello bananas"},
	})
	if err != nil {
		t.Fatalf("POST(token): %v", err)
	}
	body, _ = readClose(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST with token final status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "hello bananas") {
		t.Errorf("expected flash with the message, got:\n%s", body)
	}

	// The flash is one-shot: a subsequent load no longer shows it.
	_, body2 := get(t, client, srv.URL+"/")
	if strings.Contains(body2, "hello bananas") {
		t.Error("flash should have cleared after being read once")
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
