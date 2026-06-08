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

package render

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	htmltemplate "html/template"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	texttemplate "text/template"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/leonelquinteros/gotext"
	"github.com/mikehelmick/go-bananas/internal/util"
)

// templateFuncs returns the default HTML template FuncMap with any caller-
// provided functions ([WithFuncs]) merged on top.
func (r *Renderer) templateFuncs() htmltemplate.FuncMap {
	m := htmltemplate.FuncMap{
		"jsIncludeTag":  r.assetIncludeTag("static/js", jsIncludeTmpl, &r.jsIncludeCache),
		"cssIncludeTag": r.assetIncludeTag("static/css", cssIncludeTmpl, &r.cssIncludeCache),

		"joinStrings":    joinStrings,
		"toSentence":     toSentence,
		"trimSpace":      util.TrimSpace,
		"stringContains": strings.Contains,
		"toLower":        strings.ToLower,
		"toUpper":        strings.ToUpper,
		"toJSON":         json.Marshal,
		"humanizeTime":   humanizeTime,
		"toBase64":       base64.StdEncoding.EncodeToString,
		"toPercent":      toPercent,
		"safeHTML":       safeHTML,
		"checkedIf":      valueIfTruthy("checked"),
		"requiredIf":     valueIfTruthy("required"),
		"selectedIf":     valueIfTruthy("selected"),
		"readonlyIf":     valueIfTruthy("readonly"),
		"disabledIf":     valueIfTruthy("disabled"),
		"invalidIf":      valueIfTruthy("is-invalid"),
		"t":              translate,
		"tDefault":       translateWithFallback,
		"hasOne":         hasOne,
		"hasMany":        hasMany,

		"pathEscape":    url.PathEscape,
		"pathUnescape":  url.PathUnescape,
		"queryEscape":   url.QueryEscape,
		"queryUnescape": url.QueryUnescape,
	}

	for k, v := range r.extraFuncs {
		m[k] = v
	}
	return m
}

// textFuncs returns the default text template FuncMap with any caller-provided
// functions ([WithTextFuncs]) merged on top.
func (r *Renderer) textFuncs() texttemplate.FuncMap {
	m := texttemplate.FuncMap{
		"joinStrings":    joinStrings,
		"toSentence":     toSentence,
		"trimSpace":      util.TrimSpace,
		"stringContains": strings.Contains,
		"toLower":        strings.ToLower,
		"toUpper":        strings.ToUpper,
		"toJSON":         json.Marshal,
		"humanizeTime":   humanizeTime,
		"toBase64":       base64.StdEncoding.EncodeToString,
		"toPercent":      toPercent,
	}

	for k, v := range r.extraTextFuncs {
		m[k] = v
	}
	return m
}

// safeHTML marks a known-safe string as HTML that does not need escaping.
func safeHTML(s string) htmltemplate.HTML {
	return htmltemplate.HTML(s)
}

// translateWithFallback prints the translation for key, or, if t is nil or the
// key is unknown, the fallback formatted with vars. It backs the "tDefault"
// template function.
func translateWithFallback(t gotext.Translator, fallback string, key string, vars ...any) string {
	if t == nil {
		return fmt.Sprintf(fallback, vars...)
	}

	v := t.Get(key, vars...)
	if v == "" || v == key {
		return fmt.Sprintf(fallback, vars...)
	}
	return v
}

// translate prints the translated text for key using the given translator. It
// backs the "t" template function and returns an error if the translator is nil
// or the key is unknown.
func translate(t gotext.Translator, key string, vars ...any) (string, error) {
	if t == nil {
		return "", fmt.Errorf("missing translator")
	}

	v := t.Get(key, vars...)
	if v == "" || v == key {
		return "", fmt.Errorf("unknown i18n key %q", key)
	}
	return v, nil
}

// humanizeTime prints a time in a human-readable, relative format. A nil
// *time.Time renders as "never".
func humanizeTime(i any) (string, error) {
	switch typ := i.(type) {
	case time.Time:
		return humanize.Time(typ), nil
	case *time.Time:
		if typ == nil {
			return "never", nil
		}
		return humanize.Time(*typ), nil
	default:
		return "", fmt.Errorf("unsupported type %T", typ)
	}
}

