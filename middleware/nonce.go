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
	"encoding/base64"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mikehelmick/go-bananas/internal/util"
	"github.com/mikehelmick/go-bananas/logging"
	"github.com/mikehelmick/go-bananas/webctx"
)

// ProcessNonce generates a fresh, cryptographically-random Content-Security-
// Policy nonce per request and stores it on the context, where templates can
// read it (via [webctx.NonceFromContext]) to mark trusted inline scripts/styles.
//
// The nonce is generated server-side; it is deliberately NOT read from a request
// header, because a client-controlled nonce would let an attacker predict it and
// defeat the CSP. To take effect, emit a Content-Security-Policy header that
// references the same nonce — [ContentSecurityPolicy] does this via its
// [CSPNoncePlaceholder] when installed after this middleware.
func ProcessNonce() mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			b, err := util.RandomBytes(16)
			if err != nil {
				// crypto/rand essentially never fails; if it does, proceed without a
				// nonce rather than failing the request.
				logging.FromContext(ctx).Error("failed to generate CSP nonce", "error", err)
			} else {
				ctx = webctx.WithNonce(ctx, base64.StdEncoding.EncodeToString(b))
				r = r.Clone(ctx)
			}

			next.ServeHTTP(w, r)
		})
	}
}
