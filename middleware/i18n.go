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
	"github.com/leonelquinteros/gotext"
	"github.com/mikehelmick/go-bananas/webctx"
)

const (
	// HeaderAcceptLanguage is the standard content-negotiation language header.
	HeaderAcceptLanguage = "Accept-Language"
	// QueryKeyLanguage is the query parameter that overrides the language.
	QueryKeyLanguage = "lang"

	// LeftAlign and RightAlign are the text-direction values placed on the
	// template map for use in templates (e.g. the html dir attribute).
	LeftAlign  = "ltr"
	RightAlign = "rtl"
)

// rightAlignLanguages is the set of language codes rendered right-to-left.
var rightAlignLanguages = map[string]struct{}{
	"ar": {},
}

// LocaleProvider resolves the best [gotext.Translator] for a request. It is the
// pluggable seam for internationalization: the framework does not prescribe how
// translations are loaded, only how the chosen translator reaches templates.
type LocaleProvider interface {
	// Lookup returns the best translator for the given ordered hints (typically
	// the "lang" query parameter followed by the Accept-Language header), along
	// with its BCP-47 language tag (e.g. "en", "ar"). It may return a nil
	// translator, in which case templates fall back to default strings.
	Lookup(hints ...string) (t gotext.Translator, lang string)
}

// ProcessLocale resolves the request locale via p and stores the translator on
// the context (for the "t"/"tDefault" template functions) along with template
// map values "locale", "acceptLanguage", "textLanguage", and "textDirection".
// Install it after the session/template middleware.
func ProcessLocale(p LocaleProvider) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			param := r.URL.Query().Get(QueryKeyLanguage)
			header := r.Header.Get(HeaderAcceptLanguage)

			locale, lang := p.Lookup(param, header)

			m := webctx.TemplateMapFromContext(ctx)
			m["locale"] = locale
			m["acceptLanguage"] = []string{param, header}
			m["textLanguage"] = lang

			direction := LeftAlign
			if _, ok := rightAlignLanguages[lang]; ok {
				direction = RightAlign
			}
			m["textDirection"] = direction

			ctx = webctx.WithLocale(ctx, locale)
			ctx = webctx.WithTemplateMap(ctx, m)
			r = r.Clone(ctx)

			next.ServeHTTP(w, r)
		})
	}
}
