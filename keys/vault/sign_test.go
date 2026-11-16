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

package vault

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	vaultapi "github.com/hashicorp/vault/api"
)

func TestSignRequestKeyVersion(t *testing.T) {
	t.Parallel()

	s := &Signer{name: "k", version: "2"}
	req, err := s.signRequest([]byte("digest"))
	if err != nil {
		t.Fatal(err)
	}
	if got := req["key_version"]; got != 2 {
		t.Fatalf("key_version = %#v, want 2", got)
	}
	for _, k := range []string{"input", "prehashed", "marshaling_algorithm"} {
		if _, ok := req[k]; !ok {
			t.Errorf("sign request missing %q", k)
		}
	}

	if _, err := (&Signer{name: "k", version: "latest"}).signRequest([]byte("d")); err == nil {
		t.Error("signRequest accepted a non-numeric version")
	}
}

func TestParseKeyID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id, name, version string
	}{
		{"k@2", "k", "2"},
		{"k/2", "k", "2"},
		{"k/12", "k", "12"},
		{"k@3", "k", "3"},
	}
	for _, tc := range cases {
		k, err := parseKeyID(tc.id)
		if err != nil {
			t.Errorf("parseKeyID(%q): %v", tc.id, err)
			continue
		}
		if k.Name != tc.name || k.Version != tc.version {
			t.Errorf("parseKeyID(%q) = %+v, want %s/%s", tc.id, k, tc.name, tc.version)
		}
	}
	for _, id := range []string{"k", "/2", "k/", "k/v2", "", "k@", "k@abc", "k@-1", "k@1/2"} {
		if _, err := parseKeyID(id); err == nil {
			t.Errorf("parseKeyID(%q) = nil error, want error", id)
		}
	}
}

// fakeTransit serves a transit key "k" with versions 1..3 and accepts rotate.
func fakeTransit(t *testing.T) *vaultapi.Client {
	t.Helper()

	versions := map[string]any{}
	for i, ts := range []string{"2026-01-01T00:00:00Z", "2026-02-01T00:00:00Z", "2026-03-01T00:00:00Z"} {
		pk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		der, err := x509.MarshalPKIXPublicKey(&pk.PublicKey)
		if err != nil {
			t.Fatal(err)
		}
		versions[string(rune('1'+i))] = map[string]any{
			"creation_time": ts,
			"public_key":    string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})),
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/transit/keys/k":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"type": "ecdsa-p256", "keys": versions},
			})
		case strings.HasSuffix(r.URL.Path, "/rotate"):
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := vaultapi.DefaultConfig()
	cfg.Address = srv.URL
	client, err := vaultapi.NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	client.SetToken("test")
	return client
}

func TestSigningKeyVersionsRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	v := &HashiCorpVault{client: fakeTransit(t)}

	list, err := v.SigningKeyVersions(ctx, "k")
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(list))
	for _, ver := range list {
		ids = append(ids, ver.KeyID())
	}
	// Newest first (keys.SigningKeyManager contract).
	if got, want := strings.Join(ids, ","), "k/3,k/2,k/1"; got != want {
		t.Fatalf("KeyIDs = %s, want %s", got, want)
	}

	for _, ver := range list {
		listed, err := ver.Signer(ctx)
		if err != nil {
			t.Fatal(err)
		}
		signer, err := v.NewSigner(ctx, ver.KeyID())
		if err != nil {
			t.Fatalf("NewSigner(%q): %v", ver.KeyID(), err)
		}
		if !signer.Public().(*ecdsa.PublicKey).Equal(listed.Public()) {
			t.Errorf("NewSigner(%q) public key differs from the listed version", ver.KeyID())
		}
	}

	id, err := v.CreateKeyVersion(ctx, "k")
	if err != nil {
		t.Fatal(err)
	}
	if id != "k/3" {
		t.Errorf("CreateKeyVersion = %q, want newest %q", id, "k/3")
	}
}
