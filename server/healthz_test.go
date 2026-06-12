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

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthzHandler(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	HealthzHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := strings.TrimSpace(w.Body.String()); got != `{"status":"ok"}` {
		t.Errorf("body = %q", got)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q", ct)
	}
}

func TestReadyzHandler(t *testing.T) {
	t.Parallel()

	t.Run("all_pass", func(t *testing.T) {
		t.Parallel()

		h := ReadyzHandler(map[string]func(context.Context) error{
			"alpha": func(context.Context) error { return nil },
			"beta":  func(context.Context) error { return nil },
		})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
	})

	t.Run("one_fails", func(t *testing.T) {
		t.Parallel()

		h := ReadyzHandler(map[string]func(context.Context) error{
			"alpha": func(context.Context) error { return nil },
			"beta":  func(context.Context) error { return errors.New("dial tcp 10.0.3.12:5432: connection refused") },
		})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))

		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", w.Code)
		}

		var body struct {
			Status string   `json:"status"`
			Failed []string `json:"failed"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("invalid JSON body %q: %v", w.Body.String(), err)
		}
		if body.Status != "unavailable" {
			t.Errorf("status field = %q", body.Status)
		}
		if len(body.Failed) != 1 || body.Failed[0] != "beta" {
			t.Errorf("failed = %v, want [beta]", body.Failed)
		}
		// The raw error detail (internal addresses etc.) must never reach the
		// public body — only the check name.
		if strings.Contains(w.Body.String(), "10.0.3.12") {
			t.Errorf("body leaked check error detail: %s", w.Body.String())
		}
	})

	t.Run("panicking_check_is_failed", func(t *testing.T) {
		t.Parallel()

		h := ReadyzHandler(map[string]func(context.Context) error{
			"boom": func(context.Context) error { panic("nil pointer") },
			"ok":   func(context.Context) error { return nil },
		})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))

		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503 (panic must degrade to failed, not crash)", w.Code)
		}
		if !strings.Contains(w.Body.String(), `"boom"`) {
			t.Errorf("body = %s, want boom listed as failed", w.Body.String())
		}
	})

	t.Run("no_checks", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		ReadyzHandler(nil).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
	})
}
