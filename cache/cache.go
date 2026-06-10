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

// ErrInvalidDuration is returned by [New] when the provided expiration is
// negative.
var ErrInvalidDuration = errors.New("expireAfter duration cannot be negative")

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
}

type item[T any] struct {
	object    T
	expiresAt int64
}

func (c *item[T]) expired() bool {
	return c.expiresAt < time.Now().UnixNano()
}

// New creates a new in-memory cache whose entries expire after the given
// duration. It returns [ErrInvalidDuration] if expireAfter is negative. The
// caller is responsible for calling [Cache.Stop] to stop the background
// expiration goroutine.
func New[T any](expireAfter time.Duration) (*Cache[T], error) {
	if expireAfter < 0 {
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

// purgeExpired removes an item by name and the expiry time when the purge was
// scheduled. If there is a race, and the item has been refreshed, it will not be
// purged.
func (c *Cache[T]) purgeExpired(name string, expectedExpiryTime int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if item, ok := c.data[name]; ok && item.expiresAt == expectedExpiryTime {
		// found, and the expiry time is still the same as when the purge was requested.
		delete(c.data, name)
	}
}

// Size returns the number of items in the cache.
func (c *Cache[T]) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data)
}

// Clear removes all items from the cache, regardless of their expiration.
func (c *Cache[T]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = make(map[string]item[T], initialSize)
}

// WriteThruLookup checks the cache for the value associated with name, and if
// not found or expired, invokes the provided primaryLookup function to compute
// the value, storing and returning it. Concurrent callers for the same missing
// key are coalesced so that primaryLookup runs at most once.
func (c *Cache[T]) WriteThruLookup(name string, primaryLookup Func[T]) (T, error) {
	var nilT T

	c.mu.RLock()
	val, hit := c.lookup(name)
	if hit {
		c.mu.RUnlock()
		return val, nil
	}
	c.mu.RUnlock()

	// Ensure the value hasn't been set by another goroutine by escalating to a RW
	// lock. We need the W lock anyway if we're about to write.
	c.mu.Lock()
	defer c.mu.Unlock()
	val, hit = c.lookup(name)
	if hit {
		return val, nil
	}

	// If we got this far, it was either a miss, or hit w/ expired value, execute
	// the function.
	newData, err := primaryLookup()
	if err != nil {
		return nilT, err
	}

	// Save the newData in the cache. newData may be the zero value, if that's what
	// the lookup function provided.
	c.data[name] = item[T]{
		object:    newData,
		expiresAt: time.Now().Add(c.expireAfter).UnixNano(),
	}
	return newData, nil
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
// the duration configured when the cache was created.
func (c *Cache[T]) Set(name string, object T) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[name] = item[T]{
		object:    object,
		expiresAt: time.Now().Add(c.expireAfter).UnixNano(),
	}

	return nil
}

// lookup finds an unexpired item at the given name. The bool indicates if a hit
// occurred. This is an internal API that is NOT thread-safe. Consumers must hold
// a read or read-write lock.
func (c *Cache[T]) lookup(name string) (T, bool) {
	var nilT T
	if item, ok := c.data[name]; ok && item.expired() {
		// Cache hit, but expired. The removal from the cache is deferred.
		go c.purgeExpired(name, item.expiresAt)
		return nilT, false
	} else if ok {
		// Cache hit, not expired.
		return item.object, true
	}

	// Cache miss.
	return nilT, false
}
