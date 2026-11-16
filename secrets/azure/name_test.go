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

package azure

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/profiles/latest/keyvault/keyvault"
	"github.com/Azure/go-autorest/autorest"
)

func TestGetSecretValueRejectsBadVaultName(t *testing.T) {
	t.Parallel()

	var hosts []string
	client := keyvault.New()
	client.Sender = autorest.SenderFunc(func(r *http.Request) (*http.Response, error) {
		hosts = append(hosts, r.URL.Host)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"value":"s3cret"}`)),
			Request:    r,
		}, nil
	})
	kv := &KeyVault{client: &client}

	if _, err := kv.GetSecretValue(t.Context(), "evil.com#/api-key"); err == nil {
		t.Fatal("GetSecretValue accepted a bad vault name")
	}
	if len(hosts) != 0 {
		t.Fatalf("request sent for a bad vault name: %v", hosts)
	}

	got, err := kv.GetSecretValue(t.Context(), "my-vault/api-key/1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "s3cret" {
		t.Errorf("GetSecretValue = %q, want %q", got, "s3cret")
	}
	if len(hosts) != 1 || hosts[0] != "my-vault.vault.azure.net" {
		t.Errorf("hosts = %v, want [my-vault.vault.azure.net]", hosts)
	}
}
