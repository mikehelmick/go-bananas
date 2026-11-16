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
	"os"
	"path/filepath"
	"slices"
	"strings"
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

func TestFilesystemDecryptRejectsForeignVersion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	km := newFS(t)
	ekm := km.(EncryptionKeyManager)

	aID, err := ekm.CreateEncryptionKey(ctx, "realm", "a")
	if err != nil {
		t.Fatalf("CreateEncryptionKey(a): %v", err)
	}
	if _, err := ekm.CreateKeyVersion(ctx, aID); err != nil {
		t.Fatalf("CreateKeyVersion(a): %v", err)
	}
	bID, err := ekm.CreateEncryptionKey(ctx, "realm", "b")
	if err != nil {
		t.Fatalf("CreateEncryptionKey(b): %v", err)
	}
	if _, err := ekm.CreateKeyVersion(ctx, bID); err != nil {
		t.Fatalf("CreateKeyVersion(b): %v", err)
	}

	ct, err := km.Encrypt(ctx, bID, []byte("secret"), nil)
	if err != nil {
		t.Fatalf("Encrypt(b): %v", err)
	}
	ver, rest, ok := strings.Cut(string(ct), ":")
	if !ok {
		t.Fatalf("ciphertext has no version prefix: %q", ct)
	}

	// Point key a's Decrypt at key b's version file.
	forged := []byte("../b/" + ver + ":" + rest)
	if got, err := km.Decrypt(ctx, aID, forged, nil); err == nil {
		t.Fatalf("Decrypt with traversal prefix succeeded: %q", got)
	} else if strings.Contains(err.Error(), "failed to read") {
		t.Fatalf("Decrypt touched the filesystem for a bad version: %v", err)
	}

	for _, prefix := range []string{"../b/" + ver, "abc", "/1", "1/2", "", "-1", "+1", ver + "/.."} {
		forged := []byte(prefix + ":" + rest)
		_, err := km.Decrypt(ctx, aID, forged, nil)
		if err == nil {
			t.Errorf("Decrypt with prefix %q succeeded", prefix)
			continue
		}
		if !strings.Contains(err.Error(), "bad version") {
			t.Errorf("Decrypt with prefix %q: got %v, want a bad version error", prefix, err)
		}
	}
}

func TestFilesystemIgnoresNonNumericVersions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	km, err := NewFilesystem(ctx, &Config{FilesystemRoot: root})
	if err != nil {
		t.Fatalf("NewFilesystem: %v", err)
	}
	fsm := km.(*Filesystem)

	writeFile := func(t *testing.T, pth string, b []byte) {
		t.Helper()
		if err := os.WriteFile(pth, b, 0o600); err != nil {
			t.Fatalf("WriteFile(%s): %v", pth, err)
		}
	}
	randBytes := func(t *testing.T) []byte {
		t.Helper()
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			t.Fatal(err)
		}
		return b
	}

	t.Run("encrypt", func(t *testing.T) {
		t.Parallel()

		keyID, err := fsm.CreateEncryptionKey(ctx, "realm", "enc")
		if err != nil {
			t.Fatalf("CreateEncryptionKey: %v", err)
		}
		dir := filepath.Join(root, keyID)
		// A 32-byte junk file is a valid AES key, so only the name filter keeps
		// Encrypt from choosing it. "9" sorts after "10" as text.
		writeFile(t, filepath.Join(dir, ".DS_Store"), randBytes(t))
		writeFile(t, filepath.Join(dir, "9"), randBytes(t))
		writeFile(t, filepath.Join(dir, "10"), randBytes(t))

		ct, err := fsm.Encrypt(ctx, keyID, []byte("hello"), nil)
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}
		if ver, _, _ := strings.Cut(string(ct), ":"); ver != "10" {
			t.Fatalf("Encrypt used version %q, want %q", ver, "10")
		}
		got, err := fsm.Decrypt(ctx, keyID, ct, nil)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if string(got) != "hello" {
			t.Fatalf("Decrypt = %q, want %q", got, "hello")
		}
	})

	t.Run("signing", func(t *testing.T) {
		t.Parallel()

		keyID, err := fsm.CreateSigningKey(ctx, "realm", "sig")
		if err != nil {
			t.Fatalf("CreateSigningKey: %v", err)
		}
		dir := filepath.Join(root, keyID)
		for _, name := range []string{"9", "10"} {
			pk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			b, err := x509.MarshalECPrivateKey(pk)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(dir, name), b)
		}
		writeFile(t, filepath.Join(dir, ".DS_Store"), randBytes(t))

		versions, err := fsm.SigningKeyVersions(ctx, keyID)
		if err != nil {
			t.Fatalf("SigningKeyVersions: %v", err)
		}
		got := make([]string, 0, len(versions))
		for _, v := range versions {
			got = append(got, filepath.Base(v.KeyID()))
		}
		if want := []string{"10", "9"}; !slices.Equal(got, want) {
			t.Fatalf("SigningKeyVersions = %v, want %v", got, want)
		}
	})
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
