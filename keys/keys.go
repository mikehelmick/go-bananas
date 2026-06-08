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

// Package keys defines a pluggable abstraction over key-management systems for
// signing and encryption.
//
// The core package ships a dependency-free "FILESYSTEM" provider for local
// development and testing. Cloud-backed providers live in sub-packages that
// self-register via a blank import, so their SDKs are only compiled into
// binaries that use them:
//
//	import _ "github.com/mikehelmick/go-bananas/keys/gcp"   // GOOGLE_CLOUD_KMS
//	import _ "github.com/mikehelmick/go-bananas/keys/aws"   // AWS_KMS
//	import _ "github.com/mikehelmick/go-bananas/keys/azure" // AZURE_KEY_VAULT
//	import _ "github.com/mikehelmick/go-bananas/keys/vault" // HASHICORP_VAULT
//
// Further providers can be added the same way: a sub-package whose init function
// calls [RegisterManager].
//
// Select a provider by name through [Config.Type] and obtain it with
// [KeyManagerFor].
package keys

import (
	"context"
	"crypto"
	"fmt"
	"sort"
	"sync"
	"time"
)

// KeyManager signs and encrypts using keys held in a KMS. Implementations return
// a [crypto.Signer] for signing operations.
type KeyManager interface {
	// NewSigner returns a signer for the given key.
	NewSigner(ctx context.Context, keyID string) (crypto.Signer, error)

	// Encrypt encrypts plaintext with the named key and optional additional
	// authenticated data (AAD). AAD support depends on the implementation.
	Encrypt(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error)

	// Decrypt decrypts ciphertext with the named key. The same AAD provided at
	// encryption time, if any, must be provided here.
	Decrypt(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error)
}

// KeyVersionCreator can create a new version of an existing key.
type KeyVersionCreator interface {
	// CreateKeyVersion creates a new version of the parent key, returning its id.
	CreateKeyVersion(ctx context.Context, parent string) (string, error)
}

// KeyVersionDestroyer can destroy a key version.
type KeyVersionDestroyer interface {
	// DestroyKeyVersion destroys the given key version. Destroying a version that
	// does not exist is not an error.
	DestroyKeyVersion(ctx context.Context, id string) error
}

// SigningKeyVersion describes a single signing-key version.
type SigningKeyVersion interface {
	KeyID() string
	CreatedAt() time.Time
	DestroyedAt() time.Time
	Signer(ctx context.Context) (crypto.Signer, error)
}

// SigningKeyManager supports management and rotation of signing keys.
type SigningKeyManager interface {
	// SigningKeyVersions returns the versions of the parent signing key, newest
	// first.
	SigningKeyVersions(ctx context.Context, parent string) ([]SigningKeyVersion, error)

	// CreateSigningKey creates a new signing key under parent, returning its id.
	// If it already exists, the existing id is returned.
	CreateSigningKey(ctx context.Context, parent, name string) (string, error)

	KeyVersionCreator
	KeyVersionDestroyer
}

// EncryptionKeyManager supports management and rotation of encryption keys.
type EncryptionKeyManager interface {
	// CreateEncryptionKey creates a new encryption key under parent, returning
	// its id. If it already exists, the existing id is returned.
	CreateEncryptionKey(ctx context.Context, parent, name string) (string, error)

	KeyVersionCreator
	KeyVersionDestroyer
}

// KeyManagerFunc constructs a [KeyManager] from configuration.
type KeyManagerFunc func(context.Context, *Config) (KeyManager, error)

var (
	managers     = make(map[string]KeyManagerFunc)
	managersLock sync.RWMutex
)

// RegisterManager registers a key manager constructor under name. It panics if
// name is already registered. Providers call it from an init function.
func RegisterManager(name string, fn KeyManagerFunc) {
	managersLock.Lock()
	defer managersLock.Unlock()

	if _, ok := managers[name]; ok {
		panic(fmt.Sprintf("key manager %q is already registered", name))
	}
	managers[name] = fn
}

// RegisteredManagers returns the sorted names of all registered key managers.
func RegisteredManagers() []string {
	managersLock.RLock()
	defer managersLock.RUnlock()

	list := make([]string, 0, len(managers))
	for k := range managers {
		list = append(list, k)
	}
	sort.Strings(list)
	return list
}

// KeyManagerFor constructs the key manager named by cfg.Type. It returns an
// error if no provider with that name is registered (usually meaning the
// provider's sub-package was not blank-imported).
func KeyManagerFor(ctx context.Context, cfg *Config) (KeyManager, error) {
	managersLock.RLock()
	defer managersLock.RUnlock()

	name := cfg.Type
	fn, ok := managers[name]
	if !ok {
		return nil, fmt.Errorf("unknown or uncompiled key manager %q", name)
	}
	return fn(ctx, cfg)
}
