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

// Package cache implements a generic, in-memory, TTL-based cache for any object.
//
// Every entry shares the same expiration duration, configured when the cache is
// created. A background goroutine periodically purges expired entries; call
// [Cache.Stop] to release it when the cache is no longer needed.
//
// The zero value is not usable; construct a cache with [New].
package cache

import (
	"errors"
	"sync"
	"time"
)

// MaxExpireAfter is the longest expiration [New] accepts. Larger durations
// would overflow the entry's expiry timestamp, so every entry would be treated
// as already expired.
const MaxExpireAfter = 100 * 365 * 24 * time.Hour

// ErrInvalidDuration is returned by [New] when the provided expiration is
// negative or greater than [MaxExpireAfter].
var ErrInvalidDuration = errors.New("expireAfter duration must be between 0 and cache.MaxExpireAfter")

// ErrLookupPanicked is returned to callers that were waiting on a
// [Cache.WriteThruLookup] whose primary lookup panicked (or called
// runtime.Goexit). The caller that ran the lookup sees the panic itself.
var ErrLookupPanicked = errors.New("cache: primary lookup panicked")

const initialSize = 16

// Func is the signature of the lookup function passed to
// [Cache.WriteThruLookup]. It computes the value for a missing key.
type Func[T any] func() (T, error)

// Cache is a generic, thread-safe, in-memory cache where every entry expires
// after the same duration. It is safe for concurrent use by multiple
// goroutines.
type Cache[T any] struct {
	data        map[string]item[T]
	expireAfter time.Duration
	mu          sync.RWMutex
	stopChan    chan struct{}
	stopOnce    sync.Once
	ticker      *time.Ticker

	// inflight holds the WriteThruLookup calls currently running a primary
	// lookup, by key. Guarded by mu.
	inflight map[string]*call[T]

	// purging holds the keys that have a purge goroutine scheduled, so an
	// expired entry is purged once no matter how many lookups hit it. Guarded by
	// purgeMu. When both locks are held, mu is taken first.
	purgeMu sync.Mutex
	purging map[string]struct{}

	// purgeHook, when non-nil, runs at the start of each purge goroutine. It is
	// a test hook.
	purgeHook func()
}

type item[T any] struct {
	object    T
	expiresAt int64
}

func (c *item[T]) expired() bool {
	return c.expiresAt < time.Now().UnixNano()
}

// call is one in-flight primary lookup shared by every WriteThruLookup caller
// for the same key. val and err are written before wg.Done and read only after
// wg.Wait.
type call[T any] struct {
	wg  sync.WaitGroup
	val T
	err error
	// dups counts the callers that waited on this lookup. Guarded by Cache.mu.
	// It exists only so tests can tell when waiters are parked.
	dups int
}

// New creates a new in-memory cache whose entries expire after the given
// duration. It returns [ErrInvalidDuration] if expireAfter is negative or
// greater than [MaxExpireAfter]. The caller is responsible for calling
// [Cache.Stop] to stop the background expiration goroutine.
func New[T any](expireAfter time.Duration) (*Cache[T], error) {
	if expireAfter < 0 || expireAfter > MaxExpireAfter {
		return nil, ErrInvalidDuration
	}

	markInterval := expireAfter / 2
	if markInterval <= 0 {
		markInterval = time.Second
	}

	c := &Cache[T]{
		data:        make(map[string]item[T], initialSize),
		expireAfter: expireAfter,
		stopChan:    make(chan struct{}),
		ticker:      time.NewTicker(markInterval),
		inflight:    make(map[string]*call[T]),
		purging:     make(map[string]struct{}),
	}

	go c.backgroundExpire()

	return c, nil
}

func (c *Cache[T]) backgroundExpire() {
	for {
		select {
		case <-c.stopChan:
			return
		case t := <-c.ticker.C:
			c.mark(t.UnixNano())
		}
	}
}

// mark deletes entries that expired on or before t. It collects expired keys
// under a read lock, then deletes them in a single write-locked pass, avoiding
// spawning a goroutine per expired entry on every tick.
func (c *Cache[T]) mark(t int64) {
	c.mu.RLock()
	var expired []string
	for k, v := range c.data {
		if t > v.expiresAt {
			expired = append(expired, k)
		}
	}
	c.mu.RUnlock()

	if len(expired) == 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for _, k := range expired {
		// Re-check expiry under the write lock: the entry may have been refreshed
		// between the read and write locks.
		if v, ok := c.data[k]; ok && t > v.expiresAt {
			delete(c.data, k)
		}
	}
}

// Stop shuts down the background cleanup goroutine for the cache. It is safe to
// call multiple times. After Stop is called the cache should no longer be used.
func (c *Cache[T]) Stop() {
	c.ticker.Stop()
	c.stopOnce.Do(func() {
		close(c.stopChan)
	})
}

// schedulePurge starts a goroutine to purge the expired entry at name, unless
// one is already scheduled for that key. It must be called with c.mu held for
// reading or writing.
func (c *Cache[T]) schedulePurge(name string, expectedExpiryTime int64) {
	c.purgeMu.Lock()
	defer c.purgeMu.Unlock()

	// Dedup is per key, not per expiry time. If an entry expires (E1), is
	// refreshed and expires again (E2) while the E1 purge is still pending, hits
	// on E2 schedule nothing and the E1 purge leaves E2 in place. That is
	// accepted: lookups still treat E2 as a miss, and mark or the next expired
	// hit after the E1 purge finishes reclaims it.
	if _, ok := c.purging[name]; ok {
		return
	}
	c.purging[name] = struct{}{}
	go c.purgeExpired(name, expectedExpiryTime)
}

