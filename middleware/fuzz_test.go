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
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mikehelmick/go-bananas/internal/util"
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
		{http.MethodTrace, "", ""},
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
				case http.MethodGet, http.MethodHead, http.MethodOptions:
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

// FuzzSecureHeadersRedirect sends plain-HTTP requests with a fuzzed Host,
// X-Forwarded-Host and path through SecureHeaders with an allowlist, both with
// and without X-Forwarded-Host trusted. Any redirect must point at the
// allowlisted host, and no plain-HTTP request may be served.
func FuzzSecureHeadersRedirect(f *testing.F) {
	const allowed = "victim"
	configs := []struct {
		name    string
		handler http.Handler
	}{
		{name: "default", handler: SecureHeaders(false, ServerTypeHTML, WithAllowedHosts(allowed))(okHandler("ok"))},
		{name: "trusted", handler: SecureHeaders(false, ServerTypeHTML,
			WithTrustedHostHeaders("X-Forwarded-Host"), WithAllowedHosts(allowed))(okHandler("ok"))},
	}

	f.Add("victim", "", "/login")
	f.Add("victim", "evil.example", "/login")
	f.Add("evil.example", "victim", "/")
	f.Add("VICTIM", "", "//evil.example/x")
	f.Add("victim:8080", "", "/")
	f.Add("victim", "evil.example", "/\\evil.example")
	f.Add("", "", "")

	f.Fuzz(func(t *testing.T, host, xfh, path string) {
		for _, c := range configs {
			r := &http.Request{
				Method: http.MethodGet,
				URL:    &url.URL{Path: path},
				Host:   host,
				Header: http.Header{},
			}
			if xfh != "" {
				r.Header.Set("X-Forwarded-Host", xfh)
			}

			w := httptest.NewRecorder()
			c.handler.ServeHTTP(w, r)

			loc := w.Header().Get("Location")
			if loc == "" {
				if w.Code >= 200 && w.Code < 300 {
					t.Errorf("%s: plain-HTTP request served with %d (host=%q xfh=%q)", c.name, w.Code, host, xfh)
				}
				continue
			}
			u, err := url.Parse(loc)
			if err != nil {
				t.Errorf("%s: unparsable Location %q: %v", c.name, loc, err)
				continue
			}
			if !strings.EqualFold(u.Host, allowed) {
				t.Errorf("%s: Location host %q not allowlisted (host=%q xfh=%q path=%q)", c.name, u.Host, host, xfh, path)
			}
		}
	})
}

// FuzzCSRFToken feeds arbitrary header and body values to tokenFromRequest.
// It must never panic, an arbitrary value must never validate against a fresh
// random token, the masked existing token must always validate, and
// mask/unmask must round-trip.
func FuzzCSRFToken(f *testing.F) {
	f.Add("", "")
	f.Add("AAAA", "csrf_token=AAAA")
	f.Add("", "csrf_token=%zz")
	f.Add("!!!", "")
	f.Add("", "--x\r\nContent-Disposition: form-data; name=\"csrf_token\"\r\n\r\nAAAA\r\n--x--\r\n")
	f.Add(strings.Repeat("A", 2*TokenLength), "")

	contentTypes := []string{formContentType, "multipart/form-data; boundary=x"}

	f.Fuzz(func(t *testing.T, header, body string) {
		existing, err := util.RandomBytes(TokenLength)
		if err != nil {
			t.Fatalf("RandomBytes: %v", err)
		}

		for _, ct := range contentTypes {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
			r.Body = http.MaxBytesReader(nil, r.Body, DefaultCSRFMaxBodyBytes)
			r.Header.Set("Content-Type", ct)
			if header != "" {
				r.Header.Set(CSRFHeaderField, header)
			}

			got, err := tokenFromRequest(r)
			if err == nil && len(got) == 0 {
				t.Errorf("tokenFromRequest returned an empty token without error (header=%q)", header)
			}
			if subtle.ConstantTimeCompare(existing, got) == 1 {
				t.Errorf("arbitrary input validated (header=%q body=%q)", header, body)
			}
		}

		// The masked existing token always validates, whatever the body.
		masked, err := mask(existing)
		if err != nil {
			t.Fatalf("mask: %v", err)
		}
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		r.Header.Set(CSRFHeaderField, masked)
		got, err := tokenFromRequest(r)
		if err != nil || subtle.ConstantTimeCompare(existing, got) != 1 {
			t.Errorf("masked existing token did not validate: %v", err)
		}

		// mask/unmask round-trips arbitrary non-empty values.
		for _, raw := range [][]byte{[]byte(header), []byte(body)} {
			if len(raw) == 0 {
				continue
			}
			m, err := mask(raw)
			if err != nil {
				t.Fatalf("mask: %v", err)
			}
			b, err := base64.RawURLEncoding.DecodeString(m)
			if err != nil {
				t.Fatalf("decode mask output: %v", err)
			}
			back, err := unmask(b)
			if err != nil || !bytes.Equal(back, raw) {
				t.Errorf("unmask(mask(%q)) = %q, %v", raw, back, err)
			}
		}
	})
}
