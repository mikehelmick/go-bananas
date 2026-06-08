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

// Package secrets defines a pluggable abstraction over secret managers.
//
// The core package ships two providers with no external dependencies:
// "FILESYSTEM" and "IN_MEMORY". Cloud-backed providers live in sub-packages that
// self-register via a blank import, so their SDKs are only compiled into
// binaries that actually use them:
//
//	import _ "github.com/mikehelmick/go-bananas/secrets/gcp"   // GOOGLE_SECRET_MANAGER
//	import _ "github.com/mikehelmick/go-bananas/secrets/aws"   // AWS_SECRETS_MANAGER
//	import _ "github.com/mikehelmick/go-bananas/secrets/azure" // AZURE_KEY_VAULT
//	import _ "github.com/mikehelmick/go-bananas/secrets/vault" // HASHICORP_VAULT
//
// Further providers can be added the same way: a sub-package whose init function
// calls [RegisterManager].
//
// Select a provider by name through [Config.Type] and obtain it with
// [SecretManagerFor]. [Resolver] integrates with
// [github.com/sethvargo/go-envconfig] to transparently resolve "secret://"
// references during configuration loading.
package secrets

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// SecretManager is the minimum interface for reading secret values.
type SecretManager interface {
	// GetSecretValue returns the value of the secret with the given name.
	GetSecretValue(ctx context.Context, name string) (string, error)
}

// SecretVersionManager is a [SecretManager] that can also create and destroy
// secret versions.
type SecretVersionManager interface {
	SecretManager

	// CreateSecretVersion creates a new version of the named secret with the
	// given data, returning a reference to the created version.
	CreateSecretVersion(ctx context.Context, parent string, data []byte) (string, error)

	// DestroySecretVersion destroys the named secret version. Destroying a
	// version that does not exist is not an error.
	DestroySecretVersion(ctx context.Context, name string) error
}

// SecretManagerFunc constructs a [SecretManager] from configuration.
type SecretManagerFunc func(context.Context, *Config) (SecretManager, error)

var (
	managers     = make(map[string]SecretManagerFunc)
	managersLock sync.RWMutex
)

// RegisterManager registers a secret manager constructor under name. It panics
// if name is already registered. Providers call it from an init function.
func RegisterManager(name string, fn SecretManagerFunc) {
	managersLock.Lock()
	defer managersLock.Unlock()

	if _, ok := managers[name]; ok {
		panic(fmt.Sprintf("secret manager %q is already registered", name))
	}
	managers[name] = fn
}

// RegisteredManagers returns the sorted names of all registered secret managers.
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

// SecretManagerFor constructs the secret manager named by cfg.Type. It returns
// an error if no provider with that name is registered (which usually means the
// provider's sub-package was not blank-imported).
func SecretManagerFor(ctx context.Context, cfg *Config) (SecretManager, error) {
	managersLock.RLock()
	defer managersLock.RUnlock()

	name := cfg.Type
	fn, ok := managers[name]
	if !ok {
		return nil, fmt.Errorf("unknown or uncompiled secret manager %q", name)
	}
	return fn(ctx, cfg)
}
