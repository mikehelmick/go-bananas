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

package response

import (
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mikehelmick/go-bananas/render"
)

func testRenderer(t *testing.T) *render.Renderer {
	t.Helper()
	fsys := fs.FS(fstest.MapFS{
		"401.html": &fstest.MapFile{Data: []byte(`{{define "401"}}unauthorized page{{end}}`)},
		"404.html": &fstest.MapFile{Data: []byte(`{{define "404"}}not found page{{end}}`)},
		"500.html": &fstest.MapFile{Data: []byte(`{{define "500"}}error page{{end}}`)},
		"400.html": &fstest.MapFile{Data: []byte(`{{define "400"}}bad request page{{end}}`)},
	})
	r, err := render.New(fsys, render.WithLogger(nil))
	if err != nil {
		t.Fatalf("render.New: %v", err)
	}
	return r
}

func TestErrorHelpers_ContentNegotiation(t *testing.T) {
	t.Parallel()

	h := testRenderer(t)

	cases := []struct {
		name     string
		fn       func(http.ResponseWriter, *http.Request, *render.Renderer)
		accept   string
		wantCode int
		wantBody string
		wantCT   string
	}{
		{"unauthorized_html", Unauthorized, "text/html", http.StatusUnauthorized, "unauthorized page", "text/html"},
		{"unauthorized_json", Unauthorized, "application/json", http.StatusUnauthorized, `{"error":"unauthorized"}`, "application/json"},
		{"unauthorized_text", Unauthorized, "text/plain", http.StatusUnauthorized, "Unauthorized", ""},
		{"notfound_html", NotFound, "text/html", http.StatusNotFound, "not found page", "text/html"},
		{"notfound_json", NotFound, "application/json", http.StatusNotFound, `{"error":"not found"}`, "application/json"},
		{"badrequest_html", BadRequest, "text/html", http.StatusBadRequest, "bad request page", "text/html"},
		{"badrequest_json", BadRequest, "application/json", http.StatusBadRequest, `{"error":"bad request"}`, "application/json"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Accept", tc.accept)
			w := httptest.NewRecorder()

			tc.fn(w, req, h)

			if w.Code != tc.wantCode {
				t.Errorf("status = %d, want %d", w.Code, tc.wantCode)
			}
			if !strings.Contains(strings.TrimSpace(w.Body.String()), tc.wantBody) {
				t.Errorf("body = %q, want to contain %q", w.Body.String(), tc.wantBody)
			}
			if tc.wantCT != "" && !strings.HasPrefix(w.Header().Get("Content-Type"), tc.wantCT) {
				t.Errorf("content-type = %q, want prefix %q", w.Header().Get("Content-Type"), tc.wantCT)
			}
		})
	}
}

func TestInternalError(t *testing.T) {
	t.Parallel()

	h := testRenderer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	InternalError(w, req, h, errors.New("kaboom"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestBack(t *testing.T) {
	t.Parallel()

	h := testRenderer(t)

	cases := []struct {
		name     string
		referer  string
		host     string
		wantDest string
	}{
		{"empty", "", "example.com", "/"},
		{"same_host", "https://example.com/page", "example.com", "https://example.com/page"},
		{"other_host", "https://evil.com/page", "example.com", "/"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = tc.host
			if tc.referer != "" {
				req.Header.Set("Referer", tc.referer)
			}
			w := httptest.NewRecorder()

			Back(w, req, h)

			if w.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want 303", w.Code)
			}
			if got := w.Header().Get("Location"); got != tc.wantDest {
				t.Errorf("Location = %q, want %q", got, tc.wantDest)
			}
		})
	}
}

func TestRealHostFromRequest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		host string
		want string
	}{
		{"localhost", "localhost", "http://localhost"},
		{"remote", "example.com", "https://example.com"},
		{"remote_default_port", "example.com:443", "https://example.com"},
		{"remote_custom_port", "example.com:8443", "https://example.com:8443"},
		{"localhost_custom_port", "localhost:3000", "http://localhost:3000"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/path", nil)
			req.Host = tc.host
			if got := RealHostFromRequest(req); got != tc.want {
				t.Errorf("RealHostFromRequest = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsJSONContentType(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"application/json":                true,
		"application/json; charset=utf-8": true,
		"text/html":                       false,
		"":                                false,
	}
	for ct, want := range cases {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		if got := IsJSONContentType(req); got != want {
			t.Errorf("IsJSONContentType(%q) = %v, want %v", ct, got, want)
		}
	}
}
