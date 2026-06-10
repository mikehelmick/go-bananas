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
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mikehelmick/go-bananas/logging"
	"github.com/mikehelmick/go-bananas/webctx"
)

// PopulateLogger stores a request-scoped logger on the context, enriched with
// the request ID (and trace ID, if present) so every log line within the request
// can be correlated. Install it after [PopulateRequestID] and [PopulateTraceID].
//
// If an upstream middleware already placed a non-default logger on the context,
// that logger is used as the base instead of originalLogger.
func PopulateLogger(originalLogger *slog.Logger) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			logger := originalLogger

			// Prefer an existing non-default logger from the context. The strict
			// identity check against DefaultLogger is intentional.
			if existing := logging.FromContext(ctx); existing != logging.DefaultLogger() {
				logger = existing
			}

			if id := webctx.RequestIDFromContext(ctx); id != "" {
				logger = logger.With("request_id", id)
			}
			if id := webctx.TraceIDFromContext(ctx); id != "" {
				logger = logger.With("trace_id", id)
			}

			ctx = logging.WithLogger(ctx, logger)
			r = r.Clone(ctx)

			next.ServeHTTP(w, r)
		})
	}
}
