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
	"net/url"
	"testing"
)

func FuzzValidateVaultName(f *testing.F) {
	for _, s := range []string{
		"my-vault1", "a", "evil.com#", "a?b", "", "abc\n",
		"aaaaaaaaaaaaaaaaaaaaaaaaa", "evil.com/x", "a@b",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, name string) {
		if err := ValidateVaultName(name); err != nil {
			return
		}
		u, err := url.Parse("https://" + name + ".vault.azure.net")
		if err != nil {
			t.Fatalf("accepted %q but URL does not parse: %v", name, err)
		}
		if want := name + ".vault.azure.net"; u.Host != want {
			t.Fatalf("accepted %q but host = %q, want %q", name, u.Host, want)
		}
		got, err := VaultURL(name)
		if err != nil || got != u.String() {
			t.Fatalf("VaultURL(%q) = %q, %v; want %q", name, got, err, u.String())
		}
	})
}
