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
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mikehelmick/go-bananas/logging"
	"github.com/mikehelmick/go-bananas/render"
	"github.com/mikehelmick/go-bananas/response"
)

// Recovery recovers from panics in downstream handlers, logging the panic in a
// structured format and returning a 500 to the client so the server keeps
// running. It is typically the outermost middleware in the chain.
func Recovery(h *render.Renderer) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			logger := logging.Named(logging.FromContext(r.Context()), "middleware.Recovery")

			defer func() {
				if p := recover(); p != nil {
					logger.Error("http handler panic", "panic", p)
					response.InternalError(w, r, h, fmt.Errorf("panic: %v", p))
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
