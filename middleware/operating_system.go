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
	"github.com/mikehelmick/go-bananas/webctx"
)

// AddOperatingSystemFromUserAgent inspects the request's User-Agent and stores
// the inferred client operating system ([webctx.OSIOS], [webctx.OSAndroid], or
// [webctx.OSUnknown]) on the context.
func AddOperatingSystemFromUserAgent() mux.MiddlewareFunc {
	userAgents := map[string]webctx.OS{
		"darwin": webctx.OSIOS,
		"iphone": webctx.OSIOS,
		"dalvik": webctx.OSAndroid,
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			agent := strings.ToLower(r.UserAgent())

			osToSet := webctx.OSUnknown
			for k, os := range userAgents {
				if strings.Contains(agent, k) {
					osToSet = os
					break
				}
			}

			ctx = webctx.WithOperatingSystem(ctx, osToSet)
			r = r.Clone(ctx)

			next.ServeHTTP(w, r)
		})
	}
}
