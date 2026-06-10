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
	"net/url"
	"path"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mikehelmick/go-bananas/webctx"
)

// Path wraps the request URL and is placed on the template map under
// "currentPath" by [InjectCurrentPath]. Its methods make it easy for templates
// to highlight the active nav item.
type Path struct {
	uri *url.URL
}

// InjectCurrentPath stores the request's path on the template map as a [*Path]
// under the key "currentPath", so templates can reason about the active route.
func InjectCurrentPath() mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			m := webctx.TemplateMapFromContext(ctx)
			m["currentPath"] = &Path{uri: r.URL}

			ctx = webctx.WithTemplateMap(ctx, m)
			r = r.Clone(ctx)

			next.ServeHTTP(w, r)
		})
	}
}

// String returns the full request URI.
func (p *Path) String() string {
	return p.uri.String()
}

// IsPath reports whether the request URI exactly equals s.
func (p *Path) IsPath(s string) bool {
	return p.uri.RequestURI() == s
}

// IsDir reports whether the request URI is prefixed by s.
func (p *Path) IsDir(s string) bool {
	return strings.HasPrefix(p.uri.RequestURI(), s)
}

// IsFile reports whether the final path segment of the request URI equals s.
func (p *Path) IsFile(s string) bool {
	_, file := path.Split(p.uri.RequestURI())
	return file == s
}
