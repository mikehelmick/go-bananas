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

// Package response provides HTTP response and error helpers that negotiate
// between HTML and JSON based on the request's Accept and Content-Type headers.
//
// The error helpers ([InternalError], [Unauthorized], [BadRequest], [NotFound],
// [MissingSession]) delegate to a [github.com/mikehelmick/go-bananas/render.Renderer]
// to render the appropriate "401"/"404"/"500" template for HTML clients or a
// `{"error":...}` body for JSON clients, falling back to plain text otherwise.
package response

import (
	"net/http"
	"strings"
)

const (
	// ContentTypeJSON is the canonical JSON media type.
	ContentTypeJSON = "application/json"
	// ContentTypeHTML is the canonical HTML media type.
	ContentTypeHTML = "text/html"
)

// IsJSONContentType reports whether the request's Content-Type is
// application/json. The comparison is case-insensitive (per RFC 2045) and extra
// parameters, such as a charset, are allowed.
func IsJSONContentType(r *http.Request) bool {
	t := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	return strings.HasPrefix(t, ContentTypeJSON)
}

// acceptList returns the combined Accept and Content-Type values of the request,
// used to choose between an HTML, JSON, or plain-text response.
func acceptList(r *http.Request) []string {
	accept := strings.Split(r.Header.Get("Accept"), ",")
	accept = append(accept, strings.Split(r.Header.Get("Content-Type"), ",")...)
	return accept
}

// prefixInList reports whether any element of list begins with prefix.
func prefixInList(list []string, prefix string) bool {
	for _, v := range list {
		if strings.HasPrefix(strings.TrimSpace(v), prefix) {
			return true
		}
	}
	return false
}
