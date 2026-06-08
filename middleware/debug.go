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

	"github.com/gorilla/mux"
)

const (
	// HeaderDebug is the request header that, when present with any value,
	// triggers debug response headers.
	HeaderDebug = "x-debug"
	// HeaderDebugBuildID is the response header carrying the build ID.
	HeaderDebugBuildID = "x-build-id"
	// HeaderDebugBuildTag is the response header carrying the build tag.
	HeaderDebugBuildTag = "x-build-tag"
)

// ProcessDebug echoes the provided build identifiers in response headers when
// the request includes the "X-Debug" header with any value. This aids debugging
// without exposing build information to ordinary clients.
func ProcessDebug(buildID, buildTag string) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get(HeaderDebug) != "" {
				w.Header().Set(HeaderDebugBuildID, buildID)
				w.Header().Set(HeaderDebugBuildTag, buildTag)
			}

			next.ServeHTTP(w, r)
		})
	}
}
