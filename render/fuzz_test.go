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
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
