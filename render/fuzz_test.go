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
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"unicode"
	"unicode/utf8"
)

// FuzzRenderJSON checks that the Printf-based error body is always valid JSON,
// whatever the error text is.
func FuzzRenderJSON(f *testing.F) {
	for _, s := range []string{"", "boom", `a\b"c` + "\n", "\x00\x1f\u2028", "\xff\xfe", `"}`, "</script>"} {
		f.Add(s)
	}

	r, err := New(testFS("body{}"), WithLogger(nil), WithDevMode(true))
	if err != nil {
		f.Fatalf("New: %v", err)
	}

	f.Fuzz(func(t *testing.T, msg string) {
		w := httptest.NewRecorder()
		r.RenderJSON(w, http.StatusOK, failingJSON{msg: msg})
		if body := w.Body.Bytes(); !json.Valid(body) {
			t.Fatalf("body is not valid JSON: %q", body)
		}

		if got := escapeJSON(msg); !json.Valid([]byte(`"` + got + `"`)) {
			t.Fatalf("escapeJSON(%q) = %q is not a valid JSON string body", msg, got)
		}
	})
}

// FuzzRenderCSVFilename checks that the Content-Disposition header round-trips
// the filename through mime.ParseMediaType with no injected parameters.
func FuzzRenderCSVFilename(f *testing.F) {
	for _, s := range []string{"report.csv", "", "x.csv; filename*=UTF-8''evil.html", `a"b\c.csv`, "résumé.csv", "a b;c=d"} {
		f.Add(s)
	}

	r, err := New(testFS("body{}"), WithLogger(nil))
	if err != nil {
		f.Fatalf("New: %v", err)
	}

	f.Fuzz(func(t *testing.T, name string) {
		w := httptest.NewRecorder()
		r.RenderCSV(w, http.StatusOK, name, errCSV{data: []byte("a\n")})

		// The round-trip property only holds for printable UTF-8; anything else
		// just must not panic.
		if !utf8.ValidString(name) || strings.IndexFunc(name, unicode.IsControl) >= 0 {
			return
		}
		want := name
		if want == "" {
			want = "data.csv"
		}

		header := w.Header().Get("Content-Disposition")
		disposition, params, err := mime.ParseMediaType(header)
		if err != nil {
			t.Fatalf("ParseMediaType(%q): %v", header, err)
		}
		if disposition != "attachment" {
			t.Errorf("disposition = %q, want attachment", disposition)
		}
		if len(params) != 1 || params["filename"] != want {
			t.Errorf("params = %q, want only filename=%q (header %q)", params, want, header)
		}
	})
}

// FuzzToJSON checks that toJSON output is valid JSON that cannot close a script
// element or open an HTML comment, and that it round-trips.
func FuzzToJSON(f *testing.F) {
	for _, s := range []string{"", "</script><b>", "<!-- x -->", "\u2028\u2029", "</SCRIPT", "&amp;", "\xff"} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		for _, v := range []any{s, map[string]string{s: s}, []string{s}} {
			out, err := toJSON(v)
			if err != nil {
				t.Fatalf("toJSON(%q): %v", v, err)
			}
			got := string(out)
			if !json.Valid([]byte(got)) {
				t.Fatalf("toJSON(%q) = %q is not valid JSON", v, got)
			}
			lower := strings.ToLower(got)
			for _, bad := range []string{"</script", "<!--", "\u2028", "\u2029"} {
				if strings.Contains(lower, bad) {
					t.Fatalf("toJSON(%q) = %q contains %q", v, got, bad)
				}
			}
		}

		if !utf8.ValidString(s) {
			return
		}
		out, err := toJSON(s)
		if err != nil {
			t.Fatal(err)
		}
		var got string
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("Unmarshal(%q): %v", out, err)
		}
		if got != s {
			t.Fatalf("round-trip = %q, want %q", got, s)
		}
	})
}