// toStringSlice converts the input slice to strings. The values must be
// primitive or implement [fmt.Stringer].
func toStringSlice(i any) ([]string, error) {
	t := reflect.TypeOf(i)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Slice && t.Kind() != reflect.Array {
		return nil, fmt.Errorf("value is not a slice: %T", i)
	}

	s := reflect.ValueOf(i)
	for s.Kind() == reflect.Ptr {
		s = s.Elem()
	}

	l := make([]string, 0, s.Len())
	for i := 0; i < s.Len(); i++ {
		val := s.Index(i).Interface()
		switch t := val.(type) {
		case fmt.Stringer:
			l = append(l, t.String())
		case string:
			l = append(l, t)
		case int:
			l = append(l, strconv.FormatInt(int64(t), 10))
		case int8:
			l = append(l, strconv.FormatInt(int64(t), 10))
		case int16:
			l = append(l, strconv.FormatInt(int64(t), 10))
		case int32:
			l = append(l, strconv.FormatInt(int64(t), 10))
		case int64:
			l = append(l, strconv.FormatInt(t, 10))
		case uint:
			l = append(l, strconv.FormatUint(uint64(t), 10))
		case uint8:
			l = append(l, strconv.FormatUint(uint64(t), 10))
		case uint16:
			l = append(l, strconv.FormatUint(uint64(t), 10))
		case uint32:
			l = append(l, strconv.FormatUint(uint64(t), 10))
		case uint64:
			l = append(l, strconv.FormatUint(t, 10))
		}
	}

	return l, nil
}

// joinStrings joins a list of strings or string-like values with sep.
func joinStrings(i any, sep string) (string, error) {
	l, err := toStringSlice(i)
	if err != nil {
		return "", nil
	}
	return strings.Join(l, sep), nil
}

// toSentence joins a list of string-like values into a human-friendly sentence
// using the given joiner before the final element.
func toSentence(i any, joiner string) (string, error) {
	l, err := toStringSlice(i)
	if err != nil {
		return "", nil
	}

	switch len(l) {
	case 0:
		return "", nil
	case 1:
		return l[0], nil
	case 2:
		return l[0] + " " + joiner + " " + l[1], nil
	default:
		parts, last := l[0:len(l)-1], l[len(l)-1]
		return strings.Join(parts, ", ") + " " + joiner + ", " + last, nil
	}
}

// valueIfTruthy returns a template function that emits the given HTML attribute
// string when its argument is "truthy" (a true bool, or a non-empty array, map,
// slice, or string). It backs helpers like "checkedIf" and "disabledIf".
func valueIfTruthy(s string) func(i any) htmltemplate.HTMLAttr {
	return func(i any) htmltemplate.HTMLAttr {
		if i == nil {
			return ""
		}

		v := reflect.ValueOf(i)
		if !v.IsValid() {
			return ""
		}

		//nolint:exhaustive
		switch v.Kind() {
		case reflect.Bool:
			if v.Bool() {
				return htmltemplate.HTMLAttr(s)
			}
		case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
			if v.Len() > 0 {
				return htmltemplate.HTMLAttr(s)
			}
		default:
		}

		return ""
	}
}

// toPercent multiplies f by 100 and appends a trailing percent symbol.
func toPercent(f float64) string {
	return fmt.Sprintf("%.2f%%", f*100.0)
}

// hasOne reports whether a is a slice or array of length one.
func hasOne(a any) bool {
	s := reflect.ValueOf(a)
	if s.Kind() != reflect.Slice && s.Kind() != reflect.Array {
		return false
	}
	return s.Len() == 1
}

// hasMany reports whether a is a slice or array of length greater than one.
func hasMany(a any) bool {
	s := reflect.ValueOf(a)
	if s.Kind() != reflect.Slice && s.Kind() != reflect.Array {
		return false
	}
	return s.Len() > 1
}
