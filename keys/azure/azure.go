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

// Package azure provides an Azure Key Vault backed
// [github.com/mikehelmick/go-bananas/keys.KeyManager].
//
// Blank-import this package to register the "AZURE_KEY_VAULT" provider:
//
//	import _ "github.com/mikehelmick/go-bananas/keys/azure"
//
// The Azure SDK is imported only here, so binaries that do not use this provider
// never compile or link it.
package azure

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/asn1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/keyvault/v7.0/keyvault"
	"github.com/Azure/go-autorest/autorest"
	"github.com/mikehelmick/go-bananas/internal/azureauth"
	"github.com/mikehelmick/go-bananas/internal/util"
	"github.com/mikehelmick/go-bananas/keys"
)

func init() {
	keys.RegisterManager("AZURE_KEY_VAULT", NewAzureKeyVault)
}

var (
	_ keys.KeyManager = (*AzureKeyVault)(nil)
	_ crypto.Signer   = (*Signer)(nil)
)

// AzureKeyVault is a [github.com/mikehelmick/go-bananas/keys.KeyManager] backed
// by Azure Key Vault.
type AzureKeyVault struct {
	client *keyvault.BaseClient
}

// keyID is a parsed "VAULT/KEY/VERSION" Key Vault reference.
type keyID struct {
	Vault   string
	Key     string
	Version string
}

func parseKeyID(id string) (*keyID, error) {
	parts := strings.SplitN(id, "/", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("key must include vaultName, keyName, and keyVersion: %v", id)
	}
	return &keyID{
		Vault:   fmt.Sprintf("https://%s.vault.azure.net", parts[0]),
		Key:     parts[1],
		Version: parts[2],
	}, nil
}

// NewAzureKeyVault creates a new Key Vault key manager authorized from the
// environment.
func NewAzureKeyVault(_ context.Context, _ *keys.Config) (keys.KeyManager, error) {
	authorizer, err := azureauth.GetKeyVaultAuthorizer()
	if err != nil {
		return nil, fmt.Errorf("keys.NewAzureKeyVault: auth: %w", err)
	}
	client := keyvault.New()
	client.Authorizer = authorizer
	return &AzureKeyVault{client: &client}, nil
}

var _ keys.SigningKeyVersion = (*signingKeyVersion)(nil)

type signingKeyVersion struct {
	kv        *AzureKeyVault
	kid       string
	createdAt time.Time
}

func (v *signingKeyVersion) KeyID() string          { return v.kid }
func (v *signingKeyVersion) CreatedAt() time.Time   { return v.createdAt.UTC() }
func (v *signingKeyVersion) DestroyedAt() time.Time { return time.Time{} }
func (v *signingKeyVersion) Signer(ctx context.Context) (crypto.Signer, error) {
	return v.kv.NewSigner(ctx, v.kid)
}

// SigningKeyVersions returns the versions for the parent "VAULT/KEY".
func (v *AzureKeyVault) SigningKeyVersions(ctx context.Context, parent string) ([]keys.SigningKeyVersion, error) {
	parts := strings.SplitN(parent, "/", 2)
	if len(parts) < 2 {
		return nil, fmt.Errorf("key must include vaultName, keyName: %v", parent)
	}
	vaultID := fmt.Sprintf("https://%s.vault.azure.net", parts[0])
	keyName := parts[1]

	maxResults := int32(512)
	resp, err := v.client.GetKeyVersionsComplete(ctx, vaultID, keyName, &maxResults)
	if err != nil {
		return nil, err
	}

	var versions []keys.SigningKeyVersion
	for resp.NotDone() {
		if err := resp.NextWithContext(ctx); err != nil {
			return nil, fmt.Errorf("failed to list: %w", err)
		}
		key := resp.Value()
		versions = append(versions, &signingKeyVersion{
			kv:        v,
			kid:       *key.Kid,
			createdAt: time.Time(*key.Attributes.Created),
		})
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].(*signingKeyVersion).kid < versions[j].(*signingKeyVersion).kid
	})

	return versions, nil
}

