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
	"context"
	"net/http"
	"strings"
	"sync"
	"unicode"

	"github.com/gorilla/mux"
	"github.com/mikehelmick/go-bananas/logging"
	"github.com/mikehelmick/go-bananas/response"
	"github.com/mikehelmick/go-bananas/webctx"
)

// TemplateConfig holds the common values seeded onto the template map by
// [PopulateTemplateVariables].
type TemplateConfig struct {
	// ServerName is the application name; it is used as the default page title.
	ServerName string

	// ServerEndpoint is the canonical base URL of the server. Set it in
	// production. If it is empty, the endpoint is derived from the request's
	// Host header via [response.RealHostFromRequest], subject to DevMode and
	// AllowedHosts.
	ServerEndpoint string

	// BuildID and BuildTag identify the running build.
	BuildID  string
	BuildTag string

	// DevMode indicates the server is running in development mode. With an
	// empty ServerEndpoint, dev mode uses the derived endpoint as is.
	DevMode bool

	// AllowedHosts lists the hosts an empty ServerEndpoint may be derived from
	// outside dev mode. Entries are bare hosts, optionally with a port (for
	// example "app.example.com" or "app.example.com:8443"); empty entries and
	// entries containing "/" (such as "https://...") or whitespace are skipped
	// with a warning. Each entry is compared
	// case-insensitively against r.Host only (forwarding headers and the
	// request URL's scheme are ignored), and a match yields "https://" plus
	// the entry. When the request's host is not listed, the endpoint falls back
	// to "https://" plus the first entry. When the list is empty, the endpoint
	// is "" and a warning is logged once, because a client-chosen Host header
	// must not end up in rendered links.
	//
	// Its semantics differ from [WithAllowedHosts] (for [SecureHeaders]),
	// which usually lists the same hosts: this field reads r.Host only and
	// falls back to the first entry instead of rejecting, while
	// WithAllowedHosts is proxy-aware ([WithTrustedHostHeaders]) and rejects
	// an unlisted host with 400 Bad Request. Use WithAllowedHosts to reject
	// spoofed hosts; this field only keeps them out of rendered links.
	AllowedHosts []string

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
	cfg.AllowedHosts = validAllowedHosts(cfg.AllowedHosts)
	var warnOnce sync.Once

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			endpoint, ok := serverEndpoint(r, cfg)
			if !ok {
				warnOnce.Do(func() { warnDerivedEndpoint(ctx) })
			}

			m := webctx.TemplateMapFromContext(ctx)
			m["server"] = cfg.ServerName
			m["serverEndpoint"] = endpoint
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

// serverEndpoint returns the endpoint for r under cfg. It reports false when no
// endpoint is usable: outside dev mode, with no ServerEndpoint and no
// AllowedHosts. In that case the endpoint is "".
func serverEndpoint(r *http.Request, cfg TemplateConfig) (string, bool) {
	if cfg.ServerEndpoint != "" {
		return strings.TrimSuffix(cfg.ServerEndpoint, "/"), true
	}

	if cfg.DevMode {
		return response.RealHostFromRequest(r), true
	}

	if len(cfg.AllowedHosts) == 0 {
		return "", false
	}

	for _, h := range cfg.AllowedHosts {
		if strings.EqualFold(r.Host, h) {
			return "https://" + h, true
		}
	}
	return "https://" + cfg.AllowedHosts[0], true
}

// validAllowedHosts returns a copy of hosts without entries that are empty or
// contain "/" (which covers "://") or whitespace, logging a warning for each
// one skipped.
func validAllowedHosts(hosts []string) []string {
	var out []string
	for _, h := range hosts {
		if h == "" || strings.Contains(h, "/") || strings.ContainsFunc(h, unicode.IsSpace) {
			logging.Named(logging.DefaultLogger(), "middleware.PopulateTemplateVariables").
				Warn("skipping invalid TemplateConfig.AllowedHosts entry; use a bare host such as app.example.com",
					"entry", h)
			continue
		}
		out = append(out, h)
	}
	return out
}

// warnDerivedEndpoint logs that serverEndpoint was left empty because the only
// source was the untrusted Host header.
func warnDerivedEndpoint(ctx context.Context) {
	logger := logging.Named(logging.FromContext(ctx), "middleware.PopulateTemplateVariables")
	logger.Warn("serverEndpoint derived from Host header is not trusted outside dev mode, so it is empty; " +
		"set TemplateConfig.ServerEndpoint (or AllowedHosts) in production")
}
