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

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/mikehelmick/go-bananas/render"
	"github.com/mikehelmick/go-bananas/response"
	"github.com/mikehelmick/go-bananas/webctx"
)

// PopulateRequestID assigns a random UUID request ID to the context if one is
// not already present, so it can be logged and surfaced in templates. Install it
// before [PopulateLogger].
func PopulateRequestID(h *render.Renderer) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			if existing := webctx.RequestIDFromContext(ctx); existing == "" {
				u, err := uuid.NewRandom()
				if err != nil {
					response.InternalError(w, r, h, err)
					return
				}

				ctx = webctx.WithRequestID(ctx, u.String())
				r = r.Clone(ctx)
			}

			next.ServeHTTP(w, r)
		})
	}
}
