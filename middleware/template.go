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
	"github.com/mikehelmick/go-bananas/response"
	"github.com/mikehelmick/go-bananas/webctx"
)

// TemplateConfig holds the common values seeded onto the template map by
// [PopulateTemplateVariables].
type TemplateConfig struct {
	// ServerName is the application name; it is used as the default page title.
	ServerName string

	// ServerEndpoint is the canonical base URL of the server. If empty, it is
	// derived from each request via [response.RealHostFromRequest].
	ServerEndpoint string

	// BuildID and BuildTag identify the running build.
	BuildID  string
	BuildTag string

	// DevMode indicates the server is running in development mode.
	DevMode bool

	// Extra holds any additional application-specific values to expose to every
	// template (for example, feature flags). Keys here are copied onto the
	// template map verbatim.
	Extra map[string]any
}

// PopulateTemplateVariables seeds the template map with common values (server
// name, endpoint, build identifiers, dev-mode flag, and any Extra values) and
// bootstraps the map so later middleware can add to it. Install it after the
// session middleware and before handlers render.
func PopulateTemplateVariables(cfg TemplateConfig) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			m := webctx.TemplateMapFromContext(ctx)
			m["server"] = cfg.ServerName
			m["serverEndpoint"] = serverEndpoint(r, cfg.ServerEndpoint)
			m["title"] = cfg.ServerName
			m["buildID"] = cfg.BuildID
			m["buildTag"] = cfg.BuildTag
			m["devMode"] = cfg.DevMode
			for k, v := range cfg.Extra {
				m[k] = v
			}

			ctx = webctx.WithTemplateMap(ctx, m)
			r = r.Clone(ctx)

			next.ServeHTTP(w, r)
		})
	}
}

// serverEndpoint returns the configured endpoint, or derives one from the
// request when none is configured.
func serverEndpoint(r *http.Request, given string) string {
	if given != "" {
		return strings.TrimSuffix(given, "/")
	}
	return response.RealHostFromRequest(r)
}
