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

// cacherFetchTimeout bounds a shared fetch, which is detached from any one
// caller's cancellation.
const cacherFetchTimeout = 30 * time.Second

// GetSecretValue returns the cached value if present and unexpired, otherwise it
// fetches from the wrapped manager and caches the result.
//
// Concurrent calls for the same name share one fetch. The fetch runs on a
// context derived from the first caller's ctx that keeps its values but is
// detached from its cancellation, so cancelling one request does not fail the
// others. It ends at the first caller's deadline or after cacherFetchTimeout
// (30s), whichever is sooner, so it never runs longer than 30s.
//
// Waiters cannot cancel while a fetch is in flight: a caller's own ctx is not
// honored, and it waits for the shared fetch to finish, for at most 30s.
//
// Because the first caller's context values are used for a result shared with
// every waiter, the wrapped provider must not authorize per caller through
// context values (for example, a caller identity carried on ctx).
func (sm *Cacher) GetSecretValue(ctx context.Context, name string) (string, error) {
	return sm.cache.WriteThruLookup(name, func() (string, error) {
		fctx, cancel := fetchContext(ctx)
		defer cancel()
		return sm.sm.GetSecretValue(fctx, name)
	})
}

// fetchContext returns a context for a shared fetch: it keeps ctx's values,
// ignores its cancellation, and expires at ctx's deadline or after
// cacherFetchTimeout, whichever is sooner.
func fetchContext(ctx context.Context) (context.Context, context.CancelFunc) {
	deadline := time.Now().Add(cacherFetchTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	return context.WithDeadline(context.WithoutCancel(ctx), deadline)
}
