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

// Package gcp provides a Google Cloud KMS backed
// [github.com/mikehelmick/go-bananas/keys.KeyManager].
//
// Blank-import this package to register the "GOOGLE_CLOUD_KMS" provider, then
// select it via [github.com/mikehelmick/go-bananas/keys.Config.Type]:
//
//	import _ "github.com/mikehelmick/go-bananas/keys/gcp"
//
// Because the Google Cloud SDK is only imported here, binaries that do not use
// this provider never compile or link it.
package gcp

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"time"

	kms "cloud.google.com/go/kms/apiv1"
	"cloud.google.com/go/kms/apiv1/kmspb"
	"github.com/mikehelmick/go-bananas/keys"
	"github.com/sethvargo/go-gcpkms/pkg/gcpkms"
	"github.com/sethvargo/go-retry"
	"google.golang.org/api/iterator"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func init() {
	keys.RegisterManager("GOOGLE_CLOUD_KMS", NewGoogleCloudKMS)
}

var (
	_ keys.KeyManager        = (*GoogleCloudKMS)(nil)
	_ keys.SigningKeyManager = (*GoogleCloudKMS)(nil)
)

// GoogleCloudKMS is a [github.com/mikehelmick/go-bananas/keys.KeyManager] backed
// by Google Cloud KMS.
type GoogleCloudKMS struct {
	client *kms.KeyManagementClient
	useHSM bool
}

// CloudKMSSigningKeyVersion is a [github.com/mikehelmick/go-bananas/keys.SigningKeyVersion]
// for a Cloud KMS crypto key version.
type CloudKMSSigningKeyVersion struct {
	keyID       string
	createdAt   time.Time
	destroyedAt time.Time
	keyManager  *GoogleCloudKMS
}

// KeyID returns the key version's resource name.
func (k *CloudKMSSigningKeyVersion) KeyID() string { return k.keyID }

// CreatedAt returns when the key version was created.
func (k *CloudKMSSigningKeyVersion) CreatedAt() time.Time { return k.createdAt }

// DestroyedAt returns when the key version was destroyed, if ever.
func (k *CloudKMSSigningKeyVersion) DestroyedAt() time.Time { return k.destroyedAt }

// Signer returns a signer for this key version.
func (k *CloudKMSSigningKeyVersion) Signer(ctx context.Context) (crypto.Signer, error) {
	return k.keyManager.NewSigner(ctx, k.keyID)
}

// NewGoogleCloudKMS creates a new Cloud KMS key manager using Application
// Default Credentials.
func NewGoogleCloudKMS(ctx context.Context, cfg *keys.Config) (keys.KeyManager, error) {
	client, err := kms.NewKeyManagementClient(ctx)
	if err != nil {
		return nil, err
	}
	return &GoogleCloudKMS{client: client, useHSM: cfg.CreateHSMKeys}, nil
}

// NewSigner returns a signer for the named Cloud KMS key.
func (k *GoogleCloudKMS) NewSigner(ctx context.Context, keyID string) (crypto.Signer, error) {
	return gcpkms.NewSigner(ctx, k.client, keyID)
}

