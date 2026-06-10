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
	"github.com/gorilla/mux"
	"github.com/unrolled/secure"
)

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
// It deliberately does NOT set a Content-Security-Policy: a useful CSP is highly
// application-specific. Applications should add their own CSP header — combine it
// with [ProcessNonce], which supplies a per-request, server-generated nonce for
// trusted inline scripts/styles.
func SecureHeaders(devMode bool, serverType ServerType) mux.MiddlewareFunc {
	options := secure.Options{
		BrowserXssFilter:     false,
		ContentTypeNosniff:   true,
		FrameDeny:            serverType == ServerTypeHTML,
		HostsProxyHeaders:    []string{"X-Forwarded-Host"},
		IsDevelopment:        devMode,
		ReferrerPolicy:       "strict-origin-when-cross-origin",
		SSLProxyHeaders:      map[string]string{"X-Forwarded-Proto": "https"},
		SSLRedirect:          !devMode,
		STSIncludeSubdomains: true,
		STSPreload:           true,
		STSSeconds:           315360000,
	}

	return secure.New(options).Handler
}
