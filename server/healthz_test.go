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
			"beta":  func(context.Context) error { return errors.New("connection refused") },
		})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))

		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", w.Code)
		}

		var body struct {
			Status string            `json:"status"`
			Failed map[string]string `json:"failed"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("invalid JSON body %q: %v", w.Body.String(), err)
		}
		if body.Status != "unavailable" {
			t.Errorf("status field = %q", body.Status)
		}
		if body.Failed["beta"] != "connection refused" {
			t.Errorf("failed = %v", body.Failed)
		}
		if _, ok := body.Failed["alpha"]; ok {
			t.Error("passing check must not be reported as failed")
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
