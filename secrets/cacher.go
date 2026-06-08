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

package secrets

import (
	"context"
	"time"

	"github.com/mikehelmick/go-bananas/cache"
)

var _ SecretManager = (*Cacher)(nil)

// Cacher wraps a [SecretManager], caching resolved values for a configurable
// TTL to reduce calls to the underlying provider.
type Cacher struct {
	sm    SecretManager
	cache *cache.Cache[string]
}

// WrapCacher wraps sm with an in-memory cache whose entries expire after ttl.
func WrapCacher(_ context.Context, sm SecretManager, ttl time.Duration) (SecretManager, error) {
	c, err := cache.New[string](ttl)
	if err != nil {
		return nil, err
	}
	return &Cacher{sm: sm, cache: c}, nil
}

// GetSecretValue returns the cached value if present and unexpired, otherwise it
// fetches from the wrapped manager and caches the result.
func (sm *Cacher) GetSecretValue(ctx context.Context, name string) (string, error) {
	return sm.cache.WriteThruLookup(name, func() (string, error) {
		return sm.sm.GetSecretValue(ctx, name)
	})
}
