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

package render

import (
	"errors"
	"fmt"
	htmltemplate "html/template"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// testFS returns an in-memory FS with a home template and one css/js asset. The
// css content is parameterized so tests can build distinct FSes.
func testFS(css string) fs.FS {
	return fstest.MapFS{
		"home.html": &fstest.MapFile{Data: []byte(
			`{{define "home"}}<!doctype html><title>{{.title}}</title>{{cssIncludeTag}}{{jsIncludeTag}}<p>{{toUpper "hi"}}</p>{{end}}`)},
		"static/css/app.css": &fstest.MapFile{Data: []byte(css)},
		"static/js/app.js":   &fstest.MapFile{Data: []byte(`console.log("hi");`)},
	}
}

func mustNew(t *testing.T, fsys fs.FS, opts ...Option) *Renderer {
	t.Helper()
	r, err := New(fsys, append([]Option{WithLogger(nil)}, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

func TestRenderHTML(t *testing.T) {
	t.Parallel()

	r := mustNew(t, testFS("body{color:red}"), WithBuildID("v1"))
	w := httptest.NewRecorder()
	r.RenderHTML(w, "home", map[string]any{"title": "Home"})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"<title>Home</title>", "<p>HI</p>", `integrity="sha512-`, "?v1"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\n%s", want, body)
		}
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q", ct)
	}
}

func TestRenderHTMLStatus_DisallowedCode(t *testing.T) {
	t.Parallel()

	r := mustNew(t, testFS("body{}"))
	w := httptest.NewRecorder()
	r.RenderHTMLStatus(w, http.StatusTeapot, "home", nil)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if !strings.Contains(w.Body.String(), "not a registered response code") {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestRenderJSON(t *testing.T) {
	t.Parallel()

	r := mustNew(t, testFS("body{}"))

	cases := []struct {
		name string
		code int
		data any
		want string
	}{
		{"nil_ok", http.StatusOK, nil, `{"ok":true}`},
		{"nil_error", http.StatusNotFound, nil, `{"error":"Not Found"}`},
		{"single_error", http.StatusBadRequest, errors.New("boom"), `{"error":"boom"}`},
		{"joined_errors", http.StatusBadRequest, errors.Join(errors.New("a"), errors.New("b")), `{"errors":["a","b"]}`},
		{"data", http.StatusOK, map[string]string{"k": "v"}, `{"k":"v"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			r.RenderJSON(w, tc.code, tc.data)
			if w.Code != tc.code {
				t.Errorf("status = %d, want %d", w.Code, tc.code)
			}
			if got := strings.TrimSpace(w.Body.String()); got != tc.want {
				t.Errorf("body = %q, want %q", got, tc.want)
			}
			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("content-type = %q", ct)
			}
		})
	}
}

// TestPerRendererAssetCacheIsolation guards the fix for the former package-global
// asset cache: two renderers over different filesystems must produce different
// asset tags, never leaking one renderer's cache into the other.
func TestPerRendererAssetCacheIsolation(t *testing.T) {
	t.Parallel()

	r1 := mustNew(t, testFS("body{color:red}"))
	r2 := mustNew(t, testFS("body{color:blue}"))

	tag := func(r *Renderer) htmltemplate.HTML {
		fn := r.assetIncludeTag("static/css", cssIncludeTmpl, &r.cssIncludeCache)
		out, err := fn()
		if err != nil {
			t.Fatalf("assetIncludeTag: %v", err)
		}
		return out
	}

	// Prime r1's cache, then read both.
	first1 := tag(r1)
	got1 := tag(r1)
	got2 := tag(r2)

	if first1 != got1 {
		t.Error("expected r1's cached tag to be stable across calls")
	}
	if got1 == got2 {
		t.Errorf("expected distinct SRI tags for distinct content, both were:\n%s", got1)
	}
}

func TestWithFuncs_OverrideAndMerge(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"custom.html": &fstest.MapFile{Data: []byte(
			`{{define "custom"}}{{greet}}|{{toUpper "x"}}{{end}}`)},
	}
	r := mustNew(t, fsys, WithFuncs(htmltemplate.FuncMap{
		"greet":   func() string { return "hello" },          // new function
		"toUpper": func(string) string { return "OVERRIDE" }, // override a default
	}))

	w := httptest.NewRecorder()
	r.RenderHTML(w, "custom", nil)
	if got, want := strings.TrimSpace(w.Body.String()), "hello|OVERRIDE"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestWithBuildID_CacheBusting(t *testing.T) {
	t.Parallel()

	r := mustNew(t, testFS("body{}"), WithBuildID("deadbeef"))
	fn := r.assetIncludeTag("static/js", jsIncludeTmpl, &r.jsIncludeCache)
	out, err := fn()
	if err != nil {
		t.Fatalf("assetIncludeTag: %v", err)
	}
	if !strings.Contains(string(out), "?deadbeef") {
		t.Errorf("expected build id in asset URL, got:\n%s", out)
	}
}

func TestRenderText(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"greeting.txt": &fstest.MapFile{Data: []byte(
			`{{define "greeting"}}Hello, {{toUpper .name}}!{{end}}`)},
	}
	r := mustNew(t, fsys)

	var buf strings.Builder
	if err := r.RenderText(&buf, "greeting", map[string]any{"name": "bananas"}); err != nil {
		t.Fatalf("RenderText: %v", err)
	}
	if got, want := buf.String(), "Hello, BANANAS!"; got != want {
		t.Fatalf("RenderText = %q, want %q", got, want)
	}

	// An unknown template returns an error rather than partial output.
	if err := r.RenderText(io.Discard, "nope", nil); err == nil {
		t.Fatal("expected an error for an unknown text template")
	}
}

func TestAllowedResponseCode(t *testing.T) {
	t.Parallel()

	r := mustNew(t, testFS("body{}"))
	if !r.AllowedResponseCode(http.StatusOK) {
		t.Error("200 should be allowed")
	}
	if !r.AllowedResponseCode(http.StatusUnprocessableEntity) {
		t.Error("422 should be allowed (form validation failures)")
	}
	if r.AllowedResponseCode(http.StatusTeapot) {
		t.Error("418 should not be allowed")
	}
}

// errCSV implements CSVMarshaler.
type errCSV struct {
	data []byte
	err  error
}

func (e errCSV) MarshalCSV() ([]byte, error) { return e.data, e.err }

func TestRenderCSV(t *testing.T) {
	t.Parallel()

	r := mustNew(t, testFS("body{}"))

	w := httptest.NewRecorder()
	r.RenderCSV(w, http.StatusOK, "report.csv", errCSV{data: []byte("a,b\n1,2\n")})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Disposition"); got != "attachment;filename=report.csv" {
		t.Errorf("content-disposition = %q", got)
	}
	if w.Body.String() != "a,b\n1,2\n" {
		t.Errorf("body = %q", w.Body.String())
	}

	// A marshaling error yields a 500.
	w = httptest.NewRecorder()
	r.RenderCSV(w, http.StatusOK, "", errCSV{err: fmt.Errorf("nope")})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}
