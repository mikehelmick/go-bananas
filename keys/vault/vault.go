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

// Package vault provides a HashiCorp Vault (transit engine) backed
// [github.com/mikehelmick/go-bananas/keys.KeyManager].
//
// Blank-import this package to register the "HASHICORP_VAULT" provider:
//
//	import _ "github.com/mikehelmick/go-bananas/keys/vault"
//
// The Vault SDK is imported only here, so binaries that do not use this provider
// never compile or link it.
package vault

import (
	"context"
	"crypto"
	"encoding/base64"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	vaultapi "github.com/hashicorp/vault/api"
	"github.com/mikehelmick/go-bananas/internal/util"
	"github.com/mikehelmick/go-bananas/keys"
	"github.com/mitchellh/mapstructure"
)

func init() {
	keys.RegisterManager("HASHICORP_VAULT", NewHashiCorpVault)
}

var (
	_ keys.KeyManager        = (*HashiCorpVault)(nil)
	_ keys.SigningKeyManager = (*HashiCorpVault)(nil)
	_ crypto.Signer          = (*Signer)(nil)
)

// HashiCorpVault is a [github.com/mikehelmick/go-bananas/keys.KeyManager] backed
// by Vault's transit secrets engine. Encryption keys used with Encrypt/Decrypt
// must be created with derived=true.
type HashiCorpVault struct {
	client *vaultapi.Client
}

// NewHashiCorpVault creates a Vault-backed key manager using the default
// environment-based client configuration.
func NewHashiCorpVault(_ context.Context, _ *keys.Config) (keys.KeyManager, error) {
	client, err := vaultapi.NewClient(nil)
	if err != nil {
		return nil, fmt.Errorf("keys.NewHashiCorpVault: client: %w", err)
	}
	return &HashiCorpVault{client: client}, nil
}

// keyID is a parsed "name@version" Vault key reference.
type keyID struct {
	Name    string
	Version string
}

func parseKeyID(id string) (*keyID, error) {
	parts := strings.SplitN(id, "@", 2)
	if len(parts) < 2 {
		return nil, fmt.Errorf("missing version in: %v", id)
	}
	return &keyID{Name: parts[0], Version: parts[1]}, nil
}

type readKeyResponse struct {
	Versions map[string]struct {
		CreationTime time.Time `ms:"creation_time"`
		PublicKeyPEM string    `ms:"public_key"`
	} `ms:"keys"`
}

var _ keys.SigningKeyVersion = (*signingKeyVersion)(nil)

type signingKeyVersion struct {
	client    *vaultapi.Client
	name      string
	version   int64
	createdAt time.Time
	publicKey crypto.PublicKey
}

func (v *signingKeyVersion) KeyID() string          { return fmt.Sprintf("%s/%d", v.name, v.version) }
func (v *signingKeyVersion) CreatedAt() time.Time   { return v.createdAt.UTC() }
func (v *signingKeyVersion) DestroyedAt() time.Time { return time.Time{} }
func (v *signingKeyVersion) Signer(_ context.Context) (crypto.Signer, error) {
	return &Signer{
		client:    v.client,
		name:      v.name,
		version:   strconv.FormatInt(v.version, 10),
		publicKey: v.publicKey,
	}, nil
}