// FuzzRenderHTMLEscaping checks that data rendered through html/template can
// never introduce a raw '<' into the page.
func FuzzRenderHTMLEscaping(f *testing.F) {
	for _, s := range []string{"", "<script>alert(1)</script>", "</p><p>", "&lt;", "<!--", `\x3c`, `<`, "\xff<", "<\x00>", "＜"} {
		f.Add(s)
	}

	fsys := fstest.MapFS{
		"p.html": &fstest.MapFile{Data: []byte(`{{define "p"}}<p>{{.}}</p>{{end}}`)},
	}
	r, err := New(fsys, WithLogger(nil))
	if err != nil {
		f.Fatalf("New: %v", err)
	}

	f.Fuzz(func(t *testing.T, s string) {
		w := httptest.NewRecorder()
		r.RenderHTML(w, "p", s)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}

		body := w.Body.String()
		inner, ok := strings.CutPrefix(body, "<p>")
		if !ok {
			t.Fatalf("body %q does not start with <p>", body)
		}
		inner, ok = strings.CutSuffix(inner, "</p>")
		if !ok {
			t.Fatalf("body %q does not end with </p>", body)
		}
		if strings.Contains(inner, "<") {
			t.Fatalf("data %q produced raw '<' in body %q", s, body)
		}
	})
}

// fuzzSliceInput builds a value of one of the shapes templates commonly pass to
// the list helpers, chosen by kind. Kinds 0-2 hold exactly a, b and c.
func fuzzSliceInput(kind uint8, a, b, c string, n int64) any {
	strs := []string{a, b, c}
	switch kind % 10 {
	case 0:
		return strs
	case 1:
		return &strs
	case 2:
		return [3]string{a, b, c}
	case 3:
		return []int64{n, -n, 0}
	case 4:
		return []any{a, n, uint8(n), nil, struct{}{}}
	case 5:
		return []string{}
	case 6:
		return a // not a slice
	case 7:
		return nil
	case 8:
		return (*[]string)(nil)
	default:
		return []fmt.Stringer{nil}
	}
}

// FuzzToStringSlice checks that toStringSlice never panics, and that string
// slices, pointers to them and string arrays come back unchanged.
func FuzzToStringSlice(f *testing.F) {
	for k := range 10 {
		f.Add(uint8(k), "a", "", "\xff", int64(-1))
	}

	f.Fuzz(func(t *testing.T, kind uint8, a, b, c string, n int64) {
		got, err := toStringSlice(fuzzSliceInput(kind, a, b, c, n))
		if kind%10 > 2 {
			return
		}
		if err != nil {
			t.Fatalf("toStringSlice: %v", err)
		}
		if want := []string{a, b, c}; !slices.Equal(got, want) {
			t.Fatalf("toStringSlice = %q, want %q", got, want)
		}
	})
}

// FuzzJoinStrings checks that joinStrings never panics or errors, and that the
// output contains every element and has the expected length.
func FuzzJoinStrings(f *testing.F) {
	for k := range 10 {
		f.Add(uint8(k), "a", "", "\xff", ", ", int64(7))
	}

	f.Fuzz(func(t *testing.T, kind uint8, a, b, c, sep string, n int64) {
		in := fuzzSliceInput(kind, a, b, c, n)
		got, err := joinStrings(in, sep)
		if err != nil {
			t.Fatalf("joinStrings: %v", err)
		}
		l, err := toStringSlice(in)
		if err != nil {
			if got != "" {
				t.Fatalf("joinStrings(non-slice) = %q, want empty", got)
			}
			return
		}
		want := 0
		for _, s := range l {
			if !strings.Contains(got, s) {
				t.Fatalf("joinStrings(%q, %q) = %q, missing %q", l, sep, got, s)
			}
			want += len(s)
		}
		if len(l) > 1 {
			want += len(sep) * (len(l) - 1)
		}
		if len(got) != want {
			t.Fatalf("len(joinStrings(%q, %q)) = %d, want %d", l, sep, len(got), want)
		}
	})
}

// FuzzToSentence checks that toSentence never panics or errors, that the output
// contains every element, and that it contains the joiner when there are
// several.
func FuzzToSentence(f *testing.F) {
	for k := range 10 {
		f.Add(uint8(k), "a", "", "\xff", "and", int64(3))
	}

	f.Fuzz(func(t *testing.T, kind uint8, a, b, c, joiner string, n int64) {
		in := fuzzSliceInput(kind, a, b, c, n)
		got, err := toSentence(in, joiner)
		if err != nil {
			t.Fatalf("toSentence: %v", err)
		}
		l, err := toStringSlice(in)
		if err != nil {
			return
		}
		for _, s := range l {
			if !strings.Contains(got, s) {
				t.Fatalf("toSentence(%q, %q) = %q, missing %q", l, joiner, got, s)
			}
		}
		if len(l) > 1 && !strings.Contains(got, " "+joiner+" ") && !strings.Contains(got, " "+joiner+", ") {
			t.Fatalf("toSentence(%q, %q) = %q, missing joiner", l, joiner, got)
		}
	})
}
