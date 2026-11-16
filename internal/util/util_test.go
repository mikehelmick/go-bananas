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
	"encoding/base64"
	"testing"
)

func TestRandomBytes(t *testing.T) {
	t.Parallel()

	length := 255
	b, err := RandomBytes(length)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(b), length; got != want {
		t.Errorf("random bytes incorrect length. got %d want %d", got, want)
	}
}

func TestRandomToken(t *testing.T) {
	t.Parallel()

	for _, n := range []int{0, 1, 16, 32} {
		tok, err := RandomToken(n)
		if err != nil {
			t.Fatalf("RandomToken(%d): %v", n, err)
		}
		b, err := base64.RawURLEncoding.DecodeString(tok)
		if err != nil {
			t.Fatalf("RandomToken(%d) = %q is not raw base64url: %v", n, tok, err)
		}
		if len(b) != n {
			t.Errorf("RandomToken(%d) decodes to %d bytes", n, len(b))
		}
	}

	a, _ := RandomToken(32)
	b, _ := RandomToken(32)
	if a == b {
		t.Error("two RandomToken(32) calls returned the same token")
	}
}

func TestTrimSpace(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bom", "state\uFEFF", "state"},
		{"whitespace", " state  \r\t", "state"},
	}

	for _, tc := range cases {
		if got := TrimSpace(tc.in); got != tc.want {
			t.Errorf("TrimSpace(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTrimSpaceAndNonPrintable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bom", "state\uFEFF", "state"},
		{"whitespace", " state  \r\t", "state"},
	}

	for _, tc := range cases {
		if got := TrimSpaceAndNonPrintable(tc.in); got != tc.want {
			t.Errorf("TrimSpaceAndNonPrintable(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDefaultHTTPTransport(t *testing.T) {
	t.Parallel()

	tr := DefaultHTTPTransport()
	if tr == nil {
		t.Fatal("expected a non-nil transport")
	}
	if !tr.ForceAttemptHTTP2 {
		t.Error("expected ForceAttemptHTTP2 to be true")
	}
	// Each call must return an independent instance so callers can mutate freely.
	if other := DefaultHTTPTransport(); other == tr {
		t.Error("expected a fresh transport instance on each call")
	}
}