// Encrypt encrypts plaintext with the named key and optional AAD.
func (k *GoogleCloudKMS) Encrypt(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
	result, err := k.client.Encrypt(ctx, &kmspb.EncryptRequest{
		Name:                        keyID,
		Plaintext:                   plaintext,
		AdditionalAuthenticatedData: aad,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt: %w", err)
	}
	return result.Ciphertext, nil
}

// Decrypt decrypts ciphertext with the named key and optional AAD.
func (k *GoogleCloudKMS) Decrypt(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
	result, err := k.client.Decrypt(ctx, &kmspb.DecryptRequest{
		Name:                        keyID,
		Ciphertext:                  ciphertext,
		AdditionalAuthenticatedData: aad,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}
	return result.Plaintext, nil
}

// CreateSigningKey creates an asymmetric-signing crypto key. If it already
// exists, its resource name is returned.
func (k *GoogleCloudKMS) CreateSigningKey(ctx context.Context, parent, name string) (string, error) {
	result, err := k.client.CreateCryptoKey(ctx, &kmspb.CreateCryptoKeyRequest{
		Parent:      parent,
		CryptoKeyId: name,
		CryptoKey: &kmspb.CryptoKey{
			Purpose: kmspb.CryptoKey_ASYMMETRIC_SIGN,
			VersionTemplate: &kmspb.CryptoKeyVersionTemplate{
				ProtectionLevel: k.protectionLevel(),
				Algorithm:       kmspb.CryptoKeyVersion_EC_SIGN_P256_SHA256,
			},
		},
	})
	if err != nil {
		if grpcstatus.Code(err) == grpccodes.AlreadyExists {
			return fmt.Sprintf("%s/cryptoKeys/%s", parent, name), nil
		}
		return "", fmt.Errorf("failed to create signing key: %w", err)
	}
	return result.Name, nil
}

// SigningKeyVersions lists the enabled signing-key versions for the parent.
func (k *GoogleCloudKMS) SigningKeyVersions(ctx context.Context, parent string) ([]keys.SigningKeyVersion, error) {
	results := make([]keys.SigningKeyVersion, 0, 32)

	it := k.client.ListCryptoKeyVersions(ctx, &kmspb.ListCryptoKeyVersionsRequest{
		Parent:   parent,
		PageSize: 200,
		Filter:   `state = "ENABLED"`,
	})
	for {
		resp, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list keys: %w", err)
		}

		key := &CloudKMSSigningKeyVersion{
			keyID:      resp.Name,
			keyManager: k,
		}
		if t := resp.GetCreateTime(); t != nil {
			key.createdAt = t.AsTime()
		}
		if t := resp.GetDestroyEventTime(); t != nil {
			key.destroyedAt = t.AsTime()
		}

		results = append(results, key)
	}

	return results, nil
}

// CreateKeyVersion creates a new version of the parent key, waiting until it is
// enabled.
func (k *GoogleCloudKMS) CreateKeyVersion(ctx context.Context, parent string) (string, error) {
	result, err := k.client.CreateCryptoKeyVersion(ctx, &kmspb.CreateCryptoKeyVersionRequest{
		Parent: parent,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create key version: %w", err)
	}

	b := retry.WithMaxRetries(10, retry.NewConstant(500*time.Millisecond))
	if err := retry.Do(ctx, b, func(ctx context.Context) error {
		version, err := k.client.GetCryptoKeyVersion(ctx, &kmspb.GetCryptoKeyVersionRequest{
			Name: result.Name,
		})
		if err != nil {
			return fmt.Errorf("failed to validate if key was created: %w", err)
		}
		if version.State == kmspb.CryptoKeyVersion_ENABLED {
			return nil
		}
		return retry.RetryableError(fmt.Errorf("key is not ready (%s)", version.State))
	}); err != nil {
		return "", err
	}

	return result.Name, nil
}

// DestroyKeyVersion marks the given key version for destruction. A missing or
// already-destroyed version is not an error.
func (k *GoogleCloudKMS) DestroyKeyVersion(ctx context.Context, id string) error {
	if _, err := k.client.DestroyCryptoKeyVersion(ctx, &kmspb.DestroyCryptoKeyVersionRequest{
		Name: id,
	}); err != nil {
		code := grpcstatus.Code(err)
		if code == grpccodes.NotFound || code == grpccodes.FailedPrecondition {
			return nil
		}
		return fmt.Errorf("failed to destroy key version: %w", err)
	}
	return nil
}

func (k *GoogleCloudKMS) protectionLevel() kmspb.ProtectionLevel {
	if k.useHSM {
		return kmspb.ProtectionLevel_HSM
	}
	return kmspb.ProtectionLevel_SOFTWARE
}
