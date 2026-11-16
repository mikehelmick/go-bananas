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

package cache

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type order struct {
	Burgers int
	Fries   int
}

func checkSize(t *testing.T, c *Cache[*order], want int) {
	t.Helper()

	if got := c.Size(); got != want {
		t.Errorf("wrong size want: %v, got: %v", want, got)
	}
}

func TestCache(t *testing.T) {
	t.Parallel()

	duration := time.Millisecond * 500
	cache, err := New[*order](duration)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Stop()

	checkSize(t, cache, 0)

	if err := cache.Set("foo", &order{2, 1}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	time.Sleep(duration)
	if got, hit := cache.Lookup("foo"); got != nil || hit {
		t.Fatalf("key did not expire as expected")
	}

	if got, hit := cache.Lookup("bar"); got != nil || hit {
		t.Fatalf("got key that was never inserted")
	}

	want := &order{42, 37}
	if err := cache.Set("foo", want); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, hit := cache.Lookup("foo"); got == nil || !hit {
		t.Fatalf("lookup failed want: %v, got %v", want, got)
	} else if *got != *want {
		t.Fatalf("mismatch want: %v, got: %v", want, got)
	}
	time.Sleep(duration * 2)
	if got, hit := cache.Lookup("foo"); got != nil || hit {
		t.Fatalf("expected key to expire, but still available")
	}
	// potential race, yield CPU so that the purge go routine has a chance to run.
	time.Sleep(duration)
	checkSize(t, cache, 0)
}

func TestCacheClear(t *testing.T) {
	t.Parallel()

	cache, err := New[string](30 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Stop()

	if err := cache.Set("foo", "bar"); err != nil {
		t.Fatal(err)
	}
	if got, hit := cache.Lookup("foo"); got == "" || !hit {
		t.Fatalf("lookup failed got %#v", got)
	}

	cache.Clear()

	if got, _ := cache.Lookup("foo"); got != "" {
		t.Fatalf("lookup failed expected empty got %#v", got)
	}
}

func TestMarkAndSweep(t *testing.T) {
	t.Parallel()

	cache, err := New[*order](time.Millisecond * 250)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Stop()

	orderOne := &order{Burgers: 1, Fries: 2}
	orderTwo := &order{Burgers: 2, Fries: 3}

	if err := cache.Set("one", orderOne); err != nil {
		t.Fatal(err)
	}
	if err := cache.Set("two", orderTwo); err != nil {
		t.Fatal(err)
	}
	if err := cache.Set("three", orderOne); err != nil {
		t.Fatal(err)
	}

	checkSize(t, cache, 3)

	timer := time.NewTimer(time.Millisecond * 150)
	for i := 0; i < 5; i++ {
		<-timer.C
		// set two again so that it won't TTL
		if err := cache.Set("two", orderTwo); err != nil {
			t.Fatal(err)
		}
		timer.Reset(time.Millisecond * 150)
	}

	timer.Reset(time.Millisecond * 150)
	<-timer.C

	// entry "one" should have been purged
	checkSize(t, cache, 1)

	timer.Stop()
}

func TestWriteThruCache(t *testing.T) {
	t.Parallel()

	cache, err := New[*order](time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Stop()

	lookupCount := 0
	want := &order{12, 34}
	lookerUpper := func() (*order, error) {
		lookupCount++
		return want, nil
	}

	for i := 0; i < 2; i++ {
		got, err := cache.WriteThruLookup("foo", lookerUpper)
		if err != nil {
			t.Fatalf("unexpected error on WriteThruLookup: %v", err)
		}
		if *got != *want {
			t.Fatalf("mismatch want: %v, got: %v", want, got)
		}
	}

	if lookupCount != 1 {
		t.Fatalf("incorrect lookup count, want: 1, got: %v", lookupCount)
	}
}

func TestWriteThruError(t *testing.T) {
	t.Parallel()

	cache, err := New[*order](time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Stop()

	lookerUpper := func() (*order, error) {
		return nil, fmt.Errorf("nope")
	}

	got, err := cache.WriteThruLookup("foo", lookerUpper)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if err.Error() != "nope" {
		t.Errorf("incorrect error, want: `nope` got: %v", err.Error())
	}
	if got != nil {
		t.Errorf("unexpected cached item, want: nil, got %v", got)
	}
}

func TestStopIsIdempotent(t *testing.T) {
	t.Parallel()

	c, err := New[string](time.Second)
	if err != nil {
		t.Fatal(err)
	}
	// Multiple Stop calls must not panic (guards the send-on-closed-channel bug).
	c.Stop()
	c.Stop()
	c.Stop()
}

func TestInvalidDuration(t *testing.T) {
	t.Parallel()

	_, err := New[*order](-1 * time.Second)
	if !errors.Is(err, ErrInvalidDuration) {
		t.Fatalf("expected ErrInvalidDuration, got: %v", err)
	}
}

func TestConcurrentReaders(t *testing.T) {
	t.Parallel()

	cache, err := New[*order](time.Second * 5)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Stop()

	lookupCount := 0
	want := &order{12, 34}
	lookerUpper := func() (*order, error) {
		// The sleep here reliably triggers a race condition on multiple entrants
		// attempting to look up the cache miss to primary storage. Only one wins.
		time.Sleep(250 * time.Millisecond)
		lookupCount++
		return want, nil
	}

	parallel := 10
	done := make(chan error, parallel)
	for i := 0; i < parallel; i++ {
		ver := i
		go func() {
			got, err := cache.WriteThruLookup("foo", lookerUpper)
			if err != nil {
				done <- fmt.Errorf("routine: %v got unexpected error: %w", ver, err)
				return
			}
			if *got != *want {
				done <- fmt.Errorf("routine: %v mismatch want: %v, got: %v", ver, want, got)
				return
			}
			done <- nil
		}()
	}

	for i := 0; i < parallel; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("goroutines didn't terminate fast enough")
		}
	}

	if lookupCount != 1 {
		t.Errorf("unexpected lookupCount, want: 1, got: %v", lookupCount)
	}
}

// newGate returns a gate that lookups can block on. wait blocks until release
// is called; release is idempotent and also runs at test cleanup, so a failing
// test never leaves a lookup blocked.
func newGate(t *testing.T) (wait, release func()) {
	t.Helper()

	ch := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(ch) }) }
	t.Cleanup(release)
	return func() { <-ch }, release
}

