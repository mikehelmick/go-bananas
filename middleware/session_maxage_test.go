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
	"testing"
	"time"

	"github.com/gorilla/sessions"
	sess "github.com/mikehelmick/go-bananas/session"
	"github.com/mikehelmick/go-bananas/webctx"
)

// sessionRequest returns a request whose context carries a fresh session
// holding values.
func sessionRequest(t *testing.T, values map[any]any) (*http.Request, *sessions.Session) {
	t.Helper()

	s, _ := testStore().New(httptest.NewRequest(http.MethodGet, "/", nil), "s")
	s.Values = values
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	return r.WithContext(webctx.WithSession(r.Context(), s)), s
}

func TestCheckSessionMaxAge(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		createdAt time.Time // zero means not set
		maxAge    time.Duration
		expired   bool
	}{
		{name: "expired", createdAt: time.Now().Add(-2 * time.Hour), maxAge: time.Hour, expired: true},
		{name: "fresh", createdAt: time.Now().Add(-10 * time.Minute), maxAge: time.Hour},
		{name: "missing_is_now", maxAge: time.Hour},
		{name: "disabled", createdAt: time.Now().Add(-48 * time.Hour), maxAge: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			values := map[any]any{"user": "alice"}
			r, s := sessionRequest(t, values)
			if !tc.createdAt.IsZero() {
				sess.StoreCreatedAt(s, tc.createdAt)
			}

			var expiredRan, nextRan bool
			onExpired := func(w http.ResponseWriter, _ *http.Request) {
				expiredRan = true
				w.WriteHeader(http.StatusSeeOther)
			}
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextRan = true
				w.WriteHeader(http.StatusOK)
			})

			w := httptest.NewRecorder()
			CheckSessionMaxAge(tc.maxAge, onExpired)(next).ServeHTTP(w, r)

			if expiredRan != tc.expired {
				t.Fatalf("onExpired ran = %v, want %v", expiredRan, tc.expired)
			}
			if nextRan == tc.expired {
				t.Fatalf("next ran = %v, want %v", nextRan, !tc.expired)
			}

			if tc.expired {
				if w.Code != http.StatusSeeOther {
					t.Errorf("status = %d, want 303", w.Code)
				}
				if _, ok := s.Values["user"]; ok {
					t.Error("expired session still holds its values")
				}
				return
			}

			if _, ok := s.Values["user"]; !ok {
				t.Error("live session lost its values")
			}
			if tc.createdAt.IsZero() && tc.maxAge > 0 {
				got := sess.CreatedAt(s)
				if got.IsZero() || time.Since(got) > time.Minute {
					t.Errorf("missing created-at not stamped as now: got %v", got)
				}
			}
		})
	}
}

// TestCheckSessionMaxAgeNoSession checks a request without a session passes.
func TestCheckSessionMaxAgeNoSession(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	CheckSessionMaxAge(time.Hour, func(http.ResponseWriter, *http.Request) {
		t.Error("onExpired ran without a session")
	})(okHandler("ok")).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// loadSaved decodes the session cookie set on w.
func loadSaved(t *testing.T, store sessions.Store, w *httptest.ResponseRecorder) *sessions.Session {
	t.Helper()

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range w.Result().Cookies() {
		if c.Name == defaultSessionName {
			r.AddCookie(c)
		}
	}
	s, err := store.New(r, defaultSessionName)
	if err != nil || s.IsNew {
		t.Fatalf("no saved session cookie (err=%v)", err)
	}
	return s
}

func TestRequireSessionStampsCreatedAt(t *testing.T) {
	t.Parallel()

	store := testStore()
	h := testRenderer(t)

	t.Run("new_session", func(t *testing.T) {
		t.Parallel()

		var seen time.Time
		handler := RequireSession(store, nil, h)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = sess.CreatedAt(webctx.SessionFromContext(r.Context()))
			w.WriteHeader(http.StatusOK)
		}))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

		if seen.IsZero() || time.Since(seen) > time.Minute {
			t.Fatalf("handler saw created-at %v, want about now", seen)
		}
		if got := sess.CreatedAt(loadSaved(t, store, w)); got.Unix() != seen.Unix() {
			t.Fatalf("saved created-at = %v, want %v", got, seen)
		}
	})

	t.Run("existing_kept", func(t *testing.T) {
		t.Parallel()

		// A cookie with an old created-at keeps it across requests.
		old := time.Now().Add(-3 * time.Hour)
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		s, _ := store.New(r, defaultSessionName)
		sess.StoreCreatedAt(s, old)
		prev := httptest.NewRecorder()
		if err := s.Save(r, prev); err != nil {
			t.Fatalf("Save: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		for _, c := range prev.Result().Cookies() {
			req.AddCookie(c)
		}
		w := httptest.NewRecorder()
		RequireSession(store, nil, h)(okHandler("ok")).ServeHTTP(w, req)
		if got := sess.CreatedAt(loadSaved(t, store, w)); got.Unix() != old.Unix() {
			t.Fatalf("created-at = %v, want %v", got, old)
		}
	})

	t.Run("v1_0_cookie_accepted", func(t *testing.T) {
		t.Parallel()

		// A v1.0.x cookie has no created-at. It still loads, keeps its values
		// and gets stamped.
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		s, _ := store.New(r, defaultSessionName)
		s.Values["user"] = "alice"
		prev := httptest.NewRecorder()
		if err := s.Save(r, prev); err != nil {
			t.Fatalf("Save: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		for _, c := range prev.Result().Cookies() {
			req.AddCookie(c)
		}
		var user any
		handler := RequireSession(store, nil, h)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user = webctx.SessionFromContext(r.Context()).Values["user"]
			w.WriteHeader(http.StatusOK)
		}))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if user != "alice" {
			t.Fatalf("legacy session value = %v, want alice", user)
		}
		if got := sess.CreatedAt(loadSaved(t, store, w)); got.IsZero() {
			t.Fatal("legacy session was not stamped with created-at")
		}
	})

	t.Run("reset_in_handler", func(t *testing.T) {
		t.Parallel()

		// A handler that replaces the value map (as on login) still saves a
		// created-at, so the new session's lifetime starts now.
		handler := RequireSession(store, nil, h)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			webctx.SessionFromContext(r.Context()).Values = map[any]any{}
			w.WriteHeader(http.StatusOK)
		}))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if got := sess.CreatedAt(loadSaved(t, store, w)); got.IsZero() || time.Since(got) > time.Minute {
			t.Fatalf("created-at after reset = %v, want about now", got)
		}
	})
}
