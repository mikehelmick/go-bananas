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

// Package aws provides an AWS KMS backed
// [github.com/mikehelmick/go-bananas/keys.KeyManager].
//
// Blank-import this package to register the "AWS_KMS" provider:
//
//	import _ "github.com/mikehelmick/go-bananas/keys/aws"
//
// The AWS SDK (v2) is imported only here, so binaries that do not use this
// provider never compile or link it.
package aws

import (
	"context"
	"crypto"
	"encoding/base64"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/lstoll/awskms"
	"github.com/mikehelmick/go-bananas/keys"
)

func init() {
	keys.RegisterManager("AWS_KMS", NewKMS)
}

var _ keys.KeyManager = (*KMS)(nil)

// KMS is a [github.com/mikehelmick/go-bananas/keys.KeyManager] backed by AWS
// KMS.
type KMS struct {
	client *kms.Client
}

// NewKMS creates a new AWS KMS client using the default AWS configuration
// (environment, shared config, IAM role, etc.).
func NewKMS(ctx context.Context, _ *keys.Config) (keys.KeyManager, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return &KMS{client: kms.NewFromConfig(cfg)}, nil
}

// NewSigner returns a signer for the given KMS key.
func (s *KMS) NewSigner(ctx context.Context, keyID string) (crypto.Signer, error) {
	return awskms.NewSigner(ctx, s.client, keyID)
}

// Encrypt encrypts plaintext with the named key, binding the AAD via the KMS
// encryption context.
func (s *KMS) Encrypt(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
	output, err := s.client.Encrypt(ctx, &kms.EncryptInput{
		KeyId: &keyID,
		EncryptionContext: map[string]string{
			"aad": base64.StdEncoding.EncodeToString(aad),
		},
		Plaintext: plaintext,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt: %w", err)
	}
	return output.CiphertextBlob, nil
}

// Decrypt decrypts ciphertext with the named key and matching AAD.
func (s *KMS) Decrypt(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
	output, err := s.client.Decrypt(ctx, &kms.DecryptInput{
		KeyId: &keyID,
		EncryptionContext: map[string]string{
			"aad": base64.StdEncoding.EncodeToString(aad),
		},
		CiphertextBlob: ciphertext,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}
	return output.Plaintext, nil
}
