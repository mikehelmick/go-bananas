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
