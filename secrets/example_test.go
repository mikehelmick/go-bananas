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

package secrets_test

import (
	"context"
	"fmt"

	"github.com/mikehelmick/go-bananas/secrets"
	// A cloud provider would be registered by blank-importing its sub-package,
	// e.g.:
	//   _ "github.com/mikehelmick/go-bananas/secrets/gcp"
)

func ExampleSecretManagerFor() {
	// The FILESYSTEM and IN_MEMORY providers are registered by the core package.
	// Select one by name via Config.Type.
	sm, err := secrets.SecretManagerFor(context.Background(), &secrets.Config{
		Type: "IN_MEMORY",
	})
	if err != nil {
		panic(err)
	}

	// IN_MEMORY also implements SecretVersionManager.
	vm := sm.(secrets.SecretVersionManager)
	ref, _ := vm.CreateSecretVersion(context.Background(), "db-password", []byte("s3cret"))
	value, _ := sm.GetSecretValue(context.Background(), ref)

	fmt.Println(value)
	// Output: s3cret
}
