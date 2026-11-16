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
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// serveSecure sends r through SecureHeaders(false, ServerTypeHTML, opts...)
// wrapping a 200 handler and returns the recorder.
func serveSecure(t testing.TB, r *http.Request, opts ...SecureOption) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	SecureHeaders(false, ServerTypeHTML, opts...)(okHandler("ok")).ServeHTTP(w, r)
	return w
}

// locationHost returns the host of the Location header, or "" if there is none.
func locationHost(t testing.TB, w *httptest.ResponseRecorder) string {
	t.Helper()
	loc := w.Header().Get("Location")
	if loc == "" {
		return ""
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location %q: %v", loc, err)
	}
	return u.Host
}

func TestSecureHeadersIgnoresForwardedHost(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest(http.MethodGet, "http://victim/login", nil)
	r.Header.Set("X-Forwarded-Host", "evil.example")
	w := serveSecure(t, r)

	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301", w.Code)
	}
	if got := locationHost(t, w); got != "victim" {
		t.Errorf("Location host = %q, want victim", got)
	}
}

func TestSecureHeadersTrustedHost(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest(http.MethodGet, "http://victim/login", nil)
	r.Header.Set("X-Forwarded-Host", "evil.example")
	w := serveSecure(t, r, WithTrustedHostHeaders("X-Forwarded-Host"))

	if got := locationHost(t, w); got != "evil.example" {
		t.Errorf("Location host = %q, want evil.example", got)
	}
}

func TestSecureHeadersProxyProtoNoLoop(t *testing.T) {
	t.Parallel()

	for _, xfh := range []string{"", "other.example"} {
		t.Run("xfh="+xfh, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequest(http.MethodGet, "http://victim/login", nil)
			r.Header.Set("X-Forwarded-Proto", "https")
			if xfh != "" {
				r.Header.Set("X-Forwarded-Host", xfh)
			}
			w := serveSecure(t, r)

			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want 200 (Location %q)", w.Code, w.Header().Get("Location"))
			}
		})
	}
}

func TestSecureHeadersHSTSPreload(t *testing.T) {
	t.Parallel()

	newReq := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "http://victim/", nil)
		r.Header.Set("X-Forwarded-Proto", "https")
		return r
	}

	sts := serveSecure(t, newReq()).Header().Get("Strict-Transport-Security")
	if sts == "" {
		t.Fatal("Strict-Transport-Security missing")
	}
	if strings.Contains(sts, "preload") {
		t.Errorf("default STS = %q, want no preload", sts)
	}
	if !strings.Contains(sts, "includeSubDomains") || !strings.Contains(sts, "max-age=315360000") {
		t.Errorf("default STS = %q, want includeSubDomains and max-age kept", sts)
	}

	sts = serveSecure(t, newReq(), WithHSTSPreload()).Header().Get("Strict-Transport-Security")
	if !strings.Contains(sts, "preload") {
		t.Errorf("WithHSTSPreload STS = %q, want preload", sts)
	}
}

func TestSecureHeadersWarnsUnpinnedHostHeaders(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		opts []SecureOption
		warn bool
	}{
		{name: "defaults"},
		{name: "trusted_only", opts: []SecureOption{WithTrustedHostHeaders("X-Forwarded-Host")}, warn: true},
		{name: "trusted_with_allowlist", opts: []SecureOption{WithTrustedHostHeaders("X-Forwarded-Host"), WithAllowedHosts("victim")}},
		{name: "trusted_with_ssl_host", opts: []SecureOption{WithTrustedHostHeaders("X-Forwarded-Host"), WithSSLHost("victim")}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var cfg secureConfig
			for _, opt := range tc.opts {
				opt(&cfg)
			}
			var buf bytes.Buffer
			warnUnpinnedHostHeaders(slog.New(slog.NewTextHandler(&buf, nil)), cfg.opts)

			if got := strings.Count(buf.String(), "trusted host headers set without"); got != map[bool]int{true: 1}[tc.warn] {
				t.Errorf("warnings = %d, want warn=%v; log:\n%s", got, tc.warn, buf.String())
			}
		})
	}
}

func TestSecureHeadersAllowedHosts(t *testing.T) {
	t.Parallel()

	t.Run("rejects_unlisted_host", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequest(http.MethodGet, "http://evil.example/login", nil)
		w := serveSecure(t, r, WithAllowedHosts("victim"))

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
		if got := locationHost(t, w); got == "evil.example" {
			t.Errorf("redirected to %q", got)
		}
	})

	t.Run("allows_listed_host", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequest(http.MethodGet, "http://victim/login", nil)
		w := serveSecure(t, r, WithAllowedHosts("victim"))

		if got := locationHost(t, w); got != "victim" {
			t.Errorf("Location host = %q, want victim (status %d)", got, w.Code)
		}
	})

	t.Run("trusted_header_checked_against_allowlist", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequest(http.MethodGet, "http://victim/login", nil)
		r.Header.Set("X-Forwarded-Host", "evil.example")
		w := serveSecure(t, r, WithTrustedHostHeaders("X-Forwarded-Host"), WithAllowedHosts("victim"))

		if got := locationHost(t, w); got == "evil.example" {
			t.Errorf("redirected to %q", got)
		}
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})

	t.Run("ssl_host", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequest(http.MethodGet, "http://evil.example/login", nil)
		w := serveSecure(t, r, WithSSLHost("victim"))

		if got := locationHost(t, w); got != "victim" {
			t.Errorf("Location host = %q, want victim", got)
		}
	})
}