// waitForWaiters blocks until n callers are waiting on the in-flight lookup for
// name, failing the test if that does not happen within a few seconds.
func waitForWaiters[T any](t *testing.T, c *Cache[T], name string, n int) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		// TryLock so a lookup that wrongly holds c.mu fails the test rather than
		// deadlocking it.
		if c.mu.TryLock() {
			got := 0
			if cl, ok := c.inflight[name]; ok {
				got = cl.dups
			}
			c.mu.Unlock()
			if got >= n {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d callers to wait on %q", n, name)
}

func TestWriteThruLookupDoesNotBlockOtherKeys(t *testing.T) {
	t.Parallel()

	c, err := New[int](time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Stop()

	started := make(chan struct{})
	gateWait, unblock := newGate(t)
	ownerDone := make(chan struct{})
	defer func() {
		unblock()
		<-ownerDone
	}()

	go func() {
		defer close(ownerDone)
		_, _ = c.WriteThruLookup("a", func() (int, error) {
			close(started)
			gateWait()
			return 1, nil
		})
	}()
	<-started

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Lookup("b")
		_ = c.Set("b", 2)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Lookup/Set for another key blocked behind an in-flight WriteThruLookup")
	}
}

func TestWriteThruLookupCoalesces(t *testing.T) {
	t.Parallel()

	c, err := New[*order](time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Stop()

	const parallel = 50
	var calls atomic.Int32
	gateWait, unblock := newGate(t)
	lookup := func() (*order, error) {
		calls.Add(1)
		gateWait()
		return &order{Burgers: 7}, nil
	}

	results := make([]*order, parallel)
	errs := make([]error, parallel)
	var wg sync.WaitGroup
	for i := range parallel {
		wg.Go(func() {
			results[i], errs[i] = c.WriteThruLookup("a", lookup)
		})
	}

	waitForWaiters(t, c, "a", parallel-1)
	unblock()
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Errorf("lookup ran %d times, want 1", got)
	}
	for i := range parallel {
		if errs[i] != nil {
			t.Errorf("caller %d: unexpected error: %v", i, errs[i])
		}
		if results[i] != results[0] {
			t.Errorf("caller %d got %p, want the shared value %p", i, results[i], results[0])
		}
	}
}

func TestWriteThruLookupErrorNotCached(t *testing.T) {
	t.Parallel()

	c, err := New[int](time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Stop()

	const parallel = 10
	errBackend := errors.New("backend down")
	var calls atomic.Int32
	gateWait, unblock := newGate(t)
	lookup := func() (int, error) {
		calls.Add(1)
		gateWait()
		return 0, errBackend
	}

	errs := make([]error, parallel)
	var wg sync.WaitGroup
	for i := range parallel {
		wg.Go(func() {
			_, errs[i] = c.WriteThruLookup("a", lookup)
		})
	}

	waitForWaiters(t, c, "a", parallel-1)
	unblock()
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Errorf("lookup ran %d times for concurrent callers, want 1", got)
	}
	for i, err := range errs {
		if !errors.Is(err, errBackend) {
			t.Errorf("caller %d: got error %v, want %v", i, err, errBackend)
		}
	}
	if got := c.Size(); got != 0 {
		t.Errorf("Size() = %d after failed lookup, want 0", got)
	}

	// The error must not be cached: the next call runs the lookup again.
	got, err := c.WriteThruLookup("a", func() (int, error) {
		calls.Add(1)
		return 5, nil
	})
	if err != nil || got != 5 {
		t.Errorf("retry: got (%d, %v), want (5, nil)", got, err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("lookup ran %d times in total, want 2", got)
	}
}

func TestWriteThruLookupPanicReleasesWaiters(t *testing.T) {
	t.Parallel()

	c, err := New[int](time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Stop()

	const waiters = 5
	gateWait, unblock := newGate(t)
	started := make(chan struct{})
	ownerPanic := make(chan any, 1)

	go func() {
		defer func() { ownerPanic <- recover() }()
		_, _ = c.WriteThruLookup("a", func() (int, error) {
			close(started)
			gateWait()
			panic("boom")
		})
	}()
	<-started

	var waiterCalls atomic.Int32
	errs := make(chan error, waiters)
	for range waiters {
		go func() {
			_, err := c.WriteThruLookup("a", func() (int, error) {
				waiterCalls.Add(1)
				return 1, nil
			})
			errs <- err
		}()
	}

	waitForWaiters(t, c, "a", waiters)
	unblock()

	if p := <-ownerPanic; p != "boom" {
		t.Errorf("owner panic = %v, want %q", p, "boom")
	}
	timeout := time.After(time.Second)
	for i := range waiters {
		select {
		case err := <-errs:
			if !errors.Is(err, ErrLookupPanicked) {
				t.Errorf("waiter %d: got error %v after the lookup panicked, want ErrLookupPanicked", i, err)
			}
		case <-timeout:
			t.Fatalf("waiter %d did not return within 1s of the panic", i)
		}
	}
	if got := waiterCalls.Load(); got != 0 {
		t.Errorf("waiters ran their own lookup %d times, want 0", got)
	}

	// The next call runs the lookup again.
	var calls atomic.Int32
	got, err := c.WriteThruLookup("a", func() (int, error) {
		calls.Add(1)
		return 9, nil
	})
	if err != nil || got != 9 || calls.Load() != 1 {
		t.Errorf("retry: got (%d, %v) with %d lookups, want (9, nil) with 1", got, err, calls.Load())
	}
}

func TestWriteThruLookupSetDuringFlightWins(t *testing.T) {
	t.Parallel()

	c, err := New[int](time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Stop()

	started := make(chan struct{})
	gateWait, unblock := newGate(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		got, err := c.WriteThruLookup("a", func() (int, error) {
			close(started)
			gateWait()
			return 1, nil
		})
		if err != nil || got != 1 {
			t.Errorf("owner: got (%d, %v), want (1, nil)", got, err)
		}
	}()
	<-started

	if err := c.Set("a", 2); err != nil {
		t.Fatal(err)
	}
	unblock()
	<-done

	if got, ok := c.Lookup("a"); !ok || got != 2 {
		t.Errorf("Lookup(a) = (%d, %v), want (2, true): a Set during the lookup must win", got, ok)
	}
}

func TestWriteThruLookupClearDuringFlight(t *testing.T) {
	t.Parallel()

	c, err := New[int](time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Stop()

	started := make(chan struct{})
	gateWait, unblock := newGate(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.WriteThruLookup("a", func() (int, error) {
			close(started)
			gateWait()
			return 1, nil
		})
	}()
	<-started

	c.Clear()
	unblock()
	<-done

	if got := c.Size(); got != 0 {
		t.Errorf("Size() = %d, want 0: a Clear during the lookup must drop its result", got)
	}
}

func TestNewRejectsHugeTTL(t *testing.T) {
	t.Parallel()

	for _, d := range []time.Duration{math.MaxInt64, MaxExpireAfter + 1} {
		if c, err := New[int](d); !errors.Is(err, ErrInvalidDuration) {
			if c != nil {
				c.Stop()
			}
			t.Errorf("New(%d): got error %v, want ErrInvalidDuration", d, err)
		}
	}

	c, err := New[int](MaxExpireAfter)
	if err != nil {
		t.Fatalf("New(MaxExpireAfter): unexpected error: %v", err)
	}
	defer c.Stop()
	if err := c.Set("a", 1); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Lookup("a"); !ok {
		t.Error("Lookup after Set missed with TTL MaxExpireAfter")
	}
}

func TestExpiredHitPurgesOnce(t *testing.T) {
	t.Parallel()

	c, err := New[int](time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Stop()

	var purges atomic.Int32
	gateWait, unblock := newGate(t)
	c.purgeHook = func() {
		purges.Add(1)
		gateWait()
	}

	c.mu.Lock()
	c.data["a"] = item[int]{object: 1, expiresAt: time.Now().Add(-time.Minute).UnixNano()}
	c.mu.Unlock()

	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			if _, ok := c.Lookup("a"); ok {
				t.Error("Lookup hit an expired entry")
			}
		})
	}
	wg.Wait()
	unblock()

	// Wait for the single purge to remove the entry.
	deadline := time.Now().Add(5 * time.Second)
	for c.Size() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("expired entry was never purged")
		}
		time.Sleep(time.Millisecond)
	}
	if got := purges.Load(); got != 1 {
		t.Errorf("100 lookups of one expired key started %d purges, want exactly 1", got)
	}
}

func TestWriteThruLookupWaitersShareContextError(t *testing.T) {
	t.Parallel()

	// Waiters share the owner's error whatever it is, including context errors;
	// they do not retry (a retry on DeadlineExceeded would chain lookups during
	// a backend outage).
	c, err := New[int](time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Stop()

	gateWait, unblock := newGate(t)
	started := make(chan struct{})
	ownerErr := make(chan error, 1)
	go func() {
		_, err := c.WriteThruLookup("a", func() (int, error) {
			close(started)
			gateWait()
			return 0, context.DeadlineExceeded
		})
		ownerErr <- err
	}()
	<-started

	var waiterCalls atomic.Int32
	waiterErr := make(chan error, 1)
	go func() {
		_, err := c.WriteThruLookup("a", func() (int, error) {
			waiterCalls.Add(1)
			return 42, nil
		})
		waiterErr <- err
	}()

	waitForWaiters(t, c, "a", 1)
	unblock()

	if err := <-ownerErr; !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("owner: got error %v, want context.DeadlineExceeded", err)
	}
	select {
	case err := <-waiterErr:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("waiter: got error %v, want the owner's context.DeadlineExceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter did not return within 1s")
	}
	if got := waiterCalls.Load(); got != 0 {
		t.Errorf("waiter ran its own lookup %d times, want 0", got)
	}
	if got := c.Size(); got != 0 {
		t.Errorf("Size() = %d, want 0", got)
	}
}
