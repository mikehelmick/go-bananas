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
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mikehelmick/go-bananas/logging"
)

func TestLogRequests(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		handler    http.Handler
		wantStatus float64 // slog JSON numbers decode as float64
		wantBytes  float64
	}{
		{
			name: "explicit_status_and_body",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTeapot)
				_, _ = w.Write([]byte("short"))
			}),
			wantStatus: 418,
			wantBytes:  5,
		},
		{
			name:       "implicit_200_no_body",
			handler:    http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			wantStatus: 200,
			wantBytes:  0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Capture structured output from a request-scoped JSON logger.
			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))

			req := httptest.NewRequest(http.MethodGet, "/some/path", nil)
			req = req.WithContext(logging.WithLogger(req.Context(), logger))

			LogRequests()(tc.handler).ServeHTTP(httptest.NewRecorder(), req)

			var entry map[string]any
			if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
				t.Fatalf("expected one JSON log line, got %q: %v", buf.String(), err)
			}
			if entry["msg"] != "request" {
				t.Errorf("msg = %v, want request", entry["msg"])
			}
			if entry["method"] != http.MethodGet || entry["path"] != "/some/path" {
				t.Errorf("method/path = %v %v", entry["method"], entry["path"])
			}
			if entry["status"] != tc.wantStatus {
				t.Errorf("status = %v, want %v", entry["status"], tc.wantStatus)
			}
			if entry["bytes"] != tc.wantBytes {
				t.Errorf("bytes = %v, want %v", entry["bytes"], tc.wantBytes)
			}
			if _, ok := entry["duration_ms"]; !ok {
				t.Error("missing duration_ms")
			}
		})
	}
}
