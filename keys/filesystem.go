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
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mikehelmick/go-bananas/internal/util"
)

func init() {
	RegisterManager("FILESYSTEM", NewFilesystem)
}

var (
	_ EncryptionKeyManager = (*Filesystem)(nil)
	_ KeyManager           = (*Filesystem)(nil)
	_ SigningKeyManager    = (*Filesystem)(nil)
)

// Filesystem is a filesystem-backed key manager intended for local development
// and testing. Signing keys are stored as ECDSA P-256 private keys and
// encryption keys as 32-byte AES-GCM data-encryption keys, under the configured
// root.
type Filesystem struct {
	root string
	mu   sync.RWMutex
}

// NewFilesystem creates a [KeyManager] backed by the local filesystem. It should
// only be used for development and testing.
//
// If cfg.FilesystemRoot is provided and does not exist, it is created. A
// relative root is relative to the working directory; an empty root places data
// in the working directory.
func NewFilesystem(_ context.Context, cfg *Config) (KeyManager, error) {
	root := cfg.FilesystemRoot
	if root != "" {
		if err := os.MkdirAll(root, 0o700); err != nil {
			return nil, err
		}
	}

	return &Filesystem{root: root}, nil
}

// resolve joins the given path elements onto the key root, rejecting any that
// would escape it (path traversal).
func (k *Filesystem) resolve(parts ...string) (string, error) {
	return util.SafeJoin(k.root, filepath.Join(parts...))
}

// NewSigner returns a signer for the ECDSA private key stored at keyID.
func (k *Filesystem) NewSigner(_ context.Context, keyID string) (crypto.Signer, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()

	pth, err := k.resolve(keyID)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(pth)
	if err != nil {
		return nil, fmt.Errorf("failed to read signing key: %w", err)
	}

	pk, err := x509.ParseECPrivateKey(b)
	if err != nil {
		return nil, fmt.Errorf("failed to parse signing key: %w", err)
	}

	return pk, nil
}

// Encrypt encrypts plaintext (with optional AAD) using the most recent version
// of the named encryption key.
func (k *Filesystem) Encrypt(_ context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()

	// Find the most recent data-encryption key version.
	pth, err := k.resolve(keyID)
	if err != nil {
		return nil, err
	}
	infos, err := os.ReadDir(pth)
	if err != nil {
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}
	if len(infos) < 1 {
		return nil, fmt.Errorf("there are no key versions")
	}
	var latest fs.DirEntry
	for _, info := range infos {
		if info.Name() == "metadata" {
			continue
		}
		if latest == nil || info.Name() > latest.Name() {
			latest = info
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("key %q does not exist", keyID)
	}

	latestPath := filepath.Join(pth, latest.Name())
	dek, err := os.ReadFile(latestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read encryption key: %w", err)
	}

	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("bad cipher block: %w", err)
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to wrap cipher block: %w", err)
	}
	nonce := make([]byte, aesgcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}
	ciphertext := aesgcm.Seal(nonce, nonce, plaintext, aad)

	// Prepend the version so Decrypt knows which key to use.
	id := []byte(latest.Name() + ":")
	ciphertext = append(id, ciphertext...)

	return ciphertext, nil
}

// Decrypt decrypts ciphertext (with the same AAD used to encrypt it).
func (k *Filesystem) Decrypt(_ context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()

	// Recover the key version prepended by Encrypt.
	parts := bytes.SplitN(ciphertext, []byte(":"), 2)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid ciphertext: missing version")
	}
	version, ciphertext := parts[0], parts[1]

	versionPath, err := k.resolve(keyID, string(version))
	if err != nil {
		return nil, err
	}
	dek, err := os.ReadFile(versionPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read encryption key: %w", err)
	}

	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher from dek: %w", err)
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create gcm from dek: %w", err)
	}

	size := aesgcm.NonceSize()
	if len(ciphertext) < size {
		return nil, fmt.Errorf("malformed ciphertext")
	}
	nonce, ciphertextPortion := ciphertext[:size], ciphertext[size:]

	plaintext, err := aesgcm.Open(nil, nonce, ciphertextPortion, aad)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt ciphertext with dek: %w", err)
	}

	return plaintext, nil
}

// SigningKeyVersions lists the versions of the parent signing key, newest first.
func (k *Filesystem) SigningKeyVersions(_ context.Context, parent string) ([]SigningKeyVersion, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()

	metadata, err := k.metadataForKey(parent)
	if err != nil {
		return nil, fmt.Errorf("failed to list signing keys: %w", err)
	}
	if metadata.KeyType != "signing" {
		return nil, fmt.Errorf("failed to list signing keys: key is not a signing key type")
	}

	pth := filepath.Join(k.root, parent)
	var versions []SigningKeyVersion
	if err := filepath.Walk(pth, func(curr string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || info.Name() == "metadata" {
			return nil
		}

		b, err := os.ReadFile(curr)
		if err != nil {
			return err
		}

		pk, err := x509.ParseECPrivateKey(b)
		if err != nil {
			return fmt.Errorf("failed to parse signing key: %w", err)
		}

		versions = append(versions, &filesystemSigningKey{
			name:    strings.TrimPrefix(curr, k.root),
			created: info.ModTime(),
			pk:      pk,
		})

		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to list keys: failed to walk: %w", err)
	}

	// Sort descending so the newest key is first.
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].(*filesystemSigningKey).name > versions[j].(*filesystemSigningKey).name
	})

	return versions, nil
}

