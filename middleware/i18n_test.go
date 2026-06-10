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
	"net/http/httptest"
	"testing"

	"github.com/leonelquinteros/gotext"
	"github.com/mikehelmick/go-bananas/webctx"
)

// fakeLocaleProvider returns a fixed language derived from the first non-empty
// hint, defaulting to "en".
type fakeLocaleProvider struct{}

func (fakeLocaleProvider) Lookup(hints ...string) (gotext.Translator, string) {
	lang := "en"
	for _, h := range hints {
		if h != "" {
			lang = h
			break
		}
	}
	return gotext.NewPo(), lang
}

func TestProcessLocale(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		query    string
		wantLang string
		wantDir  string
	}{
		{"default", "", "en", LeftAlign},
		{"arabic_rtl", "ar", "ar", RightAlign},
		{"french_ltr", "fr", "fr", LeftAlign},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var m webctx.TemplateMap
			var locale gotext.Translator
			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				m = webctx.TemplateMapFromContext(r.Context())
				locale = webctx.LocaleFromContext(r.Context())
			})

			target := "/"
			if tc.query != "" {
				target += "?lang=" + tc.query
			}
			req := httptest.NewRequest(http.MethodGet, target, nil)
			ProcessLocale(fakeLocaleProvider{})(next).ServeHTTP(httptest.NewRecorder(), req)

			if m["textLanguage"] != tc.wantLang {
				t.Errorf("textLanguage = %v, want %v", m["textLanguage"], tc.wantLang)
			}
			if m["textDirection"] != tc.wantDir {
				t.Errorf("textDirection = %v, want %v", m["textDirection"], tc.wantDir)
			}
			if locale == nil {
				t.Error("expected a locale on the context")
			}
		})
	}
}
