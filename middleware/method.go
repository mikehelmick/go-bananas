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
)

// formKeyMethod is the form field used to override the HTTP method.
const formKeyMethod = "_method"

// MutateMethod lets HTML forms emulate verbs other than GET and POST by
// supplying a "_method" form value (e.g. PATCH or DELETE), which is then used as
// the request method before routing. It must be installed very early in the
// chain, before the router matches a route.
func MutateMethod() mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if method := strings.ToUpper(r.FormValue(formKeyMethod)); method != "" {
				r.Method = method
			}

			next.ServeHTTP(w, r)
		})
	}
}
