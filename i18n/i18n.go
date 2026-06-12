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

// Package i18n loads gettext (.po) translations from a filesystem and resolves
// the best locale for a request, implementing the
// [github.com/mikehelmick/go-bananas/middleware.LocaleProvider] seam so
// ProcessLocale — and the renderer's "t"/"tDefault" template functions — work
// out of the box.
//
// The expected filesystem layout has one top-level directory per locale, each
// containing a default.po:
//
//	locales/
//	├── en/default.po
//	├── es/default.po
//	└── pt_BR/default.po
//
// Load it (typically from an embed.FS) and hand it to the middleware:
//
//	//go:embed locales
//	var localesFS embed.FS
//
//	locales, err := i18n.Load(mustSub(localesFS, "locales"))
//	r.Use(middleware.ProcessLocale(locales))
package i18n

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
	"sync"

	"github.com/leonelquinteros/gotext"
	"golang.org/x/text/language"
)

// defaultLocale is the fallback locale used when no hint matches and no other
// default was configured with [WithDefaultLocale].
const defaultLocale = "en"

// poFileName is the translation file expected inside each locale directory.
const poFileName = "default.po"

// LocaleMap holds the loaded translators, keyed by locale name, with a
// [language.Matcher] for canonicalizing arbitrary client-supplied tags. It is
// safe for concurrent use. Create one with [Load].
type LocaleMap struct {
	fsys fs.FS

	// mu guards data and matcher: reloads take the write lock while lookups
	// take the read lock, so a reload can never race a concurrent lookup.
	mu      sync.RWMutex
	data    map[string]gotext.Translator
	matcher language.Matcher

	defaultLocale string
	reload        bool
}

// Option configures a [LocaleMap] in [Load].
type Option func(*LocaleMap)

// WithDefaultLocale sets the locale used when no hint matches (default "en").
func WithDefaultLocale(s string) Option {
	return func(l *LocaleMap) { l.defaultLocale = s }
}

// WithReloading reloads the translations from the filesystem on every lookup.
// It is intended for development (edit a .po, refresh the page); a reload
// failure panics, so do not enable it in production.
func WithReloading(v bool) Option {
	return func(l *LocaleMap) { l.reload = v }
}

// Load parses the locale directories in fsys (see the package documentation for
// the expected layout) and builds the matcher. Callers should cache the result;
// loading walks the whole filesystem.
func Load(fsys fs.FS, opts ...Option) (*LocaleMap, error) {
	l := &LocaleMap{
		fsys:          fsys,
		defaultLocale: defaultLocale,
	}
	for _, opt := range opts {
		opt(l)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.load(); err != nil {
		return nil, err
	}
	return l, nil
}

// Lookup finds the best translator for the given ordered hints (typically a
// "lang" query parameter followed by the Accept-Language header) and returns it
// with its locale name. When nothing matches, the default locale is returned;
// if even that is missing, an empty translator is returned so callers and the
// "tDefault" template function can fall back gracefully.
//
// Lookup implements
// [github.com/mikehelmick/go-bananas/middleware.LocaleProvider].
func (l *LocaleMap) Lookup(hints ...string) (gotext.Translator, string) {
	// Reloading takes the write lock for the whole lookup; the normal path takes
	// the read lock, so concurrent lookups never race a reload's replacement of
	// data/matcher.
	if l.reload {
		l.mu.Lock()
		defer l.mu.Unlock()

		if err := l.load(); err != nil {
			panic(err)
		}
	} else {
		l.mu.RLock()
		defer l.mu.RUnlock()
	}

	for _, hint := range hints {
		if hint == "" {
			continue
		}

		// Exact (or lang-REGION formatted) directory-name match first.
		if locale, ok := l.data[formatLangRegion(hint)]; ok {
			return locale, formatLangRegion(hint)
		}

		// Otherwise canonicalize through the language matcher (handles
		// Accept-Language q-lists and region fallback).
		canonical, err := l.canonicalize(hint)
		if err != nil {
			continue
		}
		if locale, ok := l.data[canonical]; ok {
			return locale, canonical
		}
	}

	if locale, ok := l.data[l.defaultLocale]; ok {
		return locale, l.defaultLocale
	}
	return gotext.NewPo(), l.defaultLocale
}

// TranslatorLanguage returns the language declared by a translator's .po
// header, or "" when unavailable.
func TranslatorLanguage(t gotext.Translator) string {
	typ, ok := t.(*gotext.Po)
	if !ok {
		return ""
	}
	return typ.Language
}

// formatLangRegion normalizes "pt-br" to the directory-name form "pt_BR".
// Inputs that are not a simple lang-region pair are returned unchanged.
func formatLangRegion(s string) string {
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return s
	}
	parts[0] = strings.ToLower(parts[0])
	parts[1] = strings.ToUpper(parts[1])
	return strings.Join(parts, "_")
}

// canonicalize resolves an arbitrary client-supplied tag (or Accept-Language
// list) to one of the loaded locale names via the language matcher.
func (l *LocaleMap) canonicalize(id string) (result string, retErr error) {
	// go/text can panic on pathological inputs, which here are often supplied by
	// users or browsers: https://github.com/golang/text/pull/17
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("unknown language %q", id)
		}
	}()

	desired, _, err := language.ParseAcceptLanguage(id)
	if err != nil {
		return "", err
	}
	if tag, _, conf := l.matcher.Match(desired...); conf != language.No {
		raw, _, _ := tag.Raw()
		return raw.String(), nil
	}

	return "", fmt.Errorf("malformed language %q", id)
}

// load reads every locale directory. Callers must hold mu's write lock.
func (l *LocaleMap) load() error {
	entries, err := fs.ReadDir(l.fsys, ".")
	if err != nil {
		return fmt.Errorf("failed to list locales: %w", err)
	}

	data := make(map[string]gotext.Translator, len(entries))
	names := make([]language.Tag, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()

		b, err := fs.ReadFile(l.fsys, path.Join(name, poFileName))
		if err != nil {
			return fmt.Errorf("failed to read locale %q: %w", name, err)
		}

		po := gotext.NewPo()
		po.Parse(b)
		data[name] = po
		names = append(names, language.Make(name))
	}

	if len(data) == 0 {
		return fmt.Errorf("no locales found (expected <locale>/%s directories)", poFileName)
	}

	l.data = data
	l.matcher = language.NewMatcher(names)
	return nil
}
