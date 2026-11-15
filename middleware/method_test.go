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
	"strings"
	"testing"
	"testing/iotest"

	"github.com/gorilla/mux"
	"github.com/mikehelmick/go-bananas/webctx"
)

const formContentType = "application/x-www-form-urlencoded"

// multipartBody returns a multipart body carrying the given fields, plus its
// Content-Type.
func multipartBody(t *testing.T, fields map[string]string) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatalf("WriteField: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("multipart Close: %v", err)
	}
	return &buf, mw.FormDataContentType()
}

// methodSeen runs r through MutateMethod and returns the method the next
// handler observed.
func methodSeen(t *testing.T, r *http.Request) string {
	t.Helper()
	var got string
	h := MutateMethod()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.Method
	}))
	h.ServeHTTP(httptest.NewRecorder(), r)
	return got
}

func TestMutateMethod(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		method    string
		body      string
		query     string
		multipart bool
		// contentType defaults to formContentType.
		contentType string
		want        string
	}{
		{name: "post_to_delete_lowercase", method: http.MethodPost, body: "_method=delete", want: http.MethodDelete},
		{name: "post_to_patch", method: http.MethodPost, body: "_method=PATCH", want: http.MethodPatch},
		{name: "post_to_put", method: http.MethodPost, body: "_method=PUT", want: http.MethodPut},
		{name: "post_to_get_ignored", method: http.MethodPost, body: "_method=GET", want: http.MethodPost},
		{name: "post_to_head_ignored", method: http.MethodPost, body: "_method=HEAD", want: http.MethodPost},
		{name: "post_to_options_ignored", method: http.MethodPost, body: "_method=OPTIONS", want: http.MethodPost},
		{name: "post_to_trace_ignored", method: http.MethodPost, body: "_method=TRACE", want: http.MethodPost},
		{name: "post_bad_query_still_overrides", method: http.MethodPost, body: "_method=DELETE", query: "ref=%zz", want: http.MethodDelete},
		{name: "post_bad_body_ignored", method: http.MethodPost, body: "_method=DELETE&x=%zz", want: http.MethodPost},
		{name: "post_urlencoded_charset", method: http.MethodPost, body: "_method=PUT", contentType: formContentType + "; charset=utf-8", want: http.MethodPut},
		{name: "post_query_ignored", method: http.MethodPost, query: "_method=DELETE", want: http.MethodPost},
		{name: "get_query_ignored", method: http.MethodGet, query: "_method=DELETE", want: http.MethodGet},
		{name: "put_body_ignored", method: http.MethodPut, body: "_method=DELETE", want: http.MethodPut},
		{name: "post_empty_value", method: http.MethodPost, body: "_method=", want: http.MethodPost},
		{name: "post_multipart_ignored", method: http.MethodPost, multipart: true, want: http.MethodPost},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			target := "/"
			if tc.query != "" {
				target += "?" + tc.query
			}

			var body io.Reader = strings.NewReader(tc.body)
			contentType := formContentType
			if tc.contentType != "" {
				contentType = tc.contentType
			}
			if tc.multipart {
				body, contentType = multipartBody(t, map[string]string{formKeyMethod: http.MethodDelete})
			}

			r := httptest.NewRequest(tc.method, target, body)
			r.Header.Set("Content-Type", contentType)

			if got := methodSeen(t, r); got != tc.want {
				t.Errorf("method = %q, want %q", got, tc.want)
			}
		})
	}
}

// repeatReader yields an endless stream of one byte.
type repeatReader byte

func (b repeatReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(b)
	}
	return len(p), nil
}

// closeTracker records whether Close was called.
type closeTracker struct {
	io.Reader
	closed *bool
}

func (c closeTracker) Close() error {
	*c.closed = true
	return nil
}

