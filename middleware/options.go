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

import "github.com/unrolled/secure"

// secureConfig holds the settings for [SecureHeaders]. It wraps the
// unrolled/secure options so that type is not part of this package's API.
type secureConfig struct {
	opts secure.Options
}

// SecureOption customizes [SecureHeaders]. Options are applied after the
// defaults, in order.
type SecureOption func(*secureConfig)

// WithTrustedHostHeaders makes [SecureHeaders] take the request host from the
// first non-empty header in headers (for example "X-Forwarded-Host") instead of
// the Host header. That host is used for the HTTPS redirect, so only trust a
// header that your proxy always sets or strips.
//
// It must be combined with [WithAllowedHosts] (or [WithSSLHost]); otherwise a
// client that reaches the server with a forged header controls the redirect
// target.
func WithTrustedHostHeaders(headers ...string) SecureOption {
	return func(c *secureConfig) {
		c.opts.HostsProxyHeaders = append([]string(nil), headers...)
	}
}

// WithHSTSPreload adds the "preload" directive to Strict-Transport-Security.
// Only enable it once the domain, and every subdomain, is HTTPS-only and you
// intend to submit it to the browser preload list; removal is slow.
func WithHSTSPreload() SecureOption {
	return func(c *secureConfig) {
		c.opts.STSPreload = true
	}
}

// WithSSLHost sets the host used in HTTP-to-HTTPS redirects, instead of the
// request's host. Use it to pin redirects to the canonical host.
func WithSSLHost(host string) SecureOption {
	return func(c *secureConfig) {
		c.opts.SSLHost = host
	}
}

// WithAllowedHosts rejects requests whose host (after [WithTrustedHostHeaders],
// if set) is not one of hosts, compared case-insensitively, with 400 Bad
// Request. Entries must match the host exactly, including any port. The check
// is skipped in dev mode.
func WithAllowedHosts(hosts ...string) SecureOption {
	return func(c *secureConfig) {
		c.opts.AllowedHosts = append([]string(nil), hosts...)
	}
}

// DefaultCSRFMaxBodyBytes is the default request body limit [HandleCSRF]
// applies when it must read the token from the form (1 MiB).
const DefaultCSRFMaxBodyBytes int64 = 1 << 20

// csrfConfig holds the settings for [HandleCSRF].
type csrfConfig struct {
	maxBodyBytes int64
}

// CSRFOption customizes [HandleCSRF].
type CSRFOption func(*csrfConfig)

// WithCSRFMaxBodyBytes sets the request body limit [HandleCSRF] applies to
// non-safe requests without a [CSRFHeaderField] header, whose token must be
// read from the form. The limit covers the whole body, so it also applies to
// handlers that read it. A request whose form exceeds it gets 413. Requests
// carrying the header token are not limited by HandleCSRF. Values less than or
// equal to zero keep [DefaultCSRFMaxBodyBytes].
//
// To accept larger uploads, mount the upload routes on a subrouter with its own
// HandleCSRF and a higher limit, rather than raising it for every route.
func WithCSRFMaxBodyBytes(n int64) CSRFOption {
	return func(c *csrfConfig) {
		if n > 0 {
			c.maxBodyBytes = n
		}
	}
}
