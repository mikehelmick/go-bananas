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
	"regexp"
	"strings"
	"testing"

	"github.com/mikehelmick/go-bananas/internal/util"
	"github.com/mikehelmick/go-bananas/webctx"
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

// FuzzTraceID sends an arbitrary trace header through PopulateTraceID. The
// middleware never generates an ID: it stores the portion before the first
// "/" when that is well-formed, and stores nothing otherwise. Whatever lands on
// the context must match validTraceID, because response.TracedHTTPClient
// re-emits it on outbound requests.
func FuzzTraceID(f *testing.F) {
	f.Add("105445aa7843bc8bf206b12000100000/1;o=1")
	f.Add("1d09592c-3914-4ff9-83cd-29659da1d4ac")
	f.Add(" abc /1")
	f.Add("abc\r\nX-Injected: 1")
	f.Add("abc\tdef")
	f.Add("/abc")
	f.Add("///")
	f.Add(strings.Repeat("a", 128))
	f.Add(strings.Repeat("a", 129))
	f.Add("abéc")
	f.Add("")

	traceID := regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

	f.Fuzz(func(t *testing.T, header string) {
		var got string
		h := PopulateTraceID()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			got = webctx.TraceIDFromContext(r.Context())
		}))
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header[TraceHeader] = []string{header}
		h.ServeHTTP(httptest.NewRecorder(), r)

		if got == "" {
			return
		}
		if !traceID.MatchString(got) {
			t.Fatalf("stored trace ID %q from header %q is malformed", got, header)
		}
		prefix, _, _ := strings.Cut(header, "/")
		if got != strings.TrimSpace(prefix) {
			t.Fatalf("stored trace ID %q, want the header prefix %q", got, prefix)
		}
	})
}

// FuzzProcessLocale runs ProcessLocale over a stub provider that echoes the
// first non-empty hint as the language. textDirection must always be ltr or
// rtl, and rtl only for a right-aligned language.
func FuzzProcessLocale(f *testing.F) {
	f.Add("", "")
	f.Add("ar", "")
	f.Add("", "ar")
	f.Add("AR", "en-US,en;q=0.9")
	f.Add("ar-EG", "")
	f.Add("en\x00", "ar")
	f.Add("ar;q=1", "ar")

	f.Fuzz(func(t *testing.T, lang, acceptLang string) {
		var m webctx.TemplateMap
		next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			m = webctx.TemplateMapFromContext(r.Context())
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.URL.RawQuery = url.Values{QueryKeyLanguage: {lang}}.Encode()
		r.Header[HeaderAcceptLanguage] = []string{acceptLang}
		ProcessLocale(fakeLocaleProvider{})(next).ServeHTTP(httptest.NewRecorder(), r)

		if m == nil {
			t.Fatal("handler did not run")
		}
		_, wantLang := fakeLocaleProvider{}.Lookup(lang, acceptLang)
		if m["textLanguage"] != wantLang {
			t.Fatalf("textLanguage = %v, want %q", m["textLanguage"], wantLang)
		}
		dir := m["textDirection"]
		switch dir {
		case LeftAlign, RightAlign:
		default:
			t.Fatalf("textDirection = %v, want ltr or rtl", dir)
		}
		_, rtl := rightAlignLanguages[wantLang]
		if (dir == RightAlign) != rtl {
			t.Fatalf("textDirection = %v for language %q", dir, wantLang)
		}
	})
}

// FuzzRequireHostHeader checks that RequireHostHeader with stripPort admits a
// request only when its Host, cut at the first ":" (when that is not the first
// byte), equals example.com ignoring case. Cutting at the first colon means a
// bracketed IPv6 host never matches; that fails closed.
func FuzzRequireHostHeader(f *testing.F) {
	f.Add("example.com")
	f.Add("EXAMPLE.COM")
	f.Add("example.com:8080")
	f.Add("example.com:")
	f.Add("example.com.")
	f.Add(":example.com")
	f.Add("evil.com:example.com")
	f.Add("example.com.evil.com")
	f.Add("[::1]:443")
	f.Add("example.coK")
	f.Add("")

	handler := RequireHostHeader([]string{"example.com"}, testRenderer(f), true)(okHandler("ok"))

	f.Fuzz(func(t *testing.T, host string) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Host = host
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		name := host
		if i := strings.Index(name, ":"); i > 0 {
			name = name[:i]
		}
		allowed := strings.EqualFold(name, "example.com")
		if got := w.Code == http.StatusOK; got != allowed {
			t.Fatalf("host %q: status %d, allowed = %v", host, w.Code, allowed)
		}
	})
}

// FuzzStaticPath checks that ConfigureStaticAssets, in either mode, never
// serves a directory path: a file server would answer it with a listing.
func FuzzStaticPath(f *testing.F) {
	f.Add("/")
	f.Add("/static/")
	f.Add("/static/app.css")
	f.Add("/static/..%2f")
	f.Add("//")
	f.Add("/static/app.css/")
	f.Add("")

	handlers := []http.Handler{
		ConfigureStaticAssets(false)(okHandler("listing")),
		ConfigureStaticAssets(true)(okHandler("listing")),
	}

	f.Fuzz(func(t *testing.T, path string) {
		for _, h := range handlers {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.URL.Path = path
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			if strings.HasSuffix(path, "/") && w.Code == http.StatusOK {
				t.Fatalf("directory path %q served with 200", path)
			}
		}
	})
}

// FuzzCSP checks ContentSecurityPolicy with no nonce on the context. When the
// policy's only nonce sources are the 'nonce-{{nonce}}' placeholder, the
// header must carry no nonce source at all, since it would be empty and
// guessable. With a nonce on the context, every placeholder is filled in.
func FuzzCSP(f *testing.F) {
	f.Add("default-src 'self'")
	f.Add("script-src 'self' 'nonce-{{nonce}}'")
	f.Add("script-src 'nonce-{{nonce}}'; style-src 'nonce-{{nonce}}' ; img-src *")
	f.Add("script-src {{nonce}}")
	f.Add("script-src 'nonce-{{nonce}}{{nonce}}'")
	f.Add("script-src 'nonce-{{{{nonce}}nonce}}'")
	f.Add("script-src 'nonce-")
	f.Add("")

	const nonce = "dGVzdG5vbmNl"

	f.Fuzz(func(t *testing.T, policy string) {
		if strings.Contains(strings.ReplaceAll(policy, "'nonce-"+CSPNoncePlaceholder+"'", ""), "'nonce-") {
			t.Skip("policy carries a nonce source other than the placeholder")
		}
		h := ContentSecurityPolicy(policy)(okHandler("ok"))

		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if got := w.Header().Get("Content-Security-Policy"); strings.Contains(got, "'nonce-") {
			t.Fatalf("policy %q without a nonce produced %q", policy, got)
		}

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r = r.WithContext(webctx.WithNonce(r.Context(), nonce))
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := strings.ReplaceAll(policy, CSPNoncePlaceholder, nonce)
		if got := w.Header().Get("Content-Security-Policy"); got != want {
			t.Fatalf("policy %q with nonce produced %q, want %q", policy, got, want)
		}
	})
}
