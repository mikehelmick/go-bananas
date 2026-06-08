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
	"fmt"

	"github.com/gorilla/securecookie"
)

// EntropyFunc returns the set of cookie encryption/HMAC key pairs to use. Each
// element must be at least 64 bytes: the first 32 bytes are the encryption
// (block) key and the remaining bytes are the HMAC (hash) key.
//
// Returning more than one key pair enables key rotation: cookies are always
// encoded with the first pair, but decoding is attempted against every pair, so
// an old key can be retained until outstanding cookies expire. Because the func
// is invoked on every encode and decode, it may consult an external source
// (e.g. a [github.com/mikehelmick/go-bananas/secrets.SecretManager] or
// [github.com/mikehelmick/go-bananas/keys.KeyManager]) to hot-rotate keys
// without restarting the process.
type EntropyFunc func() ([][]byte, error)

var _ securecookie.Codec = (*HotCodec)(nil)

// HotCodec is a [securecookie.Codec] that hot-loads its hash and encryption keys
// from an [EntropyFunc] on every operation, so keys can be rotated without a
// restart.
type HotCodec struct {
	maxAge      int
	entropyFunc EntropyFunc
}

// Encode implements [securecookie.Codec].
func (c *HotCodec) Encode(name string, value any) (string, error) {
	cs, err := c.newSecureCookies()
	if err != nil {
		return "", fmt.Errorf("failed to make secure cookie: %w", err)
	}

	return securecookie.EncodeMulti(name, value, cs...)
}

// Decode implements [securecookie.Codec].
func (c *HotCodec) Decode(name, value string, dst any) error {
	cs, err := c.newSecureCookies()
	if err != nil {
		return fmt.Errorf("failed to make secure cookie: %w", err)
	}
	return securecookie.DecodeMulti(name, value, dst, cs...)
}

// newSecureCookies creates a new collection of secure cookies from the data
// returned by the entropy function.
func (c *HotCodec) newSecureCookies() ([]securecookie.Codec, error) {
	bs, err := c.entropyFunc()
	if err != nil {
		return nil, fmt.Errorf("failed to get cookie hash/encryption keys: %w", err)
	}

	codecs := make([]securecookie.Codec, len(bs))
	for i, b := range bs {
		// Require at least 64 bytes: a 32-byte AES-256 block (encryption) key plus
		// a 32-byte HMAC (hash) key. A shorter key would leave the HMAC key empty
		// or undersized, silently weakening or disabling cookie integrity.
		if got, want := len(b), 64; got < want {
			return nil, fmt.Errorf("cookie key %d: length %d is below the minimum %d (32-byte encryption + 32-byte HMAC)", i, got, want)
		}

		// The first 32 bytes are the encryption (block) key, the remaining are the
		// HMAC (hash) key.
		cookie := securecookie.New(b[32:], b[:32])
		cookie.MaxAge(c.maxAge)
		codecs[i] = cookie
	}
	return codecs, nil
}
