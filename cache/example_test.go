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

package cache_test

import (
	"fmt"
	"time"

	"github.com/mikehelmick/go-bananas/cache"
)

func ExampleCache() {
	// Create a cache of strings whose entries expire after one minute.
	c, err := cache.New[string](time.Minute)
	if err != nil {
		panic(err)
	}
	defer c.Stop()

	if err := c.Set("greeting", "hello"); err != nil {
		panic(err)
	}

	if v, ok := c.Lookup("greeting"); ok {
		fmt.Println(v)
	}
	// Output: hello
}

func ExampleCache_WriteThruLookup() {
	c, err := cache.New[int](time.Minute)
	if err != nil {
		panic(err)
	}
	defer c.Stop()

	calls := 0
	expensive := func() (int, error) {
		calls++
		return 42, nil
	}

	// The first call computes the value; the second is served from the cache.
	if _, err := c.WriteThruLookup("answer", expensive); err != nil {
		panic(err)
	}
	v, _ := c.WriteThruLookup("answer", expensive)

	fmt.Println(v, calls)
	// Output: 42 1
}
