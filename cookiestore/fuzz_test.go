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
	"bytes"
	"encoding/base64"
	"testing"
)

// FuzzHotCodecDecode checks that HotCodec rejects tampered and misnamed
// cookies. For every input it checks that:
//   - decoding the raw fuzzed value as a cookie never panics;
//   - Encode then Decode under the same name round-trips;
//   - Decode under any other name fails;
//   - Decode fails after any single byte of the decoded cookie is flipped.
func FuzzHotCodecDecode(f *testing.F) {
	key := bytes.Repeat([]byte{0x5a}, 64)
	codec := &HotCodec{maxAge: 3600, entropyFunc: staticEntropy(key)}

	// More seeds live in testdata/fuzz/FuzzHotCodecDecode.
	f.Add("go-bananas-session", "hello", uint(0))
	f.Add("session", "not-a-cookie", uint(0))

	f.Fuzz(func(t *testing.T, name, value string, pos uint) {
		// Arbitrary attacker-supplied cookie values never panic.
		var raw string
		_ = codec.Decode(name, value, &raw)

		encoded, err := codec.Encode(name, value)
		if err != nil {
			// securecookie caps the encoded length at 4096 bytes.
			t.Skip("value too large to encode")
		}

		var got string
		if err := codec.Decode(name, encoded, &got); err != nil {
			t.Fatalf("round-trip decode failed: %v", err)
		}
		if got != value {
			t.Fatalf("round-trip = %q, want %q", got, value)
		}

		for _, other := range []string{name + "x", "x" + name, name + "|"} {
			var v string
			if err := codec.Decode(other, encoded, &v); err == nil {
				t.Fatalf("decode under name %q accepted a cookie for %q", other, name)
			}
		}

		blob, err := base64.URLEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatalf("encoded cookie is not base64url: %v", err)
		}
		i := int(pos % uint(len(blob)))
		blob[i] ^= 0x01
		tampered := base64.URLEncoding.EncodeToString(blob)
		var v string
		if err := codec.Decode(name, tampered, &v); err == nil {
			t.Fatalf("decode accepted a cookie with byte %d flipped", i)
		}
	})
}
