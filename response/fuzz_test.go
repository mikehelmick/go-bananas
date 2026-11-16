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
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// FuzzBack checks that Back only ever redirects to "/" or to an absolute
// http(s) URL on the request's own, non-empty host.
func FuzzBack(f *testing.F) {
	seeds := []struct{ referer, host string }{
		{"https://example.com/page", "example.com"},
		{"https://evil.com/page", "example.com"},
		{"javascript://example.com/x", "example.com"},
		{"//example.com/x", "example.com"},
		{`/\evil.com`, "example.com"},
		{"https:///evil.com", ""},
		{`https:\\evil.com`, ""},
		{"http://example.com:8080/x", "example.com:8080"},
		{"https://login.bank@example.com/x", "example.com"},
		{"", ""},
	}
	for _, s := range seeds {
		f.Add(s.referer, s.host)
	}

	h := testRenderer(f)

	f.Fuzz(func(t *testing.T, referer, host string) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = host
		req.Header["Referer"] = []string{referer}
		w := httptest.NewRecorder()

		Back(w, req, h)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303", w.Code)
		}
		loc := w.Header().Get("Location")
		if loc == "/" {
			return
		}
		if strings.HasPrefix(loc, "//") || strings.HasPrefix(loc, `/\`) {
			t.Fatalf("Location %q is scheme-relative", loc)
		}
		u, err := url.Parse(loc)
		if err != nil {
			t.Fatalf("Location %q does not parse: %v", loc, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			t.Fatalf("Location %q has scheme %q", loc, u.Scheme)
		}
		if u.Host == "" || u.Host != host {
			t.Fatalf("Location %q has host %q, want %q", loc, u.Host, host)
		}
		if u.User != nil {
			t.Fatalf("Location %q has userinfo", loc)
		}
	})
}

// FuzzRealHost builds a request from an arbitrary Host header and request
// URI and checks that RealHostFromRequest does not panic and returns a URL
// that url.Parse accepts. It caught IPv6 hosts losing their brackets
// ("[::1]:8443" became "https://::1:8443").
func FuzzRealHost(f *testing.F) {
	seeds := []struct{ host, uri string }{
		{"example.com", "/"},
		{"example.com:443", "/x"},
		{"localhost:80", "/"},
		{"127.0.0.1:8080", "/"},
		{"[::1]:8443", "/"},
		{"[::1", "/"},
		{"example.com", "https://other.example:443/x"},
		{"example.com", "http://[fe80::1]:80/"},
		{"[2001:db8::1]", "/"},
		{"[::ffff:127.0.0.1]:443", "/"},
		{"", "/"},
		{"a b", "/"},
		{"example.com:abc", "/"},
		{"evil.com/path?x#y", "/"},
		{"user@example.com", "/"},
		{"example.com", "//evil.com/x"},
		{"example.com", "*"},
	}
	for _, s := range seeds {
		f.Add(s.host, s.uri)
	}

	f.Fuzz(func(t *testing.T, host, uri string) {
		u, err := url.ParseRequestURI(uri)
		if err != nil {
			t.Skip("not a valid request URI")
		}
		if !validHostHeader(host) {
			t.Skip("net/http rejects this Host header with 400")
		}
		// Malformed authorities (an unclosed bracket, a non-numeric port, an
		// unbracketed IPv6 literal such as "::") and IPv6 zone IDs, which no
		// browser sends, are out of scope: there is no well-formed URL to return
		// for them.
		effective := host
		if u.IsAbs() {
			effective = u.Host
		}
		pu, err := url.Parse("//" + effective)
		if err != nil ||
			strings.Contains(pu.Hostname(), ":") && !strings.HasPrefix(pu.Host, "[") ||
			strings.Contains(effective, "%") && strings.Contains(effective, "[") {
			t.Skip("malformed authority")
		}
		r := &http.Request{Method: http.MethodGet, URL: u, Host: host, Header: http.Header{}}

		got := RealHostFromRequest(r)
		if _, err := url.Parse(got); err != nil {
			t.Fatalf("RealHostFromRequest(host=%q, uri=%q) = %q, which does not parse: %v", host, uri, got, err)
		}
	})
}

// validHostHeader mirrors the byte check net/http's server applies to the Host
// header (httpguts.ValidHostHeader) before a handler ever sees the request.
func validHostHeader(h string) bool {
	for i := 0; i < len(h); i++ {
		c := h[i]
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
		case strings.IndexByte("!$%&'()*+,-.:;=[]_~", c) >= 0:
		default:
			return false
		}
	}
	return true
}
