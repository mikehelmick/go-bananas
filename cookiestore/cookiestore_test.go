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

package cookiestore

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/sessions"
)

// saveCookie creates a session from store, saves it, and returns the parsed
// Set-Cookie and the Save error.
func saveCookie(t *testing.T, store sessions.Store) (*http.Cookie, error) {
	t.Helper()

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	s, err := store.New(r, "s")
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	s.Values["k"] = "v"
	w := httptest.NewRecorder()
	if err := s.Save(r, w); err != nil {
		return nil, err
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	return cookies[0], nil
}

func TestNewCookieDefaults(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		opts     *sessions.Options
		o        []Option
		secure   bool
		sameSite http.SameSite
	}{
		{name: "nil_opts", secure: true, sameSite: http.SameSiteLaxMode},
		{name: "path_only", opts: &sessions.Options{Path: "/"}, secure: true, sameSite: http.SameSiteLaxMode},
		{name: "explicit_false", opts: &sessions.Options{HttpOnly: false, Secure: false}, secure: true, sameSite: http.SameSiteLaxMode},
		{name: "default_mode", opts: &sessions.Options{SameSite: http.SameSiteDefaultMode}, secure: true, sameSite: http.SameSiteLaxMode},
		{name: "strict_kept", opts: &sessions.Options{SameSite: http.SameSiteStrictMode}, secure: true, sameSite: http.SameSiteStrictMode},
		{name: "none_secure", opts: &sessions.Options{SameSite: http.SameSiteNoneMode}, secure: true, sameSite: http.SameSiteNoneMode},
		{name: "insecure", o: []Option{WithInsecureCookies()}, secure: false, sameSite: http.SameSiteLaxMode},
		{name: "insecure_overrides_secure", opts: &sessions.Options{Secure: true}, o: []Option{WithInsecureCookies()}, secure: false, sameSite: http.SameSiteLaxMode},
		{name: "insecure_none_forces_secure", opts: &sessions.Options{SameSite: http.SameSiteNoneMode}, o: []Option{WithInsecureCookies()}, secure: true, sameSite: http.SameSiteNoneMode},
		{name: "nil_option_skipped", o: []Option{nil}, secure: true, sameSite: http.SameSiteLaxMode},
		{name: "nil_option_with_insecure", o: []Option{nil, WithInsecureCookies(), nil}, secure: false, sameSite: http.SameSiteLaxMode},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var before sessions.Options
			if tc.opts != nil {
				before = *tc.opts
			}

			store := New(staticEntropy(key64(t)), tc.opts, tc.o...)
			// Callers type-assert the v1.0 concrete type; keep it.
			if _, ok := store.(*sessions.CookieStore); !ok {
				t.Fatalf("New returned %T, want *sessions.CookieStore", store)
			}
			c, err := saveCookie(t, store)

			// The caller's options are never mutated (D18).
			if tc.opts != nil && *tc.opts != before {
				t.Errorf("caller options mutated: got %+v, want %+v", *tc.opts, before)
			}

			if err != nil {
				t.Fatalf("Save: %v", err)
			}
			if !c.HttpOnly {
				t.Error("cookie is not HttpOnly")
			}
			if c.Secure != tc.secure {
				t.Errorf("Secure = %v, want %v", c.Secure, tc.secure)
			}
			if c.SameSite != tc.sameSite {
				t.Errorf("SameSite = %v, want %v", c.SameSite, tc.sameSite)
			}
			if c.Path != "/" {
				t.Errorf("Path = %q, want /", c.Path)
			}
		})
	}
}

// TestStoreRoundTrip checks that a saved cookie loads back through Get.
func TestStoreRoundTrip(t *testing.T) {
	t.Parallel()

	store := New(staticEntropy(key64(t)), nil)
	c, err := saveCookie(t, store)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(c)
	s, err := store.Get(r, "s")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if s.IsNew {
		t.Error("loaded session reports IsNew")
	}
	if got := s.Values["k"]; got != "v" {
		t.Errorf("Values[k] = %v, want v", got)
	}
	// The registry caches Get per request.
	if s2, _ := store.Get(r, "s"); s2 != s {
		t.Error("Get did not return the registry-cached session")
	}
}
