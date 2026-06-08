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

package aws

import (
	"slices"
	"testing"

	"github.com/mikehelmick/go-bananas/secrets"
)

func TestRegistered(t *testing.T) {
	t.Parallel()
	if got := secrets.RegisteredManagers(); !slices.Contains(got, "AWS_SECRETS_MANAGER") {
		t.Fatalf("expected %q to be registered, have %v", "AWS_SECRETS_MANAGER", got)
	}
}
