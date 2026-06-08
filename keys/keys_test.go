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
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"slices"
	"testing"
)

func newFS(t *testing.T) KeyManager {
	t.Helper()
	km, err := NewFilesystem(context.Background(), &Config{FilesystemRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("NewFilesystem: %v", err)
	}
	return km
}

func TestFilesystemSigning(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	km := newFS(t)
	skm := km.(SigningKeyManager)

	parent, err := skm.CreateSigningKey(ctx, "realm", "signer")
	if err != nil {
		t.Fatalf("CreateSigningKey: %v", err)
	}
	version, err := skm.CreateKeyVersion(ctx, parent)
	if err != nil {
		t.Fatalf("CreateKeyVersion: %v", err)
	}

	signer, err := km.NewSigner(ctx, version)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	// Sign and verify a digest.
	digest := sha256.Sum256([]byte("message"))
	sig, err := signer.Sign(rand.Reader, digest[:], nil)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	pub, ok := signer.Public().(*ecdsa.PublicKey)
	if !ok {
		t.Fatal("expected an ECDSA public key")
	}
	if !ecdsa.VerifyASN1(pub, digest[:], sig) {
		t.Fatal("signature verification failed")
	}

	versions, err := skm.SigningKeyVersions(ctx, parent)
	if err != nil {
		t.Fatalf("SigningKeyVersions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("got %d versions, want 1", len(versions))
	}
}

func TestFilesystemEncryption(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	km := newFS(t)
	ekm := km.(EncryptionKeyManager)

	parent, err := ekm.CreateEncryptionKey(ctx, "realm", "enc")
	if err != nil {
		t.Fatalf("CreateEncryptionKey: %v", err)
	}
	if _, err := ekm.CreateKeyVersion(ctx, parent); err != nil {
		t.Fatalf("CreateKeyVersion: %v", err)
	}

	plaintext := []byte("top secret")
	aad := []byte("context")

	ciphertext, err := km.Encrypt(ctx, parent, plaintext, aad)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := km.Decrypt(ctx, parent, ciphertext, aad)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("decrypted = %q, want %q", got, plaintext)
	}

	// Wrong AAD must fail to decrypt.
	if _, err := km.Decrypt(ctx, parent, ciphertext, []byte("wrong")); err == nil {
		t.Fatal("expected decryption to fail with wrong AAD")
	}
}

func TestRegistry(t *testing.T) {
	t.Parallel()

	if !slices.Contains(RegisteredManagers(), "FILESYSTEM") {
		t.Errorf("expected FILESYSTEM to be registered, have %v", RegisteredManagers())
	}

	km, err := KeyManagerFor(context.Background(), &Config{Type: "FILESYSTEM", FilesystemRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("KeyManagerFor: %v", err)
	}
	if km == nil {
		t.Fatal("expected a key manager")
	}

	if _, err := KeyManagerFor(context.Background(), &Config{Type: "NOPE"}); err == nil {
		t.Fatal("expected error for unknown manager")
	}
}

func TestParseECDSAPublicKey(t *testing.T) {
	t.Parallel()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})

	pub, err := ParseECDSAPublicKey(string(pemBytes))
	if err != nil {
		t.Fatalf("ParseECDSAPublicKey: %v", err)
	}
	if !pub.Equal(&priv.PublicKey) {
		t.Fatal("parsed key does not match original")
	}

	if _, err := ParseECDSAPublicKey("not a pem"); err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}
