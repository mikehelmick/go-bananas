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

package azureauth

import (
	"strings"
	"testing"
)

func TestValidateVaultName(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"my-vault1", "abc", strings.Repeat("a", 24), "A-B-C"} {
		if err := ValidateVaultName(name); err != nil {
			t.Errorf("ValidateVaultName(%q) = %v, want nil", name, err)
		}
	}
	for _, name := range []string{
		"", "a", "ab", "evil.com#", "a?b", "a/b", "a.b", "a@b", "a b",
		"abc\n", strings.Repeat("a", 25),
	} {
		if err := ValidateVaultName(name); err == nil {
			t.Errorf("ValidateVaultName(%q) = nil, want error", name)
		}
	}
}

func TestVaultURL(t *testing.T) {
	t.Parallel()

	got, err := VaultURL("my-vault1")
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://my-vault1.vault.azure.net"; got != want {
		t.Errorf("VaultURL = %q, want %q", got, want)
	}

	if _, err := VaultURL("evil.com#"); err == nil {
		t.Error("VaultURL(evil.com#) = nil error, want error")
	}
}
