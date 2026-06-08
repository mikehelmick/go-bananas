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
	"github.com/mikehelmick/go-bananas/render"
	"github.com/mikehelmick/go-bananas/response"
)

// RequireHeader requires that the request carry the named header with any
// non-empty value, returning 401 otherwise.
func RequireHeader(header string, h *render.Renderer) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if v := r.Header.Get(header); v == "" {
				response.Unauthorized(w, r, h)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireHeaderValues requires that the request carry the named header with a
// value matching one of allowed, returning 401 otherwise.
func RequireHeaderValues(header string, allowed []string, h *render.Renderer) mux.MiddlewareFunc {
	want := make(map[string]struct{}, len(allowed))
	for _, v := range allowed {
		want[v] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			vals := r.Header.Values(header)
			if len(vals) == 0 {
				response.Unauthorized(w, r, h)
				return
			}

			found := false
			for _, v := range vals {
				if _, ok := want[v]; ok {
					found = true
					break
				}
			}

			if !found {
				response.Unauthorized(w, r, h)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireHostHeader requires that the request's Host header match one of allowed
// (case-insensitively), returning 401 otherwise. When stripPort is true, any
// port in the Host header is ignored during comparison.
func RequireHostHeader(allowed []string, h *render.Renderer, stripPort bool) mux.MiddlewareFunc {
	want := make(map[string]struct{}, len(allowed))
	for _, v := range allowed {
		want[strings.ToLower(v)] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := strings.ToLower(r.Host)

			if stripPort {
				if i := strings.Index(host, ":"); i > 0 {
					host = host[0:i]
				}
			}

			if _, ok := want[host]; !ok {
				response.Unauthorized(w, r, h)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
