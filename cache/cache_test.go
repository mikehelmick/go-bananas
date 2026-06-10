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
	"fmt"
	"strings"
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
	if err == nil || !strings.Contains(err.Error(), "duration cannot be negative") {
		t.Fatalf("expected duration error, got: %v", err)
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