// SigningKeyVersions returns the signing-key versions for the named transit key.
func (v *HashiCorpVault) SigningKeyVersions(_ context.Context, parent string) ([]keys.SigningKeyVersion, error) {
	pth := fmt.Sprintf("transit/keys/%s", parent)
	result, err := v.client.Logical().Read(pth)
	if err != nil {
		return nil, fmt.Errorf("unable to read key: %w", err)
	}
	if result == nil || result.Data == nil {
		return nil, fmt.Errorf("key does not exist")
	}

	var response readKeyResponse
	dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook:       mapstructure.StringToTimeHookFunc(time.RFC3339),
		WeaklyTypedInput: true,
		Result:           &response,
		TagName:          "ms",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to setup decoder: %w", err)
	}
	if err := dec.Decode(result.Data); err != nil {
		return nil, fmt.Errorf("failed to decode result: %w", err)
	}

	versions := make([]keys.SigningKeyVersion, 0, len(response.Versions))
	for k, ver := range response.Versions {
		publicKey, err := keys.ParseECDSAPublicKey(ver.PublicKeyPEM)
		if err != nil {
			return nil, fmt.Errorf("failed to parse public key for %s", k)
		}
		num, err := strconv.ParseInt(k, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid key version %q: %w", k, err)
		}
		versions = append(versions, &signingKeyVersion{
			client:    v.client,
			name:      parent,
			version:   num,
			createdAt: ver.CreationTime,
			publicKey: publicKey,
		})
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].(*signingKeyVersion).version < versions[j].(*signingKeyVersion).version
	})

	return versions, nil
}

// CreateSigningKey creates a new ECDSA P-256 transit signing key.
func (v *HashiCorpVault) CreateSigningKey(_ context.Context, parent, name string) (string, error) {
	id := strings.Trim(strings.Join([]string{parent, name}, "/"), "/")
	pth := fmt.Sprintf("/transit/keys/%s", id)
	if _, err := v.client.Logical().Write(pth, map[string]any{
		"name": name,
		"type": "ecdsa-p256",
	}); err != nil {
		return "", fmt.Errorf("failed to create signing key: %w", err)
	}
	return id, nil
}

// CreateKeyVersion rotates the named transit key. Note: due to Vault's API there
// is a small race in identifying exactly which version was created.
func (v *HashiCorpVault) CreateKeyVersion(ctx context.Context, parent string) (string, error) {
	pth := fmt.Sprintf("/transit/keys/%s/rotate", parent)
	if _, err := v.client.Logical().Write(pth, nil); err != nil {
		return "", fmt.Errorf("failed to rotate signing key: %w", err)
	}

	list, err := v.SigningKeyVersions(ctx, parent)
	if err != nil {
		return "", fmt.Errorf("failed to lookup signing keys: %w", err)
	}
	if len(list) < 1 {
		return "", fmt.Errorf("no signing keys")
	}
	return list[len(list)-1].KeyID(), nil
}

// DestroyKeyVersion is unsupported on Vault.
func (v *HashiCorpVault) DestroyKeyVersion(_ context.Context, _ string) error {
	return fmt.Errorf("vault does not support destroying a key version")
}

// NewSigner returns a signer for the Vault transit key "name@version".
func (v *HashiCorpVault) NewSigner(ctx context.Context, id string) (crypto.Signer, error) {
	k, err := parseKeyID(id)
	if err != nil {
		return nil, err
	}
	return NewSigner(ctx, v.client, k.Name, k.Version)
}

// Encrypt encrypts plaintext with the named transit key and AAD context.
func (v *HashiCorpVault) Encrypt(_ context.Context, id string, plaintext, aad []byte) ([]byte, error) {
	k, err := parseKeyID(id)
	if err != nil {
		return nil, err
	}
	result, err := v.client.Logical().Write(fmt.Sprintf("transit/encrypt/%s", k.Name), map[string]any{
		"plaintext": base64.StdEncoding.EncodeToString(plaintext),
		"context":   base64.StdEncoding.EncodeToString(aad),
		"type":      "aes256-gcm96",
	})
	if err != nil {
		return nil, fmt.Errorf("unable to encrypt: %w", err)
	}
	ciphertext, ok := result.Data["ciphertext"].(string)
	if !ok {
		return nil, fmt.Errorf("encryption returned no string ciphertext")
	}
	return []byte(ciphertext), nil
}

