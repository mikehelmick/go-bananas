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
	"net/url"
	"strings"
	"testing"

	"github.com/mikehelmick/go-bananas/webctx"
)

func TestRecovery(t *testing.T) {
	t.Parallel()

	h := testRenderer(t)
	panicky := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()

	Recovery(h)(panicky).ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestPopulateRequestID(t *testing.T) {
	t.Parallel()

	h := testRenderer(t)

	var seen string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = webctx.RequestIDFromContext(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	PopulateRequestID(h)(next).ServeHTTP(httptest.NewRecorder(), req)
	if seen == "" {
		t.Fatal("expected a request ID to be populated")
	}

	// An existing request ID is preserved.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(webctx.WithRequestID(req.Context(), "fixed-id"))
	PopulateRequestID(h)(next).ServeHTTP(httptest.NewRecorder(), req)
	if seen != "fixed-id" {
		t.Fatalf("request ID = %q, want fixed-id", seen)
	}
}

func TestPopulateTraceID(t *testing.T) {
	t.Parallel()

	var seen string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = webctx.TraceIDFromContext(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(TraceHeader, "abc123/spanid;o=1")
	PopulateTraceID()(next).ServeHTTP(httptest.NewRecorder(), req)
	if seen != "abc123" {
		t.Fatalf("trace ID = %q, want abc123", seen)
	}
}

func TestRequireHeader(t *testing.T) {
	t.Parallel()

	h := testRenderer(t)
	mw := RequireHeader("X-Required", h)

	// Missing header → 401.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	mw(okHandler("ok")).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing header status = %d, want 401", w.Code)
	}

	// Present header → 200.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Required", "yes")
	w = httptest.NewRecorder()
	mw(okHandler("ok")).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("present header status = %d, want 200", w.Code)
	}
}

func TestRequireHostHeader(t *testing.T) {
	t.Parallel()

	h := testRenderer(t)
	mw := RequireHostHeader([]string{"example.com"}, h, true)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "example.com:8080"
	w := httptest.NewRecorder()
	mw(okHandler("ok")).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("allowed host status = %d, want 200", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "evil.com"
	w = httptest.NewRecorder()
	mw(okHandler("ok")).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("disallowed host status = %d, want 401", w.Code)
	}
}

func TestMutateMethod(t *testing.T) {
	t.Parallel()

	var method string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		method = r.Method
	})

	form := url.Values{"_method": {"delete"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	MutateMethod()(next).ServeHTTP(httptest.NewRecorder(), req)

	if method != http.MethodDelete {
		t.Fatalf("method = %q, want DELETE", method)
	}
}

func TestInjectCurrentPath(t *testing.T) {
	t.Parallel()

	var p *Path
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		m := webctx.TemplateMapFromContext(r.Context())
		p, _ = m["currentPath"].(*Path)
	})

	req := httptest.NewRequest(http.MethodGet, "/users/edit", nil)
	InjectCurrentPath()(next).ServeHTTP(httptest.NewRecorder(), req)

	if p == nil {
		t.Fatal("expected currentPath on the template map")
	}
	if !p.IsPath("/users/edit") {
		t.Errorf("IsPath failed for %q", p.String())
	}
	if !p.IsDir("/users") {
		t.Error("IsDir(/users) should be true")
	}
	if !p.IsFile("edit") {
		t.Error("IsFile(edit) should be true")
	}
}

func TestProcessDebug(t *testing.T) {
	t.Parallel()

	mw := ProcessDebug("build-1", "tag-1")

	// Without the debug header, no debug headers are set.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	mw(okHandler("ok")).ServeHTTP(w, req)
	if w.Header().Get(HeaderDebugBuildID) != "" {
		t.Error("did not expect build id header without debug header")
	}

	// With the debug header, they are.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderDebug, "1")
	w = httptest.NewRecorder()
	mw(okHandler("ok")).ServeHTTP(w, req)
	if got := w.Header().Get(HeaderDebugBuildID); got != "build-1" {
		t.Errorf("build id header = %q, want build-1", got)
	}
	if got := w.Header().Get(HeaderDebugBuildTag); got != "tag-1" {
		t.Errorf("build tag header = %q, want tag-1", got)
	}
}

func TestAddOperatingSystemFromUserAgent(t *testing.T) {
	t.Parallel()

	cases := map[string]webctx.OS{
		"Mozilla/5.0 (iPhone; CPU iPhone OS 15)": webctx.OSIOS,
		"Dalvik/2.1.0 (Linux; Android 12)":       webctx.OSAndroid,
		"curl/8.0":                               webctx.OSUnknown,
	}

	for ua, want := range cases {
		var got webctx.OS
		next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			got = webctx.OperatingSystemFromContext(r.Context())
		})
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("User-Agent", ua)
		AddOperatingSystemFromUserAgent()(next).ServeHTTP(httptest.NewRecorder(), req)
		if got != want {
			t.Errorf("UA %q → %v, want %v", ua, got, want)
		}
	}
}

func TestOnlyIfEnabled(t *testing.T) {
	t.Parallel()

	h := testRenderer(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	OnlyIfEnabled(false, h)(okHandler("ok")).ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("disabled status = %d, want 404", w.Code)
	}

	w = httptest.NewRecorder()
	OnlyIfEnabled(true, h)(okHandler("ok")).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("enabled status = %d, want 200", w.Code)
	}
}
