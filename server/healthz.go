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
	"net/http"

	"github.com/mikehelmick/go-bananas/logging"
)

// HealthzHandler returns a liveness handler that always responds
// 200 {"status":"ok"}. Liveness means "the process is up"; for dependency
// checks use [ReadyzHandler].
func HealthzHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
}

// ReadyzHandler returns a readiness handler that runs the named checks with the
// request context. If every check returns nil it responds 200 {"status":"ok"};
// otherwise it responds 503 with the failing check names and their errors:
//
//	{"status":"unavailable","failed":{"database":"connection refused"}}
//
// Checks should be fast and side-effect free (e.g. a ping); wrap slow checks in
// your own caching if probes are frequent.
func ReadyzHandler(checks map[string]func(context.Context) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		failed := make(map[string]string)
		for name, check := range checks {
			if err := check(ctx); err != nil {
				failed[name] = err.Error()
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if len(failed) == 0 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}

		logging.Named(logging.FromContext(ctx), "server.ReadyzHandler").
			Warn("readiness checks failed", "failed", failed)

		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "unavailable",
			"failed": failed,
		})
	})
}
