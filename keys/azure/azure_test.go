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

package azure

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"slices"
	"testing"

	"github.com/mikehelmick/go-bananas/keys"
)

func TestRegistered(t *testing.T) {
	t.Parallel()
	if got := keys.RegisteredManagers(); !slices.Contains(got, "AZURE_KEY_VAULT") {
		t.Fatalf("expected %q to be registered, have %v", "AZURE_KEY_VAULT", got)
	}
}

// p256Coords returns the X and Y coordinates of a new P-256 key. If
// leadingZero is set, it retries until X has a leading zero byte.
func p256Coords(t *testing.T, leadingZero bool) (*ecdsa.PublicKey, []byte, []byte) {
	t.Helper()
	for {
		pk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		b, err := pk.PublicKey.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		x, y := b[1:33], b[33:]
		if !leadingZero || x[0] == 0 {
			return &pk.PublicKey, x, y
		}
	}
}

func TestParseP256PublicKey(t *testing.T) {
	t.Parallel()

	t.Run("round_trip", func(t *testing.T) {
		t.Parallel()
		want, x, y := p256Coords(t, false)
		got, err := parseP256PublicKey(x, y)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Equal(want) {
			t.Fatal("parsed key does not match")
		}
	})

	t.Run("short_coordinate", func(t *testing.T) {
		t.Parallel()
		want, x, y := p256Coords(t, true)
		got, err := parseP256PublicKey(bytes.TrimLeft(x, "\x00"), y)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Equal(want) {
			t.Fatal("parsed key does not match")
		}
	})

	t.Run("off_curve", func(t *testing.T) {
		t.Parallel()
		_, x, y := p256Coords(t, false)
		y = bytes.Clone(y)
		y[len(y)-1] ^= 1
		if _, err := parseP256PublicKey(x, y); err == nil {
			t.Fatal("expected error for point not on curve")
		}
	})

	t.Run("too_long", func(t *testing.T) {
		t.Parallel()
		_, x, y := p256Coords(t, false)
		if _, err := parseP256PublicKey(append([]byte{1}, x...), y); err == nil {
			t.Fatal("expected error for oversized coordinate")
		}
	})
}
