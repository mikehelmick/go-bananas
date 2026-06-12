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
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// staticCacheMaxAge is how long static assets are publicly cacheable outside
// dev mode. Cache-busting comes from the renderer's build-ID query string, so a
// week is safe.
const staticCacheMaxAge = 7 * 24 * time.Hour

// ConfigureStaticAssets prepares responses for static assets: it sets cache
// headers (never cached in dev mode so edits show up on reload; publicly
// cacheable for a week otherwise — safe because the renderer's asset tags
// append the build ID as a cache-busting query string, see
// [github.com/mikehelmick/go-bananas/render.WithBuildID]) and rejects directory
// requests (paths ending in "/") with a 404, so a wrapped [http.FileServerFS]
// never emits auto-generated directory listings.
//
// Pair it with a file-serving handler. With templates and assets in an embed.FS
// (the layout the renderer expects), wire it as:
//
//	static := middleware.ConfigureStaticAssets(devMode)
//	r.PathPrefix("/static/").Handler(static(http.FileServerFS(assets)))
//
// For defense in depth, consider serving an [io/fs.Sub] of just the static
// subtree so a routing mistake can never expose templates or other embedded
// files.
func ConfigureStaticAssets(devMode bool) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Refuse directory requests outright: file servers answer them with
			// directory listings, which enumerate every asset.
			if strings.HasSuffix(r.URL.Path, "/") {
				http.NotFound(w, r)
				return
			}

			// Do not cache assets in dev mode.
			if devMode {
				w.Header().Set("Cache-Control", "private, no-cache, max-age=0")
				w.Header().Set("Expires", time.Now().Add(-30*time.Minute).Format(http.TimeFormat))
				w.Header().Set("Vary", "Accept-Encoding")
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", int(staticCacheMaxAge.Seconds())))
			w.Header().Set("Expires", time.Now().Add(staticCacheMaxAge).Format(http.TimeFormat))
			w.Header().Set("Vary", "Accept-Encoding")
			next.ServeHTTP(w, r)
		})
	}
}
