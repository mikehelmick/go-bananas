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

package secrets

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestInMemoryRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sm, err := NewInMemory(ctx, &Config{})
	if err != nil {
		t.Fatal(err)
	}

	vm := sm.(SecretVersionManager)
	ref, err := vm.CreateSecretVersion(ctx, "db", []byte("hunter2"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := sm.GetSecretValue(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if got != "hunter2" {
		t.Fatalf("value = %q, want hunter2", got)
	}

	if err := vm.DestroySecretVersion(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := sm.GetSecretValue(ctx, ref); err == nil {
		t.Fatal("expected error after destroy")
	}
}

func TestInMemoryFromMap(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sm, err := NewInMemoryFromMap(ctx, map[string]string{"k": "v"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := sm.GetSecretValue(ctx, "k"); got != "v" {
		t.Fatalf("value = %q, want v", got)
	}
}

func TestFilesystemRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sm, err := NewFilesystem(ctx, &Config{FilesystemRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	vm := sm.(SecretVersionManager)
	ref, err := vm.CreateSecretVersion(ctx, "api", []byte("token"))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := sm.GetSecretValue(ctx, ref); got != "token" {
		t.Fatalf("value = %q, want token", got)
	}
}

func TestRegistry(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"IN_MEMORY", "FILESYSTEM"} {
		if !slices.Contains(RegisteredManagers(), name) {
			t.Errorf("expected %q to be registered, have %v", name, RegisteredManagers())
		}
	}

	sm, err := SecretManagerFor(context.Background(), &Config{Type: "IN_MEMORY"})
	if err != nil {
		t.Fatalf("SecretManagerFor: %v", err)
	}
	if sm == nil {
		t.Fatal("expected a secret manager")
	}

	if _, err := SecretManagerFor(context.Background(), &Config{Type: "NOPE"}); err == nil {
		t.Fatal("expected error for unknown manager")
	}
}

// countingSM counts GetSecretValue calls to verify caching.
type countingSM struct{ calls int }

func (c *countingSM) GetSecretValue(context.Context, string) (string, error) {
	c.calls++
	return "value", nil
}

func TestCacher(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	underlying := &countingSM{}
	sm, err := WrapCacher(ctx, underlying, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	for range 3 {
		if _, err := sm.GetSecretValue(ctx, "k"); err != nil {
			t.Fatal(err)
		}
	}
	if underlying.calls != 1 {
		t.Fatalf("underlying called %d times, want 1", underlying.calls)
	}
}

func TestJSONExpander(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	base, _ := NewInMemoryFromMap(ctx, map[string]string{
		"creds": `{"username":"gandalf","password":"abc"}`,
	})
	sm, err := WrapJSONExpander(ctx, base)
	if err != nil {
		t.Fatal(err)
	}

	if got, _ := sm.GetSecretValue(ctx, "creds.username"); got != "gandalf" {
		t.Fatalf("value = %q, want gandalf", got)
	}
	if _, err := sm.GetSecretValue(ctx, "creds.missing"); err == nil {
		t.Fatal("expected error for missing field")
	}
}

func TestJSONExpanderNested(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	base, _ := NewInMemoryFromMap(ctx, map[string]string{
		"app": `{"db":{"user":"gandalf","tls":{"enabled":true}}}`,
	})
	sm, err := WrapJSONExpander(ctx, base)
	if err != nil {
		t.Fatal(err)
	}

	// A multi-level path descends correctly.
	if got, _ := sm.GetSecretValue(ctx, "app.db.user"); got != "gandalf" {
		t.Fatalf("value = %q, want gandalf", got)
	}
	// A final element that is an object (not a string) must error, not return "".
	if got, err := sm.GetSecretValue(ctx, "app.db.tls"); err == nil {
		t.Fatalf("expected error for non-string final element, got %q", got)
	}
	// Traversing through a non-object must error.
	if _, err := sm.GetSecretValue(ctx, "app.db.user.nope"); err == nil {
		t.Fatal("expected error traversing into a string")
	}
}

func TestResolver(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sm, _ := NewInMemoryFromMap(ctx, map[string]string{"db-pw": "s3cret"})
	cfg := &Config{SecretsDir: t.TempDir()}

	mutator := Resolver(sm, cfg)
	if mutator == nil {
		t.Fatal("expected a non-nil mutator")
	}

	// A secret:// reference is resolved. The envconfig MutatorFunc signature is
	// (ctx, originalKey, resolvedKey, originalValue, currentValue).
	got, _, err := mutator.EnvMutate(ctx, "DB_PASSWORD", "DB_PASSWORD", "secret://db-pw", "secret://db-pw")
	if err != nil {
		t.Fatal(err)
	}
	if got != "s3cret" {
		t.Fatalf("resolved = %q, want s3cret", got)
	}

	// A plain value passes through unchanged.
	got, _, err = mutator.EnvMutate(ctx, "PLAIN", "PLAIN", "literal", "literal")
	if err != nil {
		t.Fatal(err)
	}
	if got != "literal" {
		t.Fatalf("passthrough = %q, want literal", got)
	}

	// A nil secret manager yields a nil mutator.
	if Resolver(nil, cfg) != nil {
		t.Fatal("expected nil mutator for nil secret manager")
	}
}
