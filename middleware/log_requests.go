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
	"time"

	"github.com/gorilla/mux"
	"github.com/mikehelmick/go-bananas/logging"
)

// LogRequests emits one structured Info log line per request with the method,
// path, response status, bytes written, and duration. It logs through
// [logging.FromContext], so when installed after [PopulateLogger] each line is
// automatically tagged with the request (and trace) ID.
//
//	r.Use(middleware.PopulateLogger(logging.DefaultLogger()))
//	r.Use(middleware.LogRequests())
func LogRequests() mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{w: w}

			next.ServeHTTP(rec, r)

			logging.Named(logging.FromContext(r.Context()), "middleware.LogRequests").
				Info("request",
					"method", r.Method,
					"path", r.URL.Path,
					"status", rec.statusOr200(),
					"bytes", rec.bytes,
					"duration_ms", time.Since(start).Milliseconds(),
				)
		})
	}
}

// statusRecorder is an http.ResponseWriter that records the status code and
// body size written through it.
type statusRecorder struct {
	w      http.ResponseWriter
	status int
	bytes  int64
}

func (r *statusRecorder) Header() http.Header {
	return r.w.Header()
}

func (r *statusRecorder) WriteHeader(c int) {
	if r.status == 0 {
		r.status = c
	}
	r.w.WriteHeader(c)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.w.Write(b)
	r.bytes += int64(n)
	return n, err
}

// statusOr200 returns the recorded status, defaulting to 200 when the handler
// never wrote a header or body (net/http sends 200 in that case).
func (r *statusRecorder) statusOr200() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

// Unwrap exposes the underlying writer so http.ResponseController (and the
// optional interfaces like http.Flusher) keep working through this wrapper.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.w
}