func TestMutateMethodBodyCap(t *testing.T) {
	t.Parallel()

	t.Run("urlencoded_over_cap", func(t *testing.T) {
		t.Parallel()

		value := strings.Repeat("x", 1<<20)
		body := "a=" + value + "&_method=DELETE"
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		r.Header.Set("Content-Type", formContentType)

		var method, a, override string
		h := MutateMethod()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			method = r.Method
			a = r.PostFormValue("a")
			override = r.PostFormValue(formKeyMethod)
		}))
		h.ServeHTTP(httptest.NewRecorder(), r)

		if method != http.MethodPost {
			t.Errorf("method = %q, want POST", method)
		}
		if a != value {
			t.Errorf("downstream form value a has length %d, want %d", len(a), len(value))
		}
		if override != http.MethodDelete {
			t.Errorf("downstream _method = %q, want DELETE", override)
		}
	})

	t.Run("under_cap_body_preserved", func(t *testing.T) {
		t.Parallel()

		body := "amount=1&note=" + strings.Repeat("y", 1000) + "&_method=DELETE"
		r := httptest.NewRequest(http.MethodPost, "/?ref=%zz", strings.NewReader(body))
		r.Header.Set("Content-Type", formContentType)

		var (
			method string
			got    []byte
			closed bool
		)
		r.Body = closeTracker{Reader: r.Body, closed: &closed}
		h := MutateMethod()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			method = r.Method
			got, _ = io.ReadAll(r.Body)
			_ = r.Body.Close()
		}))
		h.ServeHTTP(httptest.NewRecorder(), r)

		if method != http.MethodDelete {
			t.Errorf("method = %q, want DELETE", method)
		}
		if string(got) != body {
			t.Errorf("downstream body differs from original (got %d bytes, want %d)", len(got), len(body))
		}
		if !closed {
			t.Error("closing the restored body did not close the original body")
		}
	})

	t.Run("multipart_not_parsed", func(t *testing.T) {
		t.Parallel()

		const size = 100 << 20
		boundary := "mutatemethodboundary"
		head := "--" + boundary + "\r\n" +
			"Content-Disposition: form-data; name=\"" + formKeyMethod + "\"\r\n\r\nDELETE\r\n" +
			"--" + boundary + "\r\n" +
			"Content-Disposition: form-data; name=\"file\"; filename=\"big.bin\"\r\n" +
			"Content-Type: application/octet-stream\r\n\r\n"
		tail := "\r\n--" + boundary + "--\r\n"
		body := io.MultiReader(strings.NewReader(head), io.LimitReader(repeatReader('a'), size), strings.NewReader(tail))

		r := httptest.NewRequest(http.MethodPost, "/", body)
		r.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)

		var (
			method string
			parsed bool
		)
		h := MutateMethod()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			method = r.Method
			parsed = r.MultipartForm != nil
		}))
		h.ServeHTTP(httptest.NewRecorder(), r)

		if parsed {
			t.Error("MultipartForm was parsed by MutateMethod")
		}
		if method != http.MethodPost {
			t.Errorf("method = %q, want POST", method)
		}
	})
}

// seenMethodHeader is set by the test handler to the method it observed.
const seenMethodHeader = "X-Seen-Method"

// overrideRouter builds a router whose /token route (GET) returns a masked CSRF
// token and whose /transfer route records the method it saw in
// seenMethodHeader and writes PostFormValue("note") as the body. If wrap,
// MutateMethod wraps the router; otherwise it is mounted with r.Use. If methods
// is non-empty, /transfer is restricted to them. The router is stateless, so it
// can be shared across requests.
func overrideRouter(tb testing.TB, wrap bool, methods ...string) http.Handler {
	tb.Helper()
	h := testRenderer(tb)

	r := mux.NewRouter()
	if wrap {
		r.Use(RequireSession(testStore(), nil, h), HandleCSRF(h))
	} else {
		r.Use(MutateMethod(), RequireSession(testStore(), nil, h), HandleCSRF(h))
	}
	r.Handle("/token", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, _ := webctx.TemplateMapFromContext(r.Context())["csrfToken"].(template.HTML)
		_, _ = io.WriteString(w, string(tok))
	})).Methods(http.MethodGet)
	route := r.Handle("/transfer", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(seenMethodHeader, r.Method)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, r.PostFormValue("note"))
	}))
	if len(methods) > 0 {
		route.Methods(methods...)
	}

	if wrap {
		return MutateMethod()(r)
	}
	return r
}