// CreateSigningKey creates a signing key (a directory plus metadata). If it
// already exists, its id is returned.
func (k *Filesystem) CreateSigningKey(_ context.Context, parent, name string) (string, error) {
	return k.createKey(parent, name, "signing")
}

// CreateEncryptionKey creates an encryption key (a directory plus metadata). If
// it already exists, its id is returned.
func (k *Filesystem) CreateEncryptionKey(_ context.Context, parent, name string) (string, error) {
	return k.createKey(parent, name, "encryption")
}

// createKey creates the key directory and metadata for the given key type.
func (k *Filesystem) createKey(parent, name, keyType string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	pth, err := k.resolve(parent, name)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(pth, 0o700); err != nil {
		return "", fmt.Errorf("failed to create directory for key: %w", err)
	}

	metadataPath := filepath.Join(pth, "metadata")
	b, err := os.ReadFile(metadataPath)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to read metadata file: %w", err)
	}
	if len(b) > 0 {
		var metadata filesystemKeyInfo
		if err := json.Unmarshal(b, &metadata); err != nil {
			return "", fmt.Errorf("failed to parse metadata: %w", err)
		}
		if metadata.KeyType != keyType {
			return "", fmt.Errorf("found key, but is not %s type", keyType)
		}
		return strings.TrimPrefix(pth, k.root), nil
	}

	// The metadata file does not exist, so create it.
	b, err = json.Marshal(&filesystemKeyInfo{KeyType: keyType})
	if err != nil {
		return "", fmt.Errorf("failed to generate metadata file: %w", err)
	}
	if err := os.WriteFile(metadataPath, b, 0o600); err != nil {
		return "", fmt.Errorf("failed to create metadata file: %w", err)
	}
	return strings.TrimPrefix(pth, k.root), nil
}

// CreateKeyVersion creates a new version of the parent key, generating a signing
// or encryption key according to the parent's type.
func (k *Filesystem) CreateKeyVersion(_ context.Context, parent string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	metadata, err := k.metadataForKey(parent)
	if err != nil {
		return "", fmt.Errorf("failed to create key version: %w", err)
	}

	switch t := metadata.KeyType; t {
	case "signing":
		pk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return "", fmt.Errorf("failed to generate signing key: %w", err)
		}
		b, err := x509.MarshalECPrivateKey(pk)
		if err != nil {
			return "", fmt.Errorf("failed to marshal signing key: %w", err)
		}
		pth := filepath.Join(k.root, parent, strconv.FormatInt(time.Now().UnixNano(), 10))
		if err := os.WriteFile(pth, b, 0o600); err != nil {
			return "", fmt.Errorf("failed to write signing key to disk: %w", err)
		}
		return strings.TrimPrefix(pth, k.root), nil
	case "encryption":
		ek := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, ek); err != nil {
			return "", fmt.Errorf("failed to generate encryption key: %w", err)
		}
		pth := filepath.Join(k.root, parent, strconv.FormatInt(time.Now().UnixNano(), 10))
		if err := os.WriteFile(pth, ek, 0o600); err != nil {
			return "", fmt.Errorf("failed to write encryption key to disk: %w", err)
		}
		return strings.TrimPrefix(pth, k.root), nil
	default:
		return "", fmt.Errorf("unknown key type %q", t)
	}
}

// DestroyKeyVersion removes the given key version file. A missing file is not an
// error.
func (k *Filesystem) DestroyKeyVersion(_ context.Context, id string) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	pth, err := k.resolve(id)
	if err != nil {
		return err
	}
	if err := os.Remove(pth); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to destroy key version: %w", err)
	}
	return nil
}

type filesystemSigningKey struct {
	name    string
	created time.Time
	pk      *ecdsa.PrivateKey
}

func (k *filesystemSigningKey) KeyID() string          { return k.name }
func (k *filesystemSigningKey) CreatedAt() time.Time   { return k.created }
func (k *filesystemSigningKey) DestroyedAt() time.Time { return time.Time{} }
func (k *filesystemSigningKey) Signer(_ context.Context) (crypto.Signer, error) {
	return k.pk, nil
}

type filesystemKeyInfo struct {
	KeyType string `json:"t"`
}

func (k *Filesystem) metadataForKey(parent string) (*filesystemKeyInfo, error) {
	metadataPath, err := k.resolve(parent, "metadata")
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open metadata (does the key exist?): %w", err)
	}

	var metadata filesystemKeyInfo
	if err := json.Unmarshal(b, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}
	return &metadata, nil
}
