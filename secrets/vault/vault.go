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

// Package vault provides a HashiCorp Vault backed
// [github.com/mikehelmick/go-bananas/secrets.SecretManager].
//
// Blank-import this package to register the "HASHICORP_VAULT" provider:
//
//	import _ "github.com/mikehelmick/go-bananas/secrets/vault"
//
// The Vault SDK is imported only here, so binaries that do not use this provider
// never compile or link it. Configuration is read from the standard Vault
// environment variables (VAULT_ADDR, VAULT_TOKEN, …).
package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	vaultapi "github.com/hashicorp/vault/api"
	"github.com/mikehelmick/go-bananas/secrets"
)

func init() {
	secrets.RegisterManager("HASHICORP_VAULT", NewHashiCorpVault)
}

var _ secrets.SecretManager = (*HashiCorpVault)(nil)

// HashiCorpVault is a [github.com/mikehelmick/go-bananas/secrets.SecretManager]
// backed by HashiCorp Vault's KV v2 secrets engine.
type HashiCorpVault struct {
	client *vaultapi.Client
}

// NewHashiCorpVault creates a Vault-backed secret manager using the default
// environment-based client configuration.
func NewHashiCorpVault(_ context.Context, _ *secrets.Config) (secrets.SecretManager, error) {
	client, err := vaultapi.NewClient(nil)
	if err != nil {
		return nil, fmt.Errorf("secrets.NewHashiCorpVault: client: %w", err)
	}
	return &HashiCorpVault{client: client}, nil
}

// GetSecretValue reads a secret from Vault. The name is the secret path, with an
// optional "?version=N" query. The value is expected under data.value, matching
// the KV v2 engine:
//
//	$ vault kv put my-secret value="abc123"
//	/secret/data/my-secret  #=> { "data": { "value": "abc123" } }
//
// Dynamic secrets are technically fetchable, but lease renewal is not performed.
func (kv *HashiCorpVault) GetSecretValue(_ context.Context, name string) (string, error) {
	u, err := url.Parse(name)
	if err != nil {
		return "", fmt.Errorf("failed to parse name: %w", err)
	}

	name, version := u.Path, u.Query().Get("version")
	if version == "" {
		version = "1"
	}

	secret, err := kv.client.Logical().ReadWithData(name, map[string][]string{
		"version": {version},
	})
	if err != nil {
		return "", fmt.Errorf("failed to access secret: %w", err)
	}
	if secret == nil || secret.Data == nil {
		return "", fmt.Errorf("secret data is nil")
	}

	dataRaw, ok := secret.Data["data"]
	if !ok {
		return "", fmt.Errorf("missing 'data' key")
	}
	data, ok := dataRaw.(map[string]any)
	if !ok {
		return "", fmt.Errorf("data is not a map")
	}
	valueRaw, ok := data["value"]
	if !ok {
		return "", fmt.Errorf("missing 'value' key")
	}

	// Vault values are untyped, so coerce to a string.
	switch typ := valueRaw.(type) {
	case string:
		return typ, nil
	case []byte:
		return string(typ), nil
	case bool:
		return strconv.FormatBool(typ), nil
	case json.Number:
		return typ.String(), nil
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", typ), nil
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", typ), nil
	default:
		return "", fmt.Errorf("found secret %v, but is of unknown type %T", name, typ)
	}
}
