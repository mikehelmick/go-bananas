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
	"fmt"
	"regexp"
)

// vaultNameRe matches a valid Key Vault name: 3-24 letters, digits and dashes.
var vaultNameRe = regexp.MustCompile(`^[A-Za-z0-9-]{3,24}$`)

// ValidateVaultName returns an error unless name is a valid Key Vault name.
// The name is formatted into the vault URL that receives the bearer token, so
// anything else (".", "/", "#", "?", "@", ...) could redirect the token to
// another host.
func ValidateVaultName(name string) error {
	if !vaultNameRe.MatchString(name) {
		return fmt.Errorf("invalid Azure Key Vault name %q: must be 3-24 letters, digits or dashes", name)
	}
	return nil
}

// VaultURL returns the base URL ("https://NAME.vault.azure.net") of the named
// Key Vault, after validating the name with [ValidateVaultName].
func VaultURL(name string) (string, error) {
	if err := ValidateVaultName(name); err != nil {
		return "", err
	}
	return "https://" + name + ".vault.azure.net", nil
}
