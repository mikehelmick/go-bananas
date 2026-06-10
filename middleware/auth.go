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
	"github.com/mikehelmick/go-bananas/logging"
	"github.com/mikehelmick/go-bananas/render"
	"github.com/mikehelmick/go-bananas/response"
	"github.com/mikehelmick/go-bananas/webctx"
)

// Authenticator authenticates an HTTP request. It is the single seam through
// which applications plug in an identity provider (OIDC, an API key scheme, a
// signed token, etc.) without the framework depending on any of them.
//
// Implementations typically read a session, cookie, or header populated by an
// earlier login flow. The returned principal is opaque to the framework: its
// concrete type is the application's own user/session model, retrievable later
// with [webctx.PrincipalFromContext].
type Authenticator interface {
	// Authenticate returns the authenticated principal for the request, or
	// (nil, nil) if the request is anonymous (no credentials presented). A
	// non-nil error indicates the authentication attempt itself failed (for
	// example, an identity provider was unreachable) and results in a 500.
	Authenticate(r *http.Request) (principal any, err error)
}

// RequireAuthenticated rejects anonymous requests. It calls a's Authenticate: a
// non-nil error renders a 500; a nil principal renders a 401; otherwise the
// principal is stored on the context (see [webctx.PrincipalFromContext]) and the
// request proceeds.
//
// The plumbing an OIDC flow needs is provided by other middleware in this
// package: [RequireSession] for token/claims storage, [HandleCSRF],
// [SecureHeaders], and [CheckSessionIdleNoAuth] for idle expiry.
func RequireAuthenticated(a Authenticator, h *render.Renderer) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			principal, err := a.Authenticate(r)
			if err != nil {
				logging.Named(logging.FromContext(ctx), "middleware.RequireAuthenticated").
					Error("authenticator failed", "error", err)
				response.InternalError(w, r, h, err)
				return
			}
			if principal == nil {
				response.Unauthorized(w, r, h)
				return
			}

			ctx = webctx.WithPrincipal(ctx, principal)
			r = r.Clone(ctx)

			next.ServeHTTP(w, r)
		})
	}
}