// purgeExpired removes an item by name and the expiry time when the purge was
// scheduled. If there is a race, and the item has been refreshed, it will not be
// purged.
func (c *Cache[T]) purgeExpired(name string, expectedExpiryTime int64) {
	if c.purgeHook != nil {
		c.purgeHook()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if item, ok := c.data[name]; ok && item.expiresAt == expectedExpiryTime {
		// found, and the expiry time is still the same as when the purge was requested.
		delete(c.data, name)
	}

	c.purgeMu.Lock()
	delete(c.purging, name)
	c.purgeMu.Unlock()
}

// Size returns the number of items in the cache.
func (c *Cache[T]) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data)
}

// Clear removes all items from the cache, regardless of their expiration.
//
// Clear also detaches WriteThruLookup calls that are in flight: they still
// return their result to the callers already waiting on them, but do not store
// it. Because the in-flight call is detached, a WriteThruLookup miss after Clear
// starts a second lookup for the same key, which can run concurrently with the
// first.
func (c *Cache[T]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = make(map[string]item[T], initialSize)
	c.inflight = make(map[string]*call[T])
}

// WriteThruLookup checks the cache for the value associated with name, and if
// not found or expired, invokes the provided primaryLookup function to compute
// the value, storing and returning it.
//
// Concurrent callers for the same missing key are coalesced per key: the first
// caller (the owner) runs its primaryLookup and the others wait for and share
// its result; their own primaryLookup is not called. primaryLookup runs without
// holding the cache lock, so callers for other keys, and Lookup, Set and Clear,
// are not blocked by it. Waiters block until the owner's primaryLookup returns,
// bounded only by how long that lookup takes, so primaryLookup should enforce
// its own timeout. primaryLookup must not call WriteThruLookup for the same key,
// or it deadlocks.
//
// Because waiters share the owner's result, primaryLookup must not depend on a
// single caller's context: if it uses the owner's request context and that
// request is cancelled, every waiter receives the resulting error too. Run it
// on a context detached from any one caller (for example with
// [context.WithoutCancel] plus a timeout).
//
// An error from primaryLookup, whatever it is, is returned to the owner and
// every waiter, and nothing is cached, so the next call runs the lookup again.
// If primaryLookup panics, the panic propagates to the owner and the waiters
// receive [ErrLookupPanicked].
//
// A Set or Clear for name while the lookup is running takes precedence: it
// detaches the in-flight call, whose result is returned to its callers but not
// stored. A WriteThruLookup that misses after the detach starts a second
// lookup.
func (c *Cache[T]) WriteThruLookup(name string, primaryLookup Func[T]) (T, error) {
	c.mu.RLock()
	val, hit := c.lookup(name)
	c.mu.RUnlock()
	if hit {
		return val, nil
	}

	c.mu.Lock()
	// Another goroutine may have stored the value between the two locks.
	if val, hit := c.lookup(name); hit {
		c.mu.Unlock()
		return val, nil
	}
	if cl, ok := c.inflight[name]; ok {
		cl.dups++
		c.mu.Unlock()
		cl.wg.Wait()
		return cl.val, cl.err
	}
	cl := new(call[T])
	cl.wg.Add(1)
	c.inflight[name] = cl
	c.mu.Unlock()

	return c.runLookup(name, cl, primaryLookup)
}

// runLookup runs primaryLookup for the in-flight call cl, publishes the result
// to waiters and stores it if it succeeded and cl is still the registered call
// for name.
func (c *Cache[T]) runLookup(name string, cl *call[T], primaryLookup Func[T]) (T, error) {
	returned := false
	defer func() {
		if !returned {
			// primaryLookup panicked or called runtime.Goexit. The panic keeps
			// unwinding through this caller; the waiters get an error instead.
			var zero T
			cl.val, cl.err = zero, ErrLookupPanicked
		}

		c.mu.Lock()
		// A Set or Clear while the lookup ran detaches cl; don't overwrite it.
		if c.inflight[name] == cl {
			delete(c.inflight, name)
			if cl.err == nil {
				// cl.val may be the zero value, if that's what the lookup provided.
				c.data[name] = item[T]{
					object:    cl.val,
					expiresAt: time.Now().Add(c.expireAfter).UnixNano(),
				}
			}
		}
		c.mu.Unlock()
		cl.wg.Done()
	}()

	cl.val, cl.err = primaryLookup()
	returned = true
	return cl.val, cl.err
}

// Lookup checks the cache for a non-expired object stored under name. The
// boolean return informs the caller whether there was a cache hit. A return of
// (zero, true) means the zero value is cached; (zero, false) indicates a miss or
// an expired value that should be refreshed.
func (c *Cache[T]) Lookup(name string) (T, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.lookup(name)
}

// Set saves the current value of an object in the cache. The entry expires after
// the duration configured when the cache was created. A WriteThruLookup for name
// that is in flight is detached and will not overwrite this value.
func (c *Cache[T]) Set(name string, object T) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[name] = item[T]{
		object:    object,
		expiresAt: time.Now().Add(c.expireAfter).UnixNano(),
	}
	delete(c.inflight, name)

	return nil
}

// lookup finds an unexpired item at the given name. The bool indicates if a hit
// occurred. This is an internal API that is NOT thread-safe. Consumers must hold
// a read or read-write lock.
func (c *Cache[T]) lookup(name string) (T, bool) {
	var nilT T
	if item, ok := c.data[name]; ok && item.expired() {
		// Cache hit, but expired. The removal from the cache is deferred.
		c.schedulePurge(name, item.expiresAt)
		return nilT, false
	} else if ok {
		// Cache hit, not expired.
		return item.object, true
	}

	// Cache miss.
	return nilT, false
}
