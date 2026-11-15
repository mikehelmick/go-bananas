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

package i18n

import (
	"testing"
	"testing/fstest"
)

// FuzzLocaleLookup checks that Lookup, given an arbitrary lang hint and
// Accept-Language value, returns a non-nil translator and a language that is
// either the default or one of the loaded locales.
func FuzzLocaleLookup(f *testing.F) {
	f.Add("", "")
	f.Add("es", "")
	f.Add("", "es-MX,es;q=0.9,en;q=0.8")
	f.Add("ES", "")
	f.Add("es-", "")
	f.Add("fr", "de-DE")
	f.Add("x-klingon", "*")
	f.Add("en;q=abc", "en-US;q=0.5,,,;")
	f.Add("und", "zh-Hant-TW")
	f.Add("../es", "es\x00")
	f.Add("en-US-u-va-posix", "es-419")

	l, err := Load(fstest.MapFS{
		"en/default.po": &fstest.MapFile{Data: po("en", "Hello")},
		"es/default.po": &fstest.MapFile{Data: po("es", "Hola")},
	})
	if err != nil {
		f.Fatalf("Load: %v", err)
	}

	f.Fuzz(func(t *testing.T, lang, accept string) {
		tr, got := l.Lookup(lang, accept)
		if tr == nil {
			t.Fatalf("Lookup(%q, %q) returned a nil translator", lang, accept)
		}
		switch got {
		case defaultLocale, "es":
		default:
			t.Fatalf("Lookup(%q, %q) = %q, want the default or a loaded locale", lang, accept, got)
		}
	})
}
