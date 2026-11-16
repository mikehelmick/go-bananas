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
	"errors"
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
//
// GET, HEAD and OPTIONS are treated as safe and skip verification; every other
// method, including TRACE, needs a token. A token in the [CSRFHeaderField]
// header is checked without touching the body. Otherwise the token is read from
// the urlencoded or multipart form: the body is first limited to
// [DefaultCSRFMaxBodyBytes] (see [WithCSRFMaxBodyBytes]), a form over the limit
// gets 413 Request Entity Too Large, and the limit stays on the body the
// handler sees. Multipart temporary files created while parsing are removed
// when the request finishes.
func HandleCSRF(h *render.Renderer, opts ...CSRFOption) mux.MiddlewareFunc {
	cfg := csrfConfig{maxBodyBytes: DefaultCSRFMaxBodyBytes}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

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

			// The token must come from the form, so cap the body before parsing
			// it. Requests carrying the header token are not capped here, so JSON
			// and streaming bodies are left to the handler.
			if r.Header.Get(CSRFHeaderField) == "" && r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, cfg.maxBodyBytes)
			}

			preParsed := r.MultipartForm != nil
			incomingToken, err := tokenFromRequest(r)
			// Remove any multipart temp files this middleware created, whatever
			// the outcome. An earlier parse is left to whoever made it.
			if !preParsed && r.MultipartForm != nil {
				form := r.MultipartForm
				defer func() { _ = form.RemoveAll() }()
			}
			if isTooLarge(err) {
				http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
				return
			}
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
// multipart form. It returns [ErrMissingIncomingToken] if absent,
// [ErrInvalidToken] if present but undecodable, or the [*http.MaxBytesError]
// if the body exceeds the limit set on r.Body.
func tokenFromRequest(r *http.Request) ([]byte, error) {
	str := r.Header.Get(CSRFHeaderField)

	if str == "" {
		if err := parseCSRFForm(r); err != nil {
			return nil, err
		}
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

// csrfMultipartMemory is the in-memory limit passed to ParseMultipartForm. With
// a body cap at or below it, multipart forms never spill to temporary files;
// above it, [HandleCSRF] removes the temporary files when the request ends.
const csrfMultipartMemory = 32 << 20

// parseCSRFForm parses the urlencoded or multipart form. It returns only a
// [*http.MaxBytesError]; other parse errors are ignored and surface as a
// missing token. ParseForm runs first because ParseMultipartForm discards its
// error for non-multipart bodies.
func parseCSRFForm(r *http.Request) error {
	if err := r.ParseForm(); isTooLarge(err) {
		return err
	}
	if err := r.ParseMultipartForm(csrfMultipartMemory); isTooLarge(err) {
		return err
	}
	return nil
}

// isTooLarge reports whether err is, or wraps, an [*http.MaxBytesError].
func isTooLarge(err error) bool {
	_, ok := errors.AsType[*http.MaxBytesError](err)
	return ok
}

// canSkipCSRFCheck reports whether the request method is safe and does not need
// CSRF validation.
func canSkipCSRFCheck(r *http.Request) bool {
	return r.Method == http.MethodGet || r.Method == http.MethodHead ||
		r.Method == http.MethodOptions
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