// csrfSession fetches /token from router and returns the session cookies and
// the masked CSRF token bound to them.
func csrfSession(tb testing.TB, router http.Handler) ([]*http.Cookie, string) {
	tb.Helper()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/token", nil))
	if w.Code != http.StatusOK || w.Body.Len() == 0 {
		tb.Fatalf("GET /token: status %d, body %q", w.Code, w.Body.String())
	}
	return w.Result().Cookies(), w.Body.String()
}

// postForm sends a urlencoded POST of body to /transfer with the cookies.
func postForm(router http.Handler, query, body string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/transfer", strings.NewReader(body))
	r.URL.RawQuery = query
	r.Header.Set("Content-Type", formContentType)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

func TestMutateMethodUseChainCSRF(t *testing.T) {
	t.Parallel()

	router := overrideRouter(t, false, http.MethodPost)
	w := postForm(router, "", "amount=1&_method=GET", nil)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
	if got := w.Header().Get(seenMethodHeader); got != "" {
		t.Errorf("handler ran (saw %q) without a CSRF token", got)
	}
}

func TestMutateMethodOverrideWithToken(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		wrap   bool
		routes string
	}{
		// Wrapping the router: the override drives routing to a DELETE route.
		{name: "wrapped_router", wrap: true, routes: http.MethodDelete},
		// r.Use: routing already matched POST; the handler sees DELETE.
		{name: "use_chain", wrap: false, routes: http.MethodPost},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			router := overrideRouter(t, tc.wrap, tc.routes)
			cookies, token := csrfSession(t, router)

			body := url.Values{formKeyMethod: {"DELETE"}, CSRFFormField: {token}, "note": {"x"}}.Encode()
			w := postForm(router, "", body, cookies)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body=%q)", w.Code, w.Body.String())
			}
			if got := w.Header().Get(seenMethodHeader); got != http.MethodDelete {
				t.Errorf("handler method = %q, want DELETE", got)
			}
			if got := w.Body.String(); got != "x" {
				t.Errorf("FormValue(note) = %q, want x", got)
			}
		})
	}
}

func TestMutateMethodPostForm(t *testing.T) {
	t.Parallel()

	t.Run("already_parsed_is_used", func(t *testing.T) {
		t.Parallel()

		// The body is unreadable; an earlier middleware already parsed the form.
		r := httptest.NewRequest(http.MethodPost, "/", iotest.ErrReader(io.ErrUnexpectedEOF))
		r.Header.Set("Content-Type", formContentType)
		r.PostForm = url.Values{formKeyMethod: {"patch"}}

		if got := methodSeen(t, r); got != http.MethodPatch {
			t.Errorf("method = %q, want PATCH", got)
		}
	})

	t.Run("no_override_leaves_postform_nil", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("_method=GET&a=1"))
		r.Header.Set("Content-Type", formContentType)

		var parsed bool
		h := MutateMethod()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			parsed = r.PostForm != nil
		}))
		h.ServeHTTP(httptest.NewRecorder(), r)

		if parsed {
			t.Error("PostForm was set without an override")
		}
	})

	t.Run("override_sets_postform", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequest(http.MethodPost, "/?q=1", strings.NewReader("_method=DELETE&note=x"))
		r.Header.Set("Content-Type", formContentType)

		var note, q string
		h := MutateMethod()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			note = r.FormValue("note")
			q = r.FormValue("q")
		}))
		h.ServeHTTP(httptest.NewRecorder(), r)

		if note != "x" || q != "1" {
			t.Errorf("FormValue note=%q q=%q, want x and 1", note, q)
		}
	})
}
