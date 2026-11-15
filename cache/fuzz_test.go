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
	"errors"
	"fmt"
	"testing"
	"time"
)

// FuzzCacheOps decodes ops into a sequence of Set, Lookup, WriteThruLookup and
// Clear calls over 8 keys and checks the cache against a map model. The TTL is
// long enough that nothing expires during a run.
//
// Each op is two bytes: the first picks the operation (low 2 bits) and the key
// (next 3 bits); the second is the value. For WriteThruLookup an odd value makes
// the lookup fail.
func FuzzCacheOps(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x00, 1, 0x01, 0})
	f.Add([]byte{0x02, 3, 0x02, 4, 0x01, 0})
	f.Add([]byte{0x02, 4, 0x03, 0, 0x01, 0, 0x02, 6})
	f.Add([]byte{0x00, 1, 0x04, 2, 0x08, 3, 0x03, 0, 0x05, 0})

	f.Fuzz(func(t *testing.T, ops []byte) {
		c, err := New[int](time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Stop()

		errLookup := errors.New("lookup failed")
		model := make(map[string]int)

		for i := 0; i+1 < len(ops); i += 2 {
			op := ops[i] & 0x3
			key := fmt.Sprintf("k%d", (ops[i]>>2)&0x7)
			val := int(ops[i+1])

			switch op {
			case 0: // Set
				if err := c.Set(key, val); err != nil {
					t.Fatalf("op %d: Set(%q): %v", i/2, key, err)
				}
				model[key] = val
			case 1: // Lookup
				got, hit := c.Lookup(key)
				want, wantHit := model[key]
				if got != want || hit != wantHit {
					t.Fatalf("op %d: Lookup(%q) = (%d, %v), want (%d, %v)", i/2, key, got, hit, want, wantHit)
				}
			case 2: // WriteThruLookup
				ran := false
				got, err := c.WriteThruLookup(key, func() (int, error) {
					ran = true
					if val%2 == 1 {
						return 0, errLookup
					}
					return val, nil
				})
				if want, ok := model[key]; ok {
					if ran || err != nil || got != want {
						t.Fatalf("op %d: WriteThruLookup(%q) on a hit = (%d, %v, ran=%v), want (%d, nil, ran=false)", i/2, key, got, err, ran, want)
					}
					break
				}
				if !ran {
					t.Fatalf("op %d: WriteThruLookup(%q) on a miss did not run the lookup", i/2, key)
				}
				if val%2 == 1 {
					if !errors.Is(err, errLookup) || got != 0 {
						t.Fatalf("op %d: WriteThruLookup(%q) = (%d, %v), want (0, %v)", i/2, key, got, err, errLookup)
					}
					break
				}
				if err != nil || got != val {
					t.Fatalf("op %d: WriteThruLookup(%q) = (%d, %v), want (%d, nil)", i/2, key, got, err, val)
				}
				model[key] = val
			case 3: // Clear
				c.Clear()
				clear(model)
			}

			if got, want := c.Size(), len(model); got != want {
				t.Fatalf("op %d: Size() = %d, want %d", i/2, got, want)
			}
		}
	})
}

// FuzzCacheTTL checks that New either rejects the duration, or returns a cache
// where a Set is visible to a following Lookup (for TTLs long enough that the
// entry cannot expire in between).
func FuzzCacheTTL(f *testing.F) {
	for _, d := range []int64{
		-1, 0, 1, int64(time.Second), int64(time.Second) + 1, int64(time.Hour),
		int64(MaxExpireAfter), int64(MaxExpireAfter) + 1, 1<<63 - 1, -1 << 63,
	} {
		f.Add(d)
	}

	f.Fuzz(func(t *testing.T, d int64) {
		c, err := New[int](time.Duration(d))
		valid := d >= 0 && d <= int64(MaxExpireAfter)
		if !valid {
			if !errors.Is(err, ErrInvalidDuration) {
				t.Fatalf("New(%d): got error %v, want ErrInvalidDuration", d, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("New(%d): unexpected error: %v", d, err)
		}
		defer c.Stop()

		if err := c.Set("k", 1); err != nil {
			t.Fatal(err)
		}
		got, hit := c.Lookup("k")
		if d > int64(time.Second) && (!hit || got != 1) {
			t.Fatalf("New(%d): Lookup after Set = (%d, %v), want (1, true)", d, got, hit)
		}
	})
}
