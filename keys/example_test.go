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

package keys_test

import (
	"context"
	"fmt"

	"github.com/mikehelmick/go-bananas/keys"
	// A cloud provider would be registered by blank-importing its sub-package,
	// e.g.:
	//   _ "github.com/mikehelmick/go-bananas/keys/gcp"
)

func ExampleKeyManagerFor() {
	// The FILESYSTEM provider is registered by the core package and is suitable
	// for local development and tests.
	km, err := keys.KeyManagerFor(context.Background(), &keys.Config{
		Type:           "FILESYSTEM",
		FilesystemRoot: "", // current working directory
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(km != nil)
	// Output: true
}
