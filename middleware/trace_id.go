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
	"strings"

	"github.com/gorilla/mux"
	"github.com/mikehelmick/go-bananas/internal/util"
	"github.com/mikehelmick/go-bananas/webctx"
)

// TraceHeader is the request header carrying distributed-trace context. It
// matches the header injected by Google Cloud load balancers, but any upstream
// that sets it will be honored.
const TraceHeader = "X-Cloud-Trace-Context"

// PopulateTraceID extracts the trace ID from the trace header (the portion
// before the first "/") and stores it on the context, if present and not already
// set. Install it before [PopulateLogger].
func PopulateTraceID() mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			if existing := webctx.TraceIDFromContext(ctx); existing == "" {
				if v := r.Header.Get(TraceHeader); v != "" {
					parts := strings.Split(v, "/")
					if len(parts) > 0 && len(parts[0]) > 0 {
						ctx = webctx.WithTraceID(ctx, util.TrimSpace(parts[0]))
						r = r.Clone(ctx)
					}
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}
