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

package cookiestore_test

import (
	"fmt"

	"github.com/gorilla/sessions"
	"github.com/mikehelmick/go-bananas/cookiestore"
)

func ExampleNew() {
	// entropy supplies the cookie keys. In a real application this would read
	// from a secret or key manager so keys can be rotated without a restart; each
	// key must be at least 64 bytes (32-byte encryption key + 32-byte HMAC key).
	entropy := func() ([][]byte, error) {
		return [][]byte{make([]byte, 64)}, nil
	}

	store := cookiestore.New(entropy, &sessions.Options{
		Path:     "/",
		MaxAge:   3600,
		HttpOnly: true,
		Secure:   true,
	})

	fmt.Printf("%T\n", store)
	// Output: *sessions.CookieStore
}
