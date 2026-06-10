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
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mikehelmick/go-bananas/internal/util"
	"github.com/mikehelmick/go-bananas/logging"
	"github.com/mikehelmick/go-bananas/render"
	"github.com/mikehelmick/go-bananas/response"
	sess "github.com/mikehelmick/go-bananas/session"
	"github.com/mikehelmick/go-bananas/webctx"
)

const (
	// CSRFHeaderField is the header carrying the CSRF token.
	CSRFHeaderField = "X-CSRF-Token"

	// CSRFFormField is the form field carrying the CSRF token.
	CSRFFormField = "csrf_token"
	// CSRFFormFieldTemplate renders a hidden CSRF form input.
	CSRFFormFieldTemplate = `<input type="hidden" name="%s" value="%s" />`

	// CSRFMetaTagName is the meta tag name used by JavaScript to read the token.
	CSRFMetaTagName = "csrf-token"
	// CSRFMetaTagTemplate renders the CSRF meta tag.
	CSRFMetaTagTemplate = `<meta name="%s" content="%s">`

	// TokenLength is the length of the CSRF token, in bytes.
	TokenLength = 64
)

// CSRF-related sentinel errors.
const (
	ErrMissingExistingToken = Error("missing existing csrf token in session")
	ErrMissingIncomingToken = Error("missing csrf token in request")
	ErrInvalidToken         = Error("invalid csrf token")
)

// Error is a constant, comparable error type for this package.
type Error string

// Error implements the error interface.
func (e Error) Error() string { return string(e) }

// HandleCSRF manages per-request CSRF tokens. It reads (or generates and stores)
// a token on the session, exposes masked token helpers on the template map
// ("csrfToken", "csrfField", "csrfMeta", "csrfHeaderField", "csrfMetaTagName"),
// and, for mutating methods, verifies the incoming token. It must be installed
// after [RequireSession].
func HandleCSRF(h *render.Renderer) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			logger := logging.Named(logging.FromContext(ctx), "middleware.HandleCSRF")

			session := webctx.SessionFromContext(ctx)
			if session == nil {
				response.MissingSession(w, r, h)
				return
			}

			// Get the existing CSRF token from the session, if one exists.
			existingToken := sess.CSRFToken(session)

			// If the existing token is invalid or missing, generate and store a new
			// one.
			if l := len(existingToken); l != TokenLength {
				newToken, err := util.RandomBytes(TokenLength)
				if err != nil {
					response.InternalError(w, r, h, err)
					return
				}
				sess.StoreCSRFToken(session, newToken)

				// The only way to reach here is a missing or invalid existing token, so
				// it is not safe to hand to templates/helpers; use the new token. It
				// will fail validation below if applicable.
				existingToken = newToken
			}

			// Expose CSRF helpers on the template map.
			masked, err := mask(existingToken)
			if err != nil {
				response.InternalError(w, r, h, err)
				return
			}
			m := webctx.TemplateMapFromContext(ctx)
			m["csrfHeaderField"] = CSRFHeaderField
			m["csrfMetaTagName"] = CSRFMetaTagName
			m["csrfToken"] = template.HTML(masked)
			m["csrfField"] = template.HTML(fmt.Sprintf(CSRFFormFieldTemplate, CSRFFormField, masked))
			m["csrfMeta"] = template.HTML(fmt.Sprintf(CSRFMetaTagTemplate, CSRFMetaTagName, masked))
			ctx = webctx.WithTemplateMap(ctx, m)
			r = r.Clone(ctx)

			// Only mutating methods need CSRF verification.
			if canSkipCSRFCheck(r) {
				next.ServeHTTP(w, r)
				return
			}

			incomingToken, err := tokenFromRequest(r)
			if err != nil {
				// Warn but don't fail here; the comparison below returns the correct
				// response. This is almost always user error with the provided token.
				logger.Warn("invalid csrf token from request", "error", err)
			}

			if subtle.ConstantTimeCompare(existingToken, incomingToken) != 1 {
				response.Unauthorized(w, r, h)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// tokenFromRequest extracts the token from the header, then the form, then the
// multipart form. It returns [ErrMissingIncomingToken] if absent or
// [ErrInvalidToken] if present but undecodable.
func tokenFromRequest(r *http.Request) ([]byte, error) {
	str := r.Header.Get(CSRFHeaderField)

	if str == "" {
		str = r.PostFormValue(CSRFFormField)
	}

	if str == "" && r.MultipartForm != nil {
		if vals, ok := r.MultipartForm.Value[CSRFFormField]; ok && len(vals) > 0 {
			str = vals[0]
		}
	}

	if str == "" {
		return nil, ErrMissingIncomingToken
	}

	b, err := base64.RawURLEncoding.DecodeString(str)
	if err != nil {
		return nil, ErrInvalidToken
	}

	raw, err := unmask(b)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// canSkipCSRFCheck reports whether the request method is safe and does not need
// CSRF validation.
func canSkipCSRFCheck(r *http.Request) bool {
	return r.Method == http.MethodGet || r.Method == http.MethodHead ||
		r.Method == http.MethodOptions || r.Method == http.MethodTrace
}

// mask XORs the raw token with random bytes and prepends those random bytes to
// the result, producing a fresh-looking token on each render (defeating BREACH).
func mask(raw []byte) (string, error) {
	b, err := util.RandomBytes(len(raw))
	if err != nil {
		return "", fmt.Errorf("maskToken: %w", err)
	}

	b = append(b, xor(b, raw)...)
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// unmask reverses [mask], recovering the original token. It errors if the slice
// is too small or of odd length.
func unmask(raw []byte) ([]byte, error) {
	if len(raw) < 2 || len(raw)%2 != 0 {
		return nil, ErrInvalidToken
	}

	half := len(raw) / 2
	return xor(raw[half:], raw[:half]), nil
}

// xor returns the XOR of x and y. len(y) must be >= len(x).
func xor(x, y []byte) []byte {
	result := make([]byte, len(x))
	for i := 0; i < len(x); i++ {
		result[i] = x[i] ^ y[i]
	}
	return result
}
