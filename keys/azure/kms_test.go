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
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/Azure/azure-sdk-for-go/services/keyvault/v7.0/keyvault"
	"github.com/Azure/go-autorest/autorest"
	"github.com/mikehelmick/go-bananas/logging"
)

// fakeKeyVault returns a KeyVault whose client answers every request with
// body (HTTP 200) and records the requested hosts, so no network or
// credentials are needed.
func fakeKeyVault(t *testing.T, body string) (*KeyVault, func() []string) {
	t.Helper()
	return fakeKeyVaultFunc(t, func(*http.Request) string { return body })
}

// fakeKeyVaultFunc is fakeKeyVault with a per-request response body.
func fakeKeyVaultFunc(t *testing.T, bodyFn func(*http.Request) string) (*KeyVault, func() []string) {
	t.Helper()

	var mu sync.Mutex
	var hosts []string

	client := keyvault.New()
	client.Sender = autorest.SenderFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		hosts = append(hosts, r.URL.Host)
		mu.Unlock()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(bodyFn(r))),
			Request:    r,
		}, nil
	})
	return &KeyVault{client: &client}, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), hosts...)
	}
}

func TestEncryptRejectsAAD(t *testing.T) {
	t.Parallel()

	_, err := (&KeyVault{}).Encrypt(t.Context(), "v/k/1", []byte("p"), []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "does not support additional authenticated data") {
		t.Fatalf("Encrypt with AAD = %v, want AAD error", err)
	}
}

func TestDecryptWarnsOnAAD(t *testing.T) {
	t.Parallel()

	result := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	kv, _ := fakeKeyVault(t, `{"kid":"https://myvault.vault.azure.net/keys/k/1","value":"`+result+`"}`)

	var buf bytes.Buffer
	ctx := logging.WithLogger(context.Background(), slog.New(slog.NewTextHandler(&buf, nil)))

	for range 2 {
		got, err := kv.Decrypt(ctx, "myvault/k/1", []byte("ciphertext"), []byte("aad"))
		if err != nil {
			t.Fatalf("Decrypt with AAD: %v", err)
		}
		if string(got) != "hello" {
			t.Fatalf("Decrypt = %q, want %q", got, "hello")
		}
	}
	if _, err := kv.Decrypt(ctx, "myvault/k/1", []byte("ciphertext"), nil); err != nil {
		t.Fatalf("Decrypt without AAD: %v", err)
	}

	if n := strings.Count(buf.String(), "does not support additional authenticated data"); n != 1 {
		t.Fatalf("AAD warning logged %d times, want 1; log:\n%s", n, buf.String())
	}
}

func TestSigningKeyVersionIDs(t *testing.T) {
	t.Parallel()

	_, x, y := p256Coords(t, false)
	enc := base64.RawURLEncoding.EncodeToString
	var gotKeyPaths []string
	kv, hosts := fakeKeyVaultFunc(t, func(r *http.Request) string {
		if strings.HasSuffix(r.URL.Path, "/versions") {
			// Listed out of order: ddd has no created time (sorts last), ccc is
			// disabled (omitted), and bbb is the newest.
			return `{"value":[` +
				`{"kid":"https://myvault.vault.azure.net/keys/k/ddd","attributes":{"enabled":true}},` +
				`{"kid":"https://myvault.vault.azure.net/keys/k/bbb","attributes":{"enabled":true,"created":1700000250}},` +
				`{"kid":"https://myvault.vault.azure.net/keys/k/ccc","attributes":{"enabled":false,"created":1700000300}},` +
				`{"kid":"https://myvault.vault.azure.net/keys/k/aaa","attributes":{"enabled":true,"created":1700000200}}` +
				`]}`
		}
		gotKeyPaths = append(gotKeyPaths, r.URL.Path)
		return `{"key":{"kid":"https://myvault.vault.azure.net/keys/k/aaa","kty":"EC","crv":"P-256",` +
			`"x":"` + enc(x) + `","y":"` + enc(y) + `"}}`
	})

	versions, err := kv.SigningKeyVersions(t.Context(), "myvault/k")
	if err != nil {
		t.Fatal(err)
	}

	ids := make([]string, 0, len(versions))
	for _, v := range versions {
		ids = append(ids, v.KeyID())
	}
	// Newest first (keys.SigningKeyManager contract).
	if want := []string{"myvault/k/bbb", "myvault/k/aaa", "myvault/k/ddd"}; strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("KeyIDs = %v, want %v", ids, want)
	}

	for _, id := range ids {
		k, err := parseKeyID(id)
		if err != nil {
			t.Fatalf("parseKeyID(%q): %v", id, err)
		}
		if k.Vault != "https://myvault.vault.azure.net" || k.Key != "k" {
			t.Errorf("parseKeyID(%q) = %+v", id, k)
		}
	}

	// The listed ID round-trips through Signer -> NewSigner -> parseKeyID.
	if _, err := versions[0].Signer(t.Context()); err != nil {
		t.Fatalf("Signer(%q): %v", ids[0], err)
	}
	if want := []string{"/keys/k/bbb"}; strings.Join(gotKeyPaths, ",") != strings.Join(want, ",") {
		t.Errorf("GetKey paths = %v, want %v", gotKeyPaths, want)
	}

	for _, h := range hosts() {
		if h != "myvault.vault.azure.net" {
			t.Errorf("request sent to host %q", h)
		}
	}
}

func TestRejectsBadVaultName(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	const evil = "evil.com#"
	kv, hosts := fakeKeyVault(t, `{}`)

	if _, err := parseKeyID(evil + "/k/1"); err == nil {
		t.Error("parseKeyID accepted a bad vault name")
	}
	if _, err := kv.SigningKeyVersions(ctx, evil+"/k"); err == nil {
		t.Error("SigningKeyVersions accepted a bad vault name")
	}
	if _, err := kv.CreateSigningKey(ctx, evil, "k"); err == nil {
		t.Error("CreateSigningKey accepted a bad vault name")
	}
	if _, err := kv.CreateKeyVersion(ctx, evil+"/k"); err == nil {
		t.Error("CreateKeyVersion accepted a bad vault name")
	}
	if _, err := kv.Encrypt(ctx, evil+"/k/1", []byte("p"), nil); err == nil {
		t.Error("Encrypt accepted a bad vault name")
	}
	if _, err := kv.Decrypt(ctx, evil+"/k/1", []byte("c"), nil); err == nil {
		t.Error("Decrypt accepted a bad vault name")
	}
	if _, err := kv.NewSigner(ctx, evil+"/k/1"); err == nil {
		t.Error("NewSigner accepted a bad vault name")
	}
	if h := hosts(); len(h) != 0 {
		t.Errorf("requests were sent for a bad vault name: %v", h)
	}
}
