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
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/asn1"
	"math/big"
	"testing"
)

func FuzzConvert1363ToASN1(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte{1})
	f.Add([]byte{0, 0})
	f.Add([]byte{0x80, 0xff})
	f.Add(bytes.Repeat([]byte{0xff}, 64))
	f.Add(append(bytes.Repeat([]byte{0}, 32), bytes.Repeat([]byte{0x7f}, 32)...))
	f.Add(bytes.Repeat([]byte{0xab}, 63))

	f.Fuzz(func(t *testing.T, b []byte) {
		der, err := convert1363ToASN1(b)
		if err != nil {
			if len(b) != 0 && len(b)%2 == 0 {
				t.Fatalf("convert1363ToASN1 rejected even length %d: %v", len(b), err)
			}
			return
		}

		var rs struct{ R, S *big.Int }
		rest, err := asn1.Unmarshal(der, &rs)
		if err != nil {
			t.Fatalf("asn1.Unmarshal of convert1363ToASN1(%x): %v", b, err)
		}
		if len(rest) != 0 {
			t.Fatalf("convert1363ToASN1(%x) left %d trailing bytes", b, len(rest))
		}
		half := len(b) / 2
		if want := new(big.Int).SetBytes(b[:half]); rs.R.Cmp(want) != 0 {
			t.Fatalf("R = %v, want %v", rs.R, want)
		}
		if want := new(big.Int).SetBytes(b[half:]); rs.S.Cmp(want) != 0 {
			t.Fatalf("S = %v, want %v", rs.S, want)
		}
	})
}

func FuzzParseP256PublicKey(f *testing.F) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		f.Fatal(err)
	}
	point, err := k.PublicKey.Bytes()
	if err != nil {
		f.Fatal(err)
	}
	x, y := point[1:33], point[33:]
	f.Add(x, y)
	f.Add(bytes.TrimLeft(x, "\x00"), bytes.TrimLeft(y, "\x00"))
	f.Add(x, append(bytes.Clone(y[:31]), y[31]^1))
	f.Add([]byte(nil), []byte(nil))
	f.Add([]byte{0}, []byte{0})
	f.Add(bytes.Repeat([]byte{0xff}, 32), bytes.Repeat([]byte{0xff}, 32))
	f.Add(bytes.Repeat([]byte{1}, 33), y)

	f.Fuzz(func(t *testing.T, x, y []byte) {
		pub, err := parseP256PublicKey(x, y)

		// Independent oracle: crypto/ecdh validates the same uncompressed point.
		valid := false
		pt := make([]byte, 65)
		if len(x) <= 32 && len(y) <= 32 {
			pt[0] = 4
			copy(pt[33-len(x):33], x)
			copy(pt[65-len(y):], y)
			_, oerr := ecdh.P256().NewPublicKey(pt)
			valid = oerr == nil
		}

		if !valid {
			if err == nil {
				t.Fatalf("parseP256PublicKey(%x, %x) accepted a point that is off the curve or malformed", x, y)
			}
			return
		}
		if err != nil {
			t.Fatalf("parseP256PublicKey(%x, %x) rejected a valid point: %v", x, y, err)
		}
		if pub.Curve != elliptic.P256() {
			t.Fatalf("parseP256PublicKey returned curve %v", pub.Curve.Params().Name)
		}
		got, err := pub.Bytes()
		if err != nil {
			t.Fatalf("Bytes: %v", err)
		}
		if !bytes.Equal(got, pt) {
			t.Fatalf("parseP256PublicKey(%x, %x) returned point %x, want %x", x, y, got, pt)
		}
	})
}
