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

package i18n_test

import (
	"fmt"
	"testing/fstest"

	"github.com/mikehelmick/go-bananas/i18n"
	"github.com/mikehelmick/go-bananas/middleware"
)

// The LocaleMap implements the middleware's LocaleProvider seam.
var _ middleware.LocaleProvider = (*i18n.LocaleMap)(nil)

func ExampleLoad() {
	// In a real application the locales live in an embed.FS; an in-memory FS
	// keeps this example self-contained. Layout: <locale>/default.po.
	fsys := fstest.MapFS{
		"en/default.po": &fstest.MapFile{Data: []byte(
			"msgid \"\"\nmsgstr \"\"\n\"Language: en\\n\"\n\nmsgid \"greeting\"\nmsgstr \"Hello\"\n")},
		"es/default.po": &fstest.MapFile{Data: []byte(
			"msgid \"\"\nmsgstr \"\"\n\"Language: es\\n\"\n\nmsgid \"greeting\"\nmsgstr \"Hola\"\n")},
	}

	locales, err := i18n.Load(fsys)
	if err != nil {
		panic(err)
	}

	// Hand it straight to the middleware: r.Use(middleware.ProcessLocale(locales))
	// Lookups accept a query-param hint and/or an Accept-Language header.
	tr, lang := locales.Lookup("es-MX;q=0.9, en;q=0.5")
	fmt.Println(lang, tr.Get("greeting"))
	// Output: es Hola
}
