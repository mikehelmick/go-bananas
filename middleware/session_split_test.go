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
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/mikehelmick/go-bananas/webctx"
)

// splitRouter serves /login, /logout and /whoami behind RequireSession with
// "user" split into its own cookie.
func splitRouter(t *testing.T) http.Handler {
	t.Helper()

	r := mux.NewRouter()
	r.Use(RequireSession(testStore(), []any{"user"}, testRenderer(t)))
	r.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		s := webctx.SessionFromContext(r.Context())
		s.Values = map[any]any{"user": "alice"}
		w.WriteHeader(http.StatusOK)
	})
	r.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		webctx.SessionFromContext(r.Context()).Values = map[any]any{}
		w.WriteHeader(http.StatusOK)
	})
	r.HandleFunc("/delete", func(w http.ResponseWriter, r *http.Request) {
		// The gorilla logout idiom: delete the cookie, values untouched.
		webctx.SessionFromContext(r.Context()).Options.MaxAge = -1
		w.WriteHeader(http.StatusOK)
	})
	r.HandleFunc("/whoami", func(w http.ResponseWriter, r *http.Request) {
		v := webctx.SessionFromContext(r.Context()).Values["user"]
		if v != nil {
			_, _ = fmt.Fprint(w, v)
		}
	})
	return r
}

// do sends a GET for path with cookies and returns the response cookies by
// name and the body.
func do(h http.Handler, path string, cookies ...*http.Cookie) (map[string]*http.Cookie, string) {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	out := make(map[string]*http.Cookie)
	for _, c := range w.Result().Cookies() {
		out[c.Name] = c
	}
	return out, w.Body.String()
}

// TestSplitCookieBoundToSession replays a stolen split cookie. It must not
// yield its value without the main session it was issued with, nor after that
// session logs out.
func TestSplitCookieBoundToSession(t *testing.T) {
	t.Parallel()

	h := splitRouter(t)
	splitName := splitCookieName("user")

	login, _ := do(h, "/login")
	main, split := login[defaultSessionName], login[splitName]
	if main == nil || split == nil {
		t.Fatalf("login set cookies %v, want main and split", login)
	}

	// The legitimate pair works.
	if _, body := do(h, "/whoami", main, split); body != "alice" {
		t.Fatalf("whoami with main+split = %q, want alice", body)
	}

	// The split cookie replayed alone yields nothing, and is expired.
	got, body := do(h, "/whoami", split)
	if body != "" {
		t.Fatalf("whoami with split cookie alone = %q, want empty", body)
	}
	if c := got[splitName]; c == nil || c.MaxAge >= 0 {
		t.Errorf("unbound split cookie not expired: %+v", c)
	}

	// After logout, the old split cookie with the new main cookie yields
	// nothing.
	logout, _ := do(h, "/logout", main, split)
	newMain := logout[defaultSessionName]
	if newMain == nil {
		t.Fatal("logout did not set a main cookie")
	}
	if _, body := do(h, "/whoami", newMain, split); body != "" {
		t.Fatalf("whoami after logout with old split = %q, want empty", body)
	}
	// Nor does the old split cookie alone.
	if _, body := do(h, "/whoami", split); body != "" {
		t.Fatalf("whoami after logout with split alone = %q, want empty", body)
	}
}

// TestSplitCookieRotatesOnReset checks that replacing the value map (login)
// rebinds split cookies to a new session ID.
func TestSplitCookieRotatesOnReset(t *testing.T) {
	t.Parallel()

	h := splitRouter(t)
	splitName := splitCookieName("user")

	first, _ := do(h, "/login")
	second, _ := do(h, "/login", first[defaultSessionName], first[splitName])

	// The first login's split cookie does not join the second login's session.
	if _, body := do(h, "/whoami", second[defaultSessionName], first[splitName]); body != "" {
		t.Fatalf("old split joined the re-logged-in session: %q", body)
	}
	if _, body := do(h, "/whoami", second[defaultSessionName], second[splitName]); body != "alice" {
		t.Fatalf("whoami with current pair = %q, want alice", body)
	}
}

// TestSplitCookieExpiredWithSession checks that deleting the main session
// (Options.MaxAge < 0) also expires its split cookies.
func TestSplitCookieExpiredWithSession(t *testing.T) {
	t.Parallel()

	h := splitRouter(t)
	splitName := splitCookieName("user")

	login, _ := do(h, "/login")
	got, _ := do(h, "/delete", login[defaultSessionName], login[splitName])
	if c := got[splitName]; c == nil || c.MaxAge >= 0 {
		t.Fatalf("split cookie not expired with the session: %+v", c)
	}
	if c := got[defaultSessionName]; c == nil || c.MaxAge >= 0 {
		t.Fatalf("main cookie not expired: %+v", c)
	}
}

// TestSplitCookieNotSetWhenAbsent checks that a request that carries no split
// cookie, and has no split value, gets no split Set-Cookie at all.
func TestSplitCookieNotSetWhenAbsent(t *testing.T) {
	t.Parallel()

	h := splitRouter(t)
	got, _ := do(h, "/whoami")
	if c, ok := got[splitCookieName("user")]; ok {
		t.Fatalf("unexpected split Set-Cookie: %+v", c)
	}
	if _, ok := got[defaultSessionName]; !ok {
		t.Fatal("main session cookie not set")
	}
}
