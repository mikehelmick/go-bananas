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

package cookiestore

import (
	"testing"

	"github.com/mikehelmick/go-bananas/internal/util"
)

// staticEntropy returns an EntropyFunc that always yields the provided key sets.
func staticEntropy(keys ...[]byte) EntropyFunc {
	return func() ([][]byte, error) {
		return keys, nil
	}
}

// key64 returns a deterministic 64-byte key (32-byte block + 32-byte hash).
func key64(t *testing.T) []byte {
	t.Helper()
	b, err := util.RandomBytes(64)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	return b
}

func TestHotCodec_RoundTrip(t *testing.T) {
	t.Parallel()

	codec := &HotCodec{maxAge: 3600, entropyFunc: staticEntropy(key64(t))}

	encoded, err := codec.Encode("session", "hello world")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var got string
	if err := codec.Decode("session", encoded, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != "hello world" {
		t.Fatalf("round-trip mismatch: got %q want %q", got, "hello world")
	}
}

func TestHotCodec_ShortKey(t *testing.T) {
	t.Parallel()

	// A 32-byte key is too short: it would leave an empty HMAC key, silently
	// weakening cookie integrity. The codec must reject anything under 64 bytes.
	for _, n := range []int{9, 32, 63} {
		codec := &HotCodec{maxAge: 3600, entropyFunc: staticEntropy(make([]byte, n))}
		if _, err := codec.Encode("session", "value"); err == nil {
			t.Errorf("expected an error encoding with a %d-byte key (need >= 64)", n)
		}
	}
}

func TestHotCodec_KeyRotation(t *testing.T) {
	t.Parallel()

	oldKey := key64(t)
	newKey := key64(t)

	// Encode with only the old key present.
	oldCodec := &HotCodec{maxAge: 3600, entropyFunc: staticEntropy(oldKey)}
	encoded, err := oldCodec.Encode("session", "rotate me")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	// After rotation, the new key is primary but the old key is retained, so the
	// previously-issued cookie must still decode.
	rotated := &HotCodec{maxAge: 3600, entropyFunc: staticEntropy(newKey, oldKey)}
	var got string
	if err := rotated.Decode("session", encoded, &got); err != nil {
		t.Fatalf("decode after rotation: %v", err)
	}
	if got != "rotate me" {
		t.Fatalf("rotation mismatch: got %q want %q", got, "rotate me")
	}

	// A codec that knows only the new key must reject the old cookie.
	newOnly := &HotCodec{maxAge: 3600, entropyFunc: staticEntropy(newKey)}
	if err := newOnly.Decode("session", encoded, &got); err == nil {
		t.Fatal("expected decode to fail when the issuing key is absent")
	}
}
