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

package util

import (
	"bytes"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func FuzzSafeJoin(f *testing.F) {
	for _, s := range []string{
		"", ".", "..", "../", "../etc/passwd", "a/../../b", "a/b/c", "/abs/path",
		"..foo", "foo..", "a/./b", "//", "a\x00b", `..\..\windows`, "./..", "a/../..",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, name string) {
		root := t.TempDir()
		out, err := SafeJoin(root, name)
		if err != nil {
			return
		}
		rel, err := filepath.Rel(root, out)
		if err != nil {
			t.Fatalf("SafeJoin(%q) = %q, which is not relative to root: %v", name, out, err)
		}
		if filepath.IsAbs(rel) {
			t.Fatalf("SafeJoin(%q) = %q: relative path %q is absolute", name, out, rel)
		}
		// "..foo" is a legitimate child name; only a ".." element escapes.
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Fatalf("SafeJoin(%q) = %q escapes root (rel %q)", name, out, rel)
		}
	})
}

func FuzzBase64Decode(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte{0})
	f.Add([]byte{0xff, 0xfe})
	f.Add([]byte{0xfb, 0xff, 0xbf})
	f.Add([]byte("hello, world"))
	f.Add(bytes.Repeat([]byte{0xff}, 33))

	encodings := map[string]*base64.Encoding{
		"Std":    base64.StdEncoding,
		"RawStd": base64.RawStdEncoding,
		"URL":    base64.URLEncoding,
		"RawURL": base64.RawURLEncoding,
	}

	f.Fuzz(func(t *testing.T, b []byte) {
		for name, enc := range encodings {
			s := enc.EncodeToString(b)
			got, err := Base64Decode(s)
			if err != nil {
				t.Fatalf("Base64Decode(%s %q): %v", name, s, err)
			}
			if !bytes.Equal(got, b) {
				t.Fatalf("Base64Decode(%s %q) = %x, want %x", name, s, got, b)
			}
		}

		// Arbitrary input must not panic.
		_, _ = Base64Decode(string(b))
	})
}

// isTrimmed reports whether TrimSpace documents removing r: any
// unicode.IsSpace rune, plus the zero-width no-break space.
func isTrimmed(r rune) bool {
	return unicode.IsSpace(r) || r == '\uFEFF'
}

func FuzzTrimSpace(f *testing.F) {
	for _, s := range []string{
		"", " ", "\t\n\v\f\r ", "\uFEFFabc\uFEFF", "\u00A0x\u2003", "\u0085",
		"a b", " \xff ", "\xef\xbb", "\u200B x \u200B", "\u3000全角\u3000",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		got := TrimSpace(s)
		if again := TrimSpace(got); again != got {
			t.Fatalf("TrimSpace not idempotent: TrimSpace(%q) = %q, then %q", s, got, again)
		}
		if !strings.Contains(s, got) {
			t.Fatalf("TrimSpace(%q) = %q, not a substring of the input", s, got)
		}
		if got == "" {
			return
		}
		if r, _ := utf8.DecodeRuneInString(got); isTrimmed(r) {
			t.Fatalf("TrimSpace(%q) = %q has leading space %U", s, got, r)
		}
		if r, _ := utf8.DecodeLastRuneInString(got); isTrimmed(r) {
			t.Fatalf("TrimSpace(%q) = %q has trailing space %U", s, got, r)
		}
	})
}

func FuzzRandomToken(f *testing.F) {
	for _, n := range []uint8{0, 1, 2, 3, 16, 32, 255} {
		f.Add(n)
	}

	f.Fuzz(func(t *testing.T, n uint8) {
		b, err := RandomBytes(int(n))
		if err != nil {
			t.Fatalf("RandomBytes(%d): %v", n, err)
		}
		if len(b) != int(n) {
			t.Fatalf("RandomBytes(%d) returned %d bytes", n, len(b))
		}

		tok, err := RandomToken(int(n))
		if err != nil {
			t.Fatalf("RandomToken(%d): %v", n, err)
		}
		if want := base64.RawURLEncoding.EncodedLen(int(n)); len(tok) != want {
			t.Fatalf("RandomToken(%d) has length %d, want %d", n, len(tok), want)
		}
		dec, err := base64.RawURLEncoding.DecodeString(tok)
		if err != nil || len(dec) != int(n) {
			t.Fatalf("RandomToken(%d) = %q does not decode to %d bytes: %v", n, tok, n, err)
		}
	})
}
