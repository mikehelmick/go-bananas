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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mikehelmick/go-bananas/webctx"
)

func TestConfigureStaticAssets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		devMode     bool
		wantControl string
	}{
		{"dev_no_cache", true, "private, no-cache, max-age=0"},
		{"prod_cacheable", false, "public, max-age=604800"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/static/css/app.css", nil)
			ConfigureStaticAssets(tc.devMode)(okHandler("body{}")).ServeHTTP(w, req)

			if got := w.Header().Get("Cache-Control"); got != tc.wantControl {
				t.Errorf("Cache-Control = %q, want %q", got, tc.wantControl)
			}
			if w.Header().Get("Expires") == "" {
				t.Error("expected an Expires header")
			}
			if got := w.Header().Get("Vary"); got != "Accept-Encoding" {
				t.Errorf("Vary = %q, want Accept-Encoding", got)
			}
			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want 200 (must pass through)", w.Code)
			}
		})
	}
}

func TestContentSecurityPolicy(t *testing.T) {
	t.Parallel()

	t.Run("static_policy", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		ContentSecurityPolicy("default-src 'self'")(okHandler("ok")).ServeHTTP(w, req)

		if got := w.Header().Get("Content-Security-Policy"); got != "default-src 'self'" {
			t.Errorf("CSP = %q", got)
		}
	})

	t.Run("nonce_substituted", func(t *testing.T) {
		t.Parallel()

		policy := "default-src 'self'; script-src 'self' 'nonce-{{nonce}}'"
		handler := ProcessNonce()(ContentSecurityPolicy(policy)(http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				// The template-visible nonce must match the header nonce.
				nonce := webctx.NonceFromContext(r.Context())
				csp := w.Header().Get("Content-Security-Policy")
				if nonce == "" {
					t.Error("expected a nonce on the context")
				}
				if !strings.Contains(csp, "'nonce-"+nonce+"'") {
					t.Errorf("CSP %q does not contain the context nonce %q", csp, nonce)
				}
				w.WriteHeader(http.StatusOK)
			})))

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if strings.Contains(w.Header().Get("Content-Security-Policy"), CSPNoncePlaceholder) {
			t.Error("placeholder must not survive substitution")
		}
	})

	t.Run("missing_nonce_drops_source", func(t *testing.T) {
		t.Parallel()

		policy := "script-src 'self' 'nonce-{{nonce}}'"
		// No ProcessNonce installed.
		w := httptest.NewRecorder()
		ContentSecurityPolicy(policy)(okHandler("ok")).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

		csp := w.Header().Get("Content-Security-Policy")
		if strings.Contains(csp, "nonce") {
			t.Errorf("CSP %q must not contain an empty/guessable nonce source", csp)
		}
		if !strings.Contains(csp, "'self'") {
			t.Errorf("CSP %q lost unrelated sources", csp)
		}
	})
}

func TestPopulateTraceIDValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		header string
		want   string
	}{
		{"gcp_hex", "105445aa7843bc8bf206b12000100000/1;o=1", "105445aa7843bc8bf206b12000100000"},
		{"uuid", "1d09592c-3914-4ff9-83cd-29659da1d4ac", "1d09592c-3914-4ff9-83cd-29659da1d4ac"},
		{"space_rejected", "abc def/1", ""},
		{"injection_rejected", "abc\tdef", ""},
		{"too_long_rejected", strings.Repeat("a", 129), ""},
		{"empty", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got string
			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = webctx.TraceIDFromContext(r.Context())
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.header != "" {
				req.Header.Set(TraceHeader, tc.header)
			}
			PopulateTraceID()(next).ServeHTTP(httptest.NewRecorder(), req)

			if got != tc.want {
				t.Errorf("trace ID = %q, want %q", got, tc.want)
			}
		})
	}
}
