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

package keys

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
)

func FuzzFilesystemDecrypt(f *testing.F) {
	// The fuzz body runs many times per process; the key is created once here,
	// in the outer function, which each fuzz worker process runs once.
	ctx := context.Background()
	km, err := NewFilesystem(ctx, &Config{FilesystemRoot: f.TempDir()})
	if err != nil {
		f.Fatalf("NewFilesystem: %v", err)
	}
	fsm := km.(*Filesystem)
	keyID, err := fsm.CreateEncryptionKey(ctx, "realm", "fuzz")
	if err != nil {
		f.Fatalf("CreateEncryptionKey: %v", err)
	}
	if _, err := fsm.CreateKeyVersion(ctx, keyID); err != nil {
		f.Fatalf("CreateKeyVersion: %v", err)
	}

	f.Add([]byte("secret"), []byte(nil), uint16(0), "abc")
	f.Add([]byte("secret"), []byte("aad"), uint16(7), "../b/1")
	f.Add([]byte(""), []byte(""), uint16(40), "/1")
	f.Add([]byte("hello world"), []byte("ctx"), uint16(65535), "1/2")
	f.Add([]byte{0, 1, 2, 255}, []byte{':'}, uint16(3), "")
	f.Add([]byte("x"), []byte(nil), uint16(1), "12:34")

	f.Fuzz(func(t *testing.T, plaintext, aad []byte, flip uint16, prefix string) {
		ct, err := fsm.Encrypt(ctx, keyID, plaintext, aad)
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}

		// Round trip.
		got, err := fsm.Decrypt(ctx, keyID, bytes.Clone(ct), aad)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if !bytes.Equal(got, plaintext) {
			t.Fatalf("Decrypt = %q, want %q", got, plaintext)
		}

		// Any single-byte flip is rejected.
		i := int(flip) % len(ct)
		flipped := bytes.Clone(ct)
		flipped[i] ^= 0xff
		if got, err := fsm.Decrypt(ctx, keyID, flipped, aad); err == nil {
			t.Fatalf("Decrypt of ciphertext with byte %d flipped succeeded: %q", i, got)
		}

		// A forged version prefix that is not all digits is rejected before any
		// file is read.
		_, rest, _ := bytes.Cut(ct, []byte(":"))
		forged := append([]byte(prefix+":"), rest...)
		version, _, _ := strings.Cut(prefix, ":")
		if _, ok := parseVersion(version); ok {
			// A numeric version must not panic, whatever the outcome.
			_, _ = fsm.Decrypt(ctx, keyID, forged, aad)
			return
		}
		_, err = fsm.Decrypt(ctx, keyID, forged, aad)
		if err == nil {
			t.Fatalf("Decrypt with version %q succeeded", version)
		}
		if !strings.Contains(err.Error(), "bad version") {
			t.Fatalf("Decrypt with version %q: got %v, want a bad version error", version, err)
		}
	})
}

func FuzzParseECDSAPublicKey(f *testing.F) {
	pemFor := func(pub any) string {
		der, err := x509.MarshalPKIXPublicKey(pub)
		if err != nil {
			f.Fatal(err)
		}
		return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	}
	for _, c := range []elliptic.Curve{elliptic.P256(), elliptic.P384()} {
		k, err := ecdsa.GenerateKey(c, rand.Reader)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(pemFor(&k.PublicKey))
	}
	edPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(pemFor(edPub))
	f.Add("")
	f.Add("-----BEGIN PUBLIC KEY-----\n-----END PUBLIC KEY-----\n")
	f.Add("-----BEGIN PUBLIC KEY-----\nMAA=\n-----END PUBLIC KEY-----\n")
	f.Add("not pem")

	f.Fuzz(func(t *testing.T, s string) {
		pub, err := ParseECDSAPublicKey(s)
		if err != nil {
			if pub != nil {
				t.Fatalf("ParseECDSAPublicKey returned a key with error %v", err)
			}
			return
		}
		if pub == nil {
			t.Fatal("ParseECDSAPublicKey returned nil key and nil error")
		}
		// ParseECDSAPublicKey promises an ECDSA key, not a P-256 one: it accepts
		// any curve x509 supports, and callers such as keys/vault check the key
		// type separately. So assert a supported NIST curve and a valid point.
		switch pub.Curve {
		case elliptic.P224():
			// crypto/ecdh has no P-224; x509 already validated the point.
			return
		case elliptic.P256(), elliptic.P384(), elliptic.P521():
		default:
			t.Fatalf("ParseECDSAPublicKey returned unexpected curve %v", pub.Curve.Params().Name)
		}
		if _, err := pub.ECDH(); err != nil {
			t.Fatalf("ParseECDSAPublicKey returned an invalid point: %v", err)
		}
	})
}