// CreateSigningKey creates a signing key under parent, returning its id. If it
// already exists, the existing id is returned.
func (v *AzureKeyVault) CreateSigningKey(ctx context.Context, parent, name string) (string, error) {
	vaultID := fmt.Sprintf("https://%s.vault.azure.net", parent)
	if _, err := v.client.GetKey(ctx, vaultID, name, ""); err != nil {
		var aerr autorest.DetailedError
		if errors.As(err, &aerr) {
			if code, ok := aerr.StatusCode.(int); ok && code == http.StatusNotFound {
				// The key does not exist yet; create it. (There is no compare-and-swap
				// in the Key Vault API, so a concurrent create could race here.)
				return v.CreateKeyVersion(ctx, fmt.Sprintf("%s/%s", parent, name))
			}
		}
		return "", err
	}
	return fmt.Sprintf("%s/%s", parent, name), nil
}

// CreateKeyVersion creates a new EC P-256 version of the parent "VAULT/KEY".
func (v *AzureKeyVault) CreateKeyVersion(ctx context.Context, parent string) (string, error) {
	parts := strings.SplitN(parent, "/", 2)
	if len(parts) < 2 {
		return "", fmt.Errorf("key must include vaultName, keyName: %v", parent)
	}
	vaultID := fmt.Sprintf("https://%s.vault.azure.net", parts[0])
	keyName := parts[1]

	resp, err := v.client.CreateKey(ctx, vaultID, keyName, keyvault.KeyCreateParameters{
		Kty:   keyvault.EC,
		Curve: keyvault.P256,
		KeyOps: &[]keyvault.JSONWebKeyOperation{
			keyvault.Sign,
			keyvault.Verify,
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to create signing key: %w", err)
	}
	if resp.Key == nil || resp.Key.Kid == nil {
		return "", fmt.Errorf("bad response")
	}
	return fmt.Sprintf("%s/%s", parent, path.Base(*resp.Key.Kid)), nil
}

// DestroyKeyVersion is unsupported on Key Vault.
func (v *AzureKeyVault) DestroyKeyVersion(_ context.Context, _ string) error {
	return fmt.Errorf("keyvault does not support destroying a key version")
}

// Encrypt encrypts plaintext with the named key. Key Vault does not support
// additional authenticated data, so aad is ignored.
func (v *AzureKeyVault) Encrypt(ctx context.Context, keyID string, plaintext, _ []byte) ([]byte, error) {
	k, err := parseKeyID(keyID)
	if err != nil {
		return nil, err
	}
	value := base64.URLEncoding.EncodeToString(plaintext)
	res, err := v.client.Encrypt(ctx, k.Vault, k.Key, k.Version, keyvault.KeyOperationsParameters{
		Algorithm: keyvault.RSAOAEP256,
		Value:     &value,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to encrypt: %w", err)
	}
	resBytes, err := base64.RawURLEncoding.DecodeString(*res.Result)
	if err != nil {
		return nil, fmt.Errorf("unable to decode encrypted data: %w", err)
	}
	return resBytes, nil
}

// Decrypt decrypts ciphertext with the named key. aad is ignored (unsupported).
func (v *AzureKeyVault) Decrypt(ctx context.Context, keyID string, ciphertext, _ []byte) ([]byte, error) {
	k, err := parseKeyID(keyID)
	if err != nil {
		return nil, err
	}
	value := base64.URLEncoding.EncodeToString(ciphertext)
	res, err := v.client.Decrypt(ctx, k.Vault, k.Key, k.Version, keyvault.KeyOperationsParameters{
		Algorithm: keyvault.RSAOAEP256,
		Value:     &value,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to decrypt: %w", err)
	}
	plaintext, err := base64.RawURLEncoding.DecodeString(*res.Result)
	if err != nil {
		return nil, fmt.Errorf("unable to decode decrypted data: %w", err)
	}
	return plaintext, nil
}

// NewSigner returns a signer for the key "VAULT/KEY/VERSION".
func (v *AzureKeyVault) NewSigner(ctx context.Context, keyID string) (crypto.Signer, error) {
	k, err := parseKeyID(keyID)
	if err != nil {
		return nil, err
	}
	return NewSigner(ctx, v.client, k.Vault, k.Key, k.Version)
}

// Signer is a [crypto.Signer] backed by an Azure Key Vault EC key.
type Signer struct {
	client    *keyvault.BaseClient
	vault     string
	key       string
	version   string
	publicKey *ecdsa.PublicKey
}

// NewSigner creates a signer for the Key Vault key, fetching its public key.
func NewSigner(ctx context.Context, client *keyvault.BaseClient, vault, key, version string) (*Signer, error) {
	if client == nil {
		return nil, fmt.Errorf("missing client")
	}
	if vault == "" {
		return nil, fmt.Errorf("missing vault")
	}
	if key == "" {
		return nil, fmt.Errorf("missing key")
	}
	if version == "" {
		return nil, fmt.Errorf("missing version")
	}

	s := &Signer{client: client, vault: vault, key: key, version: version}
	publicKey, err := s.getPublicKey(ctx)
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

// Sign signs the digest via Key Vault, converting the IEEE-1363 signature to
// ASN.1 so it interoperates with the rest of the framework.
func (s *Signer) Sign(_ io.Reader, digest []byte, _ crypto.SignerOpts) ([]byte, error) {
	ctx := context.Background()
	b64Digest := base64.RawURLEncoding.EncodeToString(digest)

	result, err := s.client.Sign(ctx, s.vault, s.key, s.version, keyvault.KeySignParameters{
		Algorithm: keyvault.ES256,
		Value:     &b64Digest,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to sign: %w", err)
	}
	if result.Result == nil {
		return nil, fmt.Errorf("signature is nil")
	}

	b, err := util.Base64Decode(*result.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to decode signature: %w", err)
	}
	b, err = convert1363ToASN1(b)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to asn1: %w", err)
	}
	return b, nil
}

func (s *Signer) getPublicKey(ctx context.Context) (*ecdsa.PublicKey, error) {
	bundle, err := s.client.GetKey(ctx, s.vault, s.key, s.version)
	if err != nil {
		return nil, fmt.Errorf("failed to get key %v from %v: %w", s.key, s.vault, err)
	}
	jsonKey := bundle.Key
	if jsonKey == nil {
		return nil, fmt.Errorf("found %v, but it is not a key", s.key)
	}
	if jsonKey.Kty != keyvault.EC {
		return nil, fmt.Errorf("found %v, but type is not EC: %v", s.key, jsonKey.Kty)
	}
	if jsonKey.Crv != keyvault.P256 {
		return nil, fmt.Errorf("found %v, but curve is not P256: %v", s.key, jsonKey.Crv)
	}
	if jsonKey.X == nil || jsonKey.Y == nil {
		return nil, fmt.Errorf("found %v, but X or Y is nil", s.key)
	}

	xRaw, err := util.Base64Decode(*jsonKey.X)
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode X: %w", err)
	}
	yRaw, err := util.Base64Decode(*jsonKey.Y)
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode Y: %w", err)
	}

	var x, y big.Int
	x.SetBytes(xRaw)
	y.SetBytes(yRaw)

	if !elliptic.P256().IsOnCurve(&x, &y) {
		return nil, fmt.Errorf("not on curve")
	}

	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: &x, Y: &y}, nil
}

// convert1363ToASN1 converts an IEEE-1363 (r||s) signature to ASN.1 DER, so it
// matches the other signers and works with standard tooling.
func convert1363ToASN1(b []byte) ([]byte, error) {
	if len(b) == 0 || len(b)%2 != 0 {
		return nil, fmt.Errorf("invalid 1363 signature length %d", len(b))
	}
	rs := struct {
		R, S *big.Int
	}{
		R: new(big.Int).SetBytes(b[:len(b)/2]),
		S: new(big.Int).SetBytes(b[len(b)/2:]),
	}
	return asn1.Marshal(rs)
}
