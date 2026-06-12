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
	"fmt"
	"net/http"
	"sort"

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
// otherwise it responds 503 listing only the failing check names:
//
//	{"status":"unavailable","failed":["database"]}
//
// Check error details are logged server-side, never returned in the body, so a
// publicly-reachable probe cannot leak internal hostnames, addresses, or paths.
// A check that panics is treated as failed rather than crashing the probe.
// Checks should be fast and side-effect free (e.g. a ping); wrap slow checks in
// your own caching if probes are frequent.
func ReadyzHandler(checks map[string]func(context.Context) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		failedNames := make([]string, 0, len(checks))
		failedDetails := make(map[string]string)
		for name, check := range checks {
			if err := runCheck(ctx, check); err != nil {
				failedNames = append(failedNames, name)
				failedDetails[name] = err.Error()
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if len(failedNames) == 0 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		sort.Strings(failedNames)

		// Details stay in the logs; the public body carries names only.
		logging.Named(logging.FromContext(ctx), "server.ReadyzHandler").
			Warn("readiness checks failed", "failed", failedDetails)

		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "unavailable",
			"failed": failedNames,
		})
	})
}

// runCheck invokes a readiness check, converting a panic into an error so a
// buggy check degrades to "failed" instead of taking down the probe.
func runCheck(ctx context.Context, check func(context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("check panicked: %v", r)
		}
	}()
	return check(ctx)
}
