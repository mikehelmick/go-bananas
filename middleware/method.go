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
	"bytes"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/mux"
)

// formKeyMethod is the form field used to override the HTTP method.
const formKeyMethod = "_method"

// maxMethodOverrideBodyBytes caps how much of a request body MutateMethod reads
// while looking for the override field.
const maxMethodOverrideBodyBytes = 64 << 10

// MutateMethod lets HTML forms emulate PUT, PATCH and DELETE by supplying a
// "_method" form value.
//
// The override applies only to POST requests whose Content-Type is
// application/x-www-form-urlencoded, and the value is read only from the body,
// never the query string. Bodies larger than 64 KB never get an override, and
// multipart bodies are never parsed. The value is case-insensitive and must be
// PUT, PATCH or DELETE; anything else is ignored and the method stays POST.
// The body is parsed strictly: a body that url.ParseQuery rejects (for example,
// a malformed percent-escape in any field) gets no override. The request body
// is left intact for later handlers, and on an override r.PostForm holds the
// parsed body so CSRF tokens and form fields remain readable.
//
// If earlier middleware has already parsed the form, r.PostForm is used as
// is and the size and strictness rules above do not apply. On an override,
// later form parsing (including form.Bind's WithMaxBodyBytes) is a no-op
// because r.PostForm is already set, so overridden requests are bounded by
// the 64 KB cap here rather than by later parse limits.
//
// To affect routing, MutateMethod must wrap the router:
//
//	handler := middleware.MutateMethod()(router)
//
// When installed with router.Use it runs after a route has matched, so it only
// changes the method that later middleware and handlers see.
//
// An override turns a cross-origin form POST into a PUT, PATCH or DELETE
// without a CORS preflight. Every route reachable through an override must be
// behind [HandleCSRF].
func MutateMethod() mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if method, ok := overrideMethod(r); ok {
				r.Method = method
			}

			next.ServeHTTP(w, r)
		})
	}
}

// readCloser pairs a replacement reader with the original body's Close.
type readCloser struct {
	io.Reader
	io.Closer
}

// overrideMethod returns the method requested by the "_method" body field and
// whether it is a permitted override for r.
//
// If r.PostForm was already parsed, it is used as is. Otherwise overrideMethod
// reads up to 64 KB (plus one byte) of r.Body and then restores r.Body so that
// later readers see exactly the original bytes. On a successful override it
// sets r.PostForm to the parsed body values: net/http's ParseForm reads the
// body only for POST, PUT and PATCH, so a DELETE would otherwise lose its form
// fields, including the CSRF token. Without an override, r.PostForm is left
// untouched.
func overrideMethod(r *http.Request) (string, bool) {
	if r.Method != http.MethodPost {
		return "", false
	}

	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		return "", false
	}

	vals := r.PostForm
	if vals == nil {
		if vals = readBodyForm(r); vals == nil {
			return "", false
		}
	}

	switch method := strings.ToUpper(vals.Get(formKeyMethod)); method {
	case http.MethodPut, http.MethodPatch, http.MethodDelete:
		r.PostForm = vals
		return method, true
	default:
		return "", false
	}
}

// readBodyForm parses up to 64 KB of r.Body as a urlencoded form and restores
// r.Body to replay the bytes it read, followed by the rest of the original
// body; closing the restored body closes the original. It returns nil if the
// body is absent, larger than the cap, unreadable, or not strictly valid
// urlencoded data (as judged by url.ParseQuery).
func readBodyForm(r *http.Request) url.Values {
	if r.Body == nil || r.Body == http.NoBody || r.ContentLength > maxMethodOverrideBodyBytes {
		return nil
	}

	orig := r.Body
	buf, err := io.ReadAll(io.LimitReader(orig, maxMethodOverrideBodyBytes+1))
	r.Body = readCloser{Reader: io.MultiReader(bytes.NewReader(buf), orig), Closer: orig}
	if err != nil || len(buf) > maxMethodOverrideBodyBytes {
		return nil
	}

	vals, err := url.ParseQuery(string(buf))
	if err != nil {
		return nil
	}
	return vals
}
