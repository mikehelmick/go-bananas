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
	"html/template"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/mikehelmick/go-bananas/webctx"
)

// csrfRouter builds a router with RequireSession and HandleCSRF(opts...). GET
// /token returns a masked token. /submit accepts any method, reads the whole
// body; a body read error yields 500, otherwise 200 with the byte count in
// bodyBytesHeader.
func csrfRouter(tb testing.TB, opts ...CSRFOption) http.Handler {
	tb.Helper()
	h := testRenderer(tb)

	r := mux.NewRouter()
	r.Use(RequireSession(testStore(), nil, h), HandleCSRF(h, opts...))
	r.Handle("/token", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, _ := webctx.TemplateMapFromContext(r.Context())["csrfToken"].(template.HTML)
		_, _ = io.WriteString(w, string(tok))
	})).Methods(http.MethodGet)
	r.Handle("/submit", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set(bodyBytesHeader, strconv.FormatInt(n, 10))
		w.WriteHeader(http.StatusOK)
	}))
	return r
}

// bodyBytesHeader is set by the /submit handler to the body length it read.
const bodyBytesHeader = "X-Body-Bytes"

// bigMultipart returns a multipart body with a csrf_token field followed by a
// file part of size bytes, plus its Content-Type.
func bigMultipart(t testing.TB, token string, size int) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField(CSRFFormField, token); err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	fw, err := mw.CreateFormFile("file", "big.bin")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(bytes.Repeat([]byte("a"), size)); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("multipart Close: %v", err)
	}
	return &buf, mw.FormDataContentType()
}

func sendWithCookies(router http.Handler, r *http.Request, cookies []*http.Cookie) *httptest.ResponseRecorder {
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

func TestCSRFBodyCap(t *testing.T) {
	t.Parallel()

	if DefaultCSRFMaxBodyBytes != 1<<20 {
		t.Errorf("DefaultCSRFMaxBodyBytes = %d, want 1 MiB", DefaultCSRFMaxBodyBytes)
	}

	const size = 2 << 20

	cases := []struct {
		name string
		opts []CSRFOption
		want int
	}{
		{name: "default_rejects", want: http.StatusRequestEntityTooLarge},
		{name: "raised_cap_accepts", opts: []CSRFOption{WithCSRFMaxBodyBytes(8 << 20)}, want: http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			router := csrfRouter(t, tc.opts...)
			cookies, token := csrfSession(t, router)

			body, ct := bigMultipart(t, token, size)
			r := httptest.NewRequest(http.MethodPost, "/submit", body)
			r.Header.Set("Content-Type", ct)
			w := sendWithCookies(router, r, cookies)

			if w.Code != tc.want {
				t.Errorf("status = %d, want %d (body %q)", w.Code, tc.want, w.Body.String())
			}
		})
	}

	t.Run("urlencoded_over_cap", func(t *testing.T) {
		t.Parallel()

		router := csrfRouter(t)
		cookies, token := csrfSession(t, router)

		body := url.Values{CSRFFormField: {token}, "pad": {strings.Repeat("p", size)}}.Encode()
		r := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(body))
		r.Header.Set("Content-Type", formContentType)
		w := sendWithCookies(router, r, cookies)

		if w.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("status = %d, want 413", w.Code)
		}
	})

	t.Run("header_token_json_not_capped", func(t *testing.T) {
		t.Parallel()

		router := csrfRouter(t)
		cookies, token := csrfSession(t, router)

		body := `{"pad":"` + strings.Repeat("j", size) + `"}`
		r := httptest.NewRequest(http.MethodPut, "/submit", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set(CSRFHeaderField, token)
		w := sendWithCookies(router, r, cookies)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		if got, want := w.Header().Get(bodyBytesHeader), strconv.Itoa(len(body)); got != want {
			t.Errorf("handler read %s bytes, want %s", got, want)
		}
	})

	t.Run("get_not_capped", func(t *testing.T) {
		t.Parallel()

		router := csrfRouter(t)
		r := httptest.NewRequest(http.MethodGet, "/submit", bytes.NewReader(make([]byte, size)))
		w := sendWithCookies(router, r, nil)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
	})
}

func TestCSRFTraceNotSafe(t *testing.T) {
	t.Parallel()

	router := csrfRouter(t)
	r := httptest.NewRequest(http.MethodTrace, "/submit", nil)
	w := sendWithCookies(router, r, nil)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("TRACE status = %d, want 401", w.Code)
	}
}

// TestCSRFMultipartTempFilesRemoved uploads a multipart body larger than the
// in-memory limit, so ParseMultipartForm spills to temp files, and checks that
// none are left once HandleCSRF returns.
func TestCSRFMultipartTempFilesRemoved(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)

	const size = 40 << 20
	router := csrfRouter(t, WithCSRFMaxBodyBytes(128<<20))
	cookies, token := csrfSession(t, router)

	cases := []struct {
		name  string
		token string
		want  int
	}{
		{name: "no_token_rejected", want: http.StatusUnauthorized},
		{name: "valid_token_served", token: token, want: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, ct := bigMultipart(t, tc.token, size)
			r := httptest.NewRequest(http.MethodPost, "/submit", body)
			r.Header.Set("Content-Type", ct)
			w := sendWithCookies(router, r, cookies)

			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d", w.Code, tc.want)
			}
			left, err := filepath.Glob(filepath.Join(dir, "multipart-*"))
			if err != nil {
				t.Fatalf("Glob: %v", err)
			}
			if len(left) > 0 {
				t.Errorf("multipart temp files left behind: %v", left)
			}
		})
	}
}

// TestCSRFBodyCapWithMutateMethod checks that the body cap does not break an
// overridden DELETE whose token MutateMethod already parsed from the body.
func TestCSRFBodyCapWithMutateMethod(t *testing.T) {
	t.Parallel()

	for _, wrap := range []bool{true, false} {
		name := "use_chain"
		routes := http.MethodPost
		if wrap {
			name, routes = "wrapped_router", http.MethodDelete
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			router := overrideRouter(t, wrap, routes)
			cookies, token := csrfSession(t, router)

			body := url.Values{formKeyMethod: {"DELETE"}, CSRFFormField: {token}, "note": {"keep"}}.Encode()
			w := postForm(router, "", body, cookies)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
			}
			if got := w.Header().Get(seenMethodHeader); got != http.MethodDelete {
				t.Errorf("method = %q, want DELETE", got)
			}
			if got := w.Body.String(); got != "keep" {
				t.Errorf("note = %q, want keep", got)
			}
		})
	}
}
