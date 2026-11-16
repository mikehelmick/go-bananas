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
)

// FuzzMutateMethodCSRF sends requests through MutateMethod, RequireSession
// and HandleCSRF in both supported shapes: wrapping the router, and mounted via
// r.Use. The /transfer route accepts every method so any method that slips past
// CSRF is observable.
func FuzzMutateMethodCSRF(f *testing.F) {
	shapes := []struct {
		name    string
		router  http.Handler
		cookies []*http.Cookie
		token   string
	}{
		{name: "wrapped", router: overrideRouter(f, true)},
		{name: "use", router: overrideRouter(f, false)},
	}
	for i := range shapes {
		shapes[i].cookies, shapes[i].token = csrfSession(f, shapes[i].router)
	}

	seeds := []struct{ method, body, query string }{
		{http.MethodPost, "_method=delete", ""},
		{http.MethodPost, "_method=PATCH", ""},
		{http.MethodPost, "_method=PUT", ""},
		{http.MethodPost, "_method=GET", ""},
		{http.MethodPost, "_method=HEAD", ""},
		{http.MethodPost, "_method=OPTIONS", ""},
		{http.MethodPost, "_method=TRACE", ""},
		{http.MethodPost, "", "_method=DELETE"},
		{http.MethodGet, "", "_method=DELETE"},
		{http.MethodPut, "_method=DELETE", ""},
		{http.MethodPost, "_method=", ""},
		{http.MethodPost, "amount=1&_method=GET", "_method=GET"},
		{http.MethodPost, "_method=DELETE", "ref=%zz"},
		{"post", "_method=get", ""},
	}
	contentTypes := []string{
		formContentType,
		formContentType + "; charset=utf-8",
		"multipart/form-data; boundary=x",
		"",
	}
	for _, s := range seeds {
		for _, ct := range contentTypes {
			f.Add(s.method, s.body, s.query, ct)
		}
	}

	f.Fuzz(func(t *testing.T, method, body, query, contentType string) {
		for _, sh := range shapes {
			// Tokenless request with fuzzed method, body, query and type.
			r, err := http.NewRequest(method, "/transfer", strings.NewReader(body))
			if err != nil {
				t.Skip("invalid method")
			}
			// NewRequest maps "" to GET; compare against what was actually sent.
			orig := r.Method
			r.URL.RawQuery = query
			r.Header.Set("Content-Type", contentType)

			w := httptest.NewRecorder()
			sh.router.ServeHTTP(w, r)

			if seen := w.Header().Get(seenMethodHeader); seen != "" {
				// (a) Without a token, only methods HandleCSRF treats as safe may
				// reach the handler.
				switch orig {
				case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
				default:
					t.Errorf("%s: handler ran for %q without a CSRF token (body=%q query=%q type=%q, saw %q)",
						sh.name, orig, body, query, contentType, seen)
				}

				// (b) The method is either unchanged or one of the allowed overrides.
				switch seen {
				case orig, http.MethodPut, http.MethodPatch, http.MethodDelete:
				default:
					t.Errorf("%s: handler saw method %q for original %q", sh.name, seen, orig)
				}
			}

			// (c) A valid token plus an override reaches the handler as DELETE with
			// the body fields intact.
			form := url.Values{formKeyMethod: {"delete"}, CSRFFormField: {sh.token}, "note": {body}}.Encode()
			if len(form) > maxMethodOverrideBodyBytes {
				continue
			}
			w = postForm(sh.router, query, form, sh.cookies)
			if w.Code != http.StatusOK {
				t.Errorf("%s: valid override got status %d (note=%q query=%q)", sh.name, w.Code, body, query)
				continue
			}
			if got := w.Header().Get(seenMethodHeader); got != http.MethodDelete {
				t.Errorf("%s: valid override saw method %q, want DELETE", sh.name, got)
			}
			if got := w.Body.String(); got != body {
				t.Errorf("%s: note = %q, want %q", sh.name, got, body)
			}
		}
	})
}
