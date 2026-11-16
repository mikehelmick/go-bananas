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
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mikehelmick/go-bananas/logging"
	"github.com/unrolled/secure"
)

// warnUnpinnedHostHeaders logs a warning when trusted host headers are set
// but neither AllowedHosts nor SSLHost constrains the host, so a forged header
// controls the HTTPS redirect target.
func warnUnpinnedHostHeaders(logger *slog.Logger, opts secure.Options) {
	if len(opts.HostsProxyHeaders) > 0 && len(opts.AllowedHosts) == 0 && opts.SSLHost == "" {
		logger.Warn("trusted host headers set without WithAllowedHosts or WithSSLHost; "+
			"clients can choose the HTTPS redirect host",
			"headers", opts.HostsProxyHeaders)
	}
}

// ServerType describes the kind of responses a server emits, which tweaks a few
// secure-header defaults (for example, whether to deny framing).
type ServerType string

const (
	// ServerTypeHTML is for servers that render HTML pages; it enables
	// clickjacking protection (X-Frame-Options: DENY).
	ServerTypeHTML ServerType = "html"
	// ServerTypeAPI is for servers that emit machine-readable responses.
	ServerTypeAPI ServerType = "api"
)

// SecureHeaders installs a sensible set of security-related response headers
// (HSTS, nosniff, referrer policy, and, for HTML servers, frame denial). When
// devMode is true, HTTPS redirects and HSTS are relaxed for local development.
//
// Outside dev mode, plain-HTTP requests are redirected to HTTPS on the
// request's Host. X-Forwarded-Proto: https is trusted to mark a request as
// HTTPS (so apps behind a TLS-terminating proxy don't redirect forever), but
// X-Forwarded-Host is not; opt in with [WithTrustedHostHeaders] together with
// [WithAllowedHosts]. HSTS preload is off unless [WithHSTSPreload] is given.
// Options are applied after the defaults.
//
// It deliberately does NOT set a Content-Security-Policy: a useful CSP is
// highly application-specific. Add one with [ContentSecurityPolicy], combined
// with [ProcessNonce] for per-request nonces on trusted inline scripts/styles.
func SecureHeaders(devMode bool, serverType ServerType, opts ...SecureOption) mux.MiddlewareFunc {
	cfg := secureConfig{opts: secure.Options{
		BrowserXssFilter:     false,
		ContentTypeNosniff:   true,
		FrameDeny:            serverType == ServerTypeHTML,
		IsDevelopment:        devMode,
		ReferrerPolicy:       "strict-origin-when-cross-origin",
		SSLProxyHeaders:      map[string]string{"X-Forwarded-Proto": "https"},
		SSLRedirect:          !devMode,
		STSIncludeSubdomains: true,
		STSSeconds:           315360000,
	}}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	warnUnpinnedHostHeaders(logging.Named(logging.DefaultLogger(), "middleware.SecureHeaders"), cfg.opts)

	s := secure.New(cfg.opts)
	// A host outside the allowlist is a client error; unrolled's default is 500.
	s.SetBadHostHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
	}))
	return s.Handler
}
