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

func po(lang, greeting string) []byte {
	return []byte(`msgid ""
msgstr ""
"Language: ` + lang + `\n"
"Content-Type: text/plain; charset=UTF-8\n"

msgid "greeting"
msgstr "` + greeting + `"
`)
}

func testLocales() fstest.MapFS {
	return fstest.MapFS{
		"en/default.po":    &fstest.MapFile{Data: po("en", "Hello")},
		"es/default.po":    &fstest.MapFile{Data: po("es", "Hola")},
		"pt_BR/default.po": &fstest.MapFile{Data: po("pt_BR", "Olá")},
	}
}

func TestLoadAndLookup(t *testing.T) {
	t.Parallel()

	l, err := Load(testLocales())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	cases := []struct {
		name     string
		hints    []string
		wantLang string
		wantMsg  string
	}{
		{"exact", []string{"es"}, "es", "Hola"},
		{"region_dash_form", []string{"pt-br"}, "pt_BR", "Olá"},
		{"accept_language_qlist", []string{"", "es-MX;q=0.9, en;q=0.5"}, "es", "Hola"},
		{"unknown_falls_back", []string{"zz"}, "en", "Hello"},
		{"empty_hints_fall_back", []string{"", ""}, "en", "Hello"},
		{"malformed_is_safe", []string{"!!not-a-tag!!"}, "en", "Hello"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tr, lang := l.Lookup(tc.hints...)
			if lang != tc.wantLang {
				t.Errorf("lang = %q, want %q", lang, tc.wantLang)
			}
			if got := tr.Get("greeting"); got != tc.wantMsg {
				t.Errorf("greeting = %q, want %q", got, tc.wantMsg)
			}
		})
	}
}

func TestWithDefaultLocale(t *testing.T) {
	t.Parallel()

	l, err := Load(testLocales(), WithDefaultLocale("es"))
	if err != nil {
		t.Fatal(err)
	}

	tr, lang := l.Lookup("zz")
	if lang != "es" || tr.Get("greeting") != "Hola" {
		t.Fatalf("fallback = %q/%q, want es/Hola", lang, tr.Get("greeting"))
	}
}

func TestLoadEmptyFS(t *testing.T) {
	t.Parallel()

	if _, err := Load(fstest.MapFS{}); err == nil {
		t.Fatal("expected an error for a filesystem with no locales")
	}
}

func TestMissingDefaultLocaleStillUsable(t *testing.T) {
	t.Parallel()

	// Only Spanish loaded, default "en" missing entirely: Lookup must not panic
	// and must return a usable (empty) translator.
	l, err := Load(fstest.MapFS{"es/default.po": &fstest.MapFile{Data: po("es", "Hola")}})
	if err != nil {
		t.Fatal(err)
	}

	tr, _ := l.Lookup("zz")
	if tr == nil {
		t.Fatal("expected a non-nil translator")
	}
	_ = tr.Get("greeting") // must not panic
}

func TestTranslatorLanguage(t *testing.T) {
	t.Parallel()

	l, err := Load(testLocales())
	if err != nil {
		t.Fatal(err)
	}
	tr, _ := l.Lookup("es")
	if got := TranslatorLanguage(tr); got != "es" {
		t.Errorf("TranslatorLanguage = %q, want es", got)
	}
}