// Decrypt decrypts ciphertext with the named transit key and AAD context.
func (v *HashiCorpVault) Decrypt(_ context.Context, id string, ciphertext, aad []byte) ([]byte, error) {
	k, err := parseKeyID(id)
	if err != nil {
		return nil, err
	}
	result, err := v.client.Logical().Write(fmt.Sprintf("transit/decrypt/%s", k.Name), map[string]any{
		"ciphertext": string(ciphertext),
		"context":    base64.StdEncoding.EncodeToString(aad),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to decrypt: %w", err)
	}
	plaintext, ok := result.Data["plaintext"].(string)
	if !ok {
		return nil, fmt.Errorf("decryption returned no string plaintext")
	}
	decoded, err := util.Base64Decode(plaintext)
	if err != nil {
		return nil, fmt.Errorf("unable to decode plaintext: %w", err)
	}
	return decoded, nil
}

// Signer is a [crypto.Signer] backed by a Vault transit key.
type Signer struct {
	client    *vaultapi.Client
	name      string
	version   string
	publicKey crypto.PublicKey
}

// NewSigner creates a signer for the Vault transit key name/version, fetching
// the public key up front.
func NewSigner(_ context.Context, client *vaultapi.Client, name, version string) (*Signer, error) {
	if client == nil {
		return nil, fmt.Errorf("missing client")
	}
	if name == "" {
		return nil, fmt.Errorf("missing name")
	}
	if version == "" {
		return nil, fmt.Errorf("version is required")
	}

	s := &Signer{client: client, name: name, version: version}
	publicKey, err := s.getPublicKey()
	if err != nil {
		return nil, fmt.Errorf("failed to get public key: %w", err)
	}
	s.publicKey = publicKey
	return s, nil
}

// Public returns the signer's public key.
func (s *Signer) Public() crypto.PublicKey {
	return s.publicKey
}

// Sign signs the pre-hashed digest via Vault's transit engine.
func (s *Signer) Sign(_ io.Reader, digest []byte, _ crypto.SignerOpts) ([]byte, error) {
	secret, err := s.client.Logical().Write(fmt.Sprintf("transit/sign/%s/sha2-256", s.name), map[string]any{
		"input":                base64.StdEncoding.EncodeToString(digest),
		"prehashed":            true,
		"marshaling_algorithm": "asn1",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to sign: %w", err)
	}
	if secret == nil || secret.Data == nil {
		return nil, fmt.Errorf("got response for signing, but was nil")
	}
	raw, ok := secret.Data["signature"]
	if !ok {
		return nil, fmt.Errorf("response does not have 'signature' key")
	}
	signature, ok := raw.(string)
	if !ok {
		return nil, fmt.Errorf("signature is not a string")
	}

	// Vault returns "vault:vX:BASE64_SIG"; extract the raw signature.
	parts := strings.SplitN(signature, ":", 3)
	b, err := util.Base64Decode(parts[len(parts)-1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode signature: %w", err)
	}
	return b, nil
}

func (s *Signer) getPublicKey() (crypto.PublicKey, error) {
	secret, err := s.client.Logical().Read(fmt.Sprintf("transit/keys/%s", s.name))
	if err != nil {
		return nil, fmt.Errorf("failed to get public key for %v: %w", s.name, err)
	}
	if secret == nil || secret.Data == nil {
		return nil, fmt.Errorf("found %v, but public key was empty", s.name)
	}

	typ, ok := secret.Data["type"].(string)
	if !ok {
		return nil, fmt.Errorf("missing or invalid type field")
	}
	if typ != "ecdsa-p256" {
		return nil, fmt.Errorf("invalid key type %v: expected ecdsa-p256", typ)
	}

	m, ok := secret.Data["keys"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%v does not contain public keys", s.name)
	}
	keyTyped, ok := m[s.version].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%v has no version %v", s.name, s.version)
	}
	publicKeyPEM, ok := keyTyped["public_key"].(string)
	if !ok {
		return nil, fmt.Errorf("missing or invalid public_key field")
	}
	return keys.ParseECDSAPublicKey(publicKeyPEM)
}
