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

// Package azure provides an Azure Key Vault backed
// [github.com/mikehelmick/go-bananas/secrets.SecretManager].
//
// Blank-import this package to register the "AZURE_KEY_VAULT" provider:
//
//	import _ "github.com/mikehelmick/go-bananas/secrets/azure"
//
// The Azure SDK is imported only here, so binaries that do not use this provider
// never compile or link it. Credentials are read from the standard Azure
// environment variables.
package azure

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/profiles/latest/keyvault/keyvault"
	"github.com/mikehelmick/go-bananas/internal/azureauth"
	"github.com/mikehelmick/go-bananas/secrets"
)

func init() {
	secrets.RegisterManager("AZURE_KEY_VAULT", NewAzureKeyVault)
}

var _ secrets.SecretManager = (*AzureKeyVault)(nil)

// AzureKeyVault is a [github.com/mikehelmick/go-bananas/secrets.SecretManager]
// backed by Azure Key Vault.
type AzureKeyVault struct {
	client *keyvault.BaseClient
}

// NewAzureKeyVault creates a new Key Vault secret manager authorized from the
// environment.
func NewAzureKeyVault(_ context.Context, _ *secrets.Config) (secrets.SecretManager, error) {
	authorizer, err := azureauth.GetKeyVaultAuthorizer()
	if err != nil {
		return nil, fmt.Errorf("secrets.NewAzureKeyVault: auth: %w", err)
	}

	client := keyvault.New()
	client.Authorizer = authorizer
	return &AzureKeyVault{client: &client}, nil
}

// GetSecretValue reads a secret named "VAULT_NAME/SECRET_NAME/SECRET_VERSION"
// (the version is optional; omit it for the latest), e.g.
// "my-company-vault/api-key/1".
func (kv *AzureKeyVault) GetSecretValue(ctx context.Context, name string) (string, error) {
	var vaultName, secretName, version string
	parts := strings.SplitN(name, "/", 3)
	switch len(parts) {
	case 0, 1:
		return "", fmt.Errorf("%v is not a valid secret ref", name)
	case 2:
		vaultName, secretName, version = parts[0], parts[1], ""
	case 3:
		vaultName, secretName, version = parts[0], parts[1], parts[2]
	}

	vaultURL := fmt.Sprintf("https://%s.vault.azure.net", vaultName)
	result, err := kv.client.GetSecret(ctx, vaultURL, secretName, version)
	if err != nil {
		return "", fmt.Errorf("failed to access secret %v: %w", name, err)
	}
	if result.Value == nil {
		return "", fmt.Errorf("found secret %v, but value was nil", name)
	}
	return *result.Value, nil
}
