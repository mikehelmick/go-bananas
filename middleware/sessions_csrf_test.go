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

package middleware

import (
	"context"
	"encoding/base64"
	"html/template"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	sess "github.com/mikehelmick/go-bananas/session"
	"github.com/mikehelmick/go-bananas/webctx"
)

// decodeMasked decodes a masked CSRF token string back to bytes.
func decodeMasked(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

// setLastActivity stamps the session on ctx with the given last-activity time.
func setLastActivity(ctx context.Context, at time.Time) {
	sess.StoreLastActivity(webctx.SessionFromContext(ctx), at)
}

func TestMaskUnmaskRoundTrip(t *testing.T) {
	t.Parallel()

	raw := make([]byte, TokenLength)
	for i := range raw {
		raw[i] = byte(i)
	}

	masked, err := mask(raw)
	if err != nil {
		t.Fatalf("mask: %v", err)
	}

	// Two masks of the same token differ (random pad) but both unmask correctly.
	masked2, _ := mask(raw)
	if masked == masked2 {
		t.Error("expected masked tokens to differ across calls")
	}

	for _, m := range []string{masked, masked2} {
		decoded, err := decodeMasked(m)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		got, err := unmask(decoded)
		if err != nil {
			t.Fatalf("unmask: %v", err)
		}
		if string(got) != string(raw) {
			t.Fatal("unmasked token does not match original")
		}
	}
}

func TestSessionAndCSRFFlow(t *testing.T) {
	t.Parallel()

	h := testRenderer(t)
	store := testStore()

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			m := webctx.TemplateMapFromContext(r.Context())
			tok, _ := m["csrfToken"].(template.HTML)
			_, _ = io.WriteString(w, string(tok))
			return
		}
		_, _ = io.WriteString(w, "posted")
	})

	handler := RequireSession(store, nil, h)(HandleCSRF(h)(final))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	// GET issues a session cookie and returns a masked CSRF token.
	resp, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", resp.StatusCode)
	}
	token := strings.TrimSpace(string(body))
	if token == "" {
		t.Fatal("expected a CSRF token in the GET response")
	}

	// POST without a token is rejected.
	resp, err = client.PostForm(srv.URL+"/", url.Values{})
	if err != nil {
		t.Fatalf("POST(no token): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST without token status = %d, want 401", resp.StatusCode)
	}

	// POST with the token from the GET succeeds.
	resp, err = client.PostForm(srv.URL+"/", url.Values{CSRFFormField: {token}})
	if err != nil {
		t.Fatalf("POST(token): %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST with token status = %d, want 200 (body=%q)", resp.StatusCode, body)
	}
	if string(body) != "posted" {
		t.Fatalf("POST body = %q, want posted", body)
	}
}

func TestCheckSessionIdleNoAuth(t *testing.T) {
	t.Parallel()

	store := testStore()
	// Build a session and stamp its last activity well in the past.
	session, _ := store.New(httptest.NewRequest(http.MethodGet, "/", nil), "s")
	session.Values = map[any]any{}
	pastReq := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := webctx.WithSession(pastReq.Context(), session)
	setLastActivity(ctx, time.Now().Add(-time.Hour))

	idleFired := false
	onIdle := func(w http.ResponseWriter, _ *http.Request) {
		idleFired = true
		w.WriteHeader(http.StatusSeeOther)
	}

	mw := CheckSessionIdleNoAuth(time.Minute, onIdle)
	w := httptest.NewRecorder()
	mw(okHandler("ok")).ServeHTTP(w, pastReq.WithContext(ctx))

	if !idleFired {
		t.Fatal("expected onIdle to fire for an idle session")
	}
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w.Code)
	}

	// A fresh session does not trip the idle handler.
	fresh, _ := store.New(httptest.NewRequest(http.MethodGet, "/", nil), "s")
	fresh.Values = map[any]any{}
	freshReq := httptest.NewRequest(http.MethodGet, "/", nil)
	fctx := webctx.WithSession(freshReq.Context(), fresh)
	setLastActivity(fctx, time.Now())

	idleFired = false
	w = httptest.NewRecorder()
	mw(okHandler("ok")).ServeHTTP(w, freshReq.WithContext(fctx))
	if idleFired {
		t.Fatal("did not expect onIdle to fire for a fresh session")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}
