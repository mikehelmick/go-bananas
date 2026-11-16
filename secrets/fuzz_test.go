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
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikehelmick/go-bananas/logging"
)

// quietCtx returns a context whose logger discards output, so fuzz iterations
// don't flood the test log.
func quietCtx() context.Context {
	return logging.WithLogger(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func FuzzFilesystemSecretName(f *testing.F) {
	// Layout, built once per worker:
	//   base/sentinel           <- outside root, must never be read
	//   base/root-sibling/s     <- outside root, shares root's name prefix
	//   base/root/inside        <- inside root, may be read
	const sentinel = "SENTINEL-OUTSIDE-ROOT"
	base := f.TempDir()
	root := filepath.Join(base, "root")
	for p, v := range map[string]string{
		filepath.Join(base, "sentinel"):          sentinel,
		filepath.Join(base, "root-sibling", "s"): sentinel,
		filepath.Join(root, "inside"):            "inside",
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			f.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(v), 0o600); err != nil {
			f.Fatal(err)
		}
	}

	ctx := quietCtx()
	sm, err := NewFilesystem(ctx, &Config{FilesystemRoot: root})
	if err != nil {
		f.Fatal(err)
	}

	for _, s := range []string{
		"inside", "../sentinel", "../root-sibling/s", "./../sentinel", "a/../../sentinel",
		"/../sentinel", "..", ".", "", "inside/..", `..\sentinel`, "../root/inside",
		filepath.Join(base, "sentinel"),
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, name string) {
		got, err := sm.GetSecretValue(ctx, name)
		if err != nil {
			return
		}
		if strings.Contains(got, sentinel) {
			t.Fatalf("GetSecretValue(%q) read a file outside the root", name)
		}
	})
}

func FuzzResolver(f *testing.F) {
	ctx := quietCtx()
	sm, err := NewInMemoryFromMap(ctx, map[string]string{
		"a":             "value-a",
		"b":             "value-b",
		"../escape":     "value-escape",
		"/abs":          "value-abs",
		"a?target=file": "nested-suffix",
	})
	if err != nil {
		f.Fatal(err)
	}

	for _, s := range []string{
		"", "plain", "a,b", "secret://a", "secret://a?target=file", "secret://../escape?target=file",
		"secret:///abs?target=file", "secret://missing", "x,secret://b?target=file,y",
		"secret://", "secret://?target=file", "secret://a?target=file?target=file",
		"SECRET://a", " secret://a", ",,,",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, value string) {
		base := t.TempDir()
		dir := filepath.Join(base, "secrets")
		mutator := Resolver(sm, &Config{SecretsDir: dir})

		got, _, err := mutator.EnvMutate(ctx, "KEY", "KEY", "", value)

		if !strings.Contains(value, SecretPrefix) {
			if err != nil {
				t.Fatalf("Resolver(%q) without %s: unexpected error %v", value, SecretPrefix, err)
			}
			if got != value {
				t.Fatalf("Resolver(%q) without %s = %q, want passthrough", value, SecretPrefix, got)
			}
		}

		// Nothing may be created anywhere under base except inside dir.
		err = filepath.WalkDir(base, func(p string, _ fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if p == base || p == dir {
				return nil
			}
			rel, err := filepath.Rel(dir, p)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				t.Errorf("Resolver(%q) created %q outside SecretsDir", value, p)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
	})
}

func FuzzJSONExpander(f *testing.F) {
	f.Add(`{"a":"x"}`, "a")
	f.Add(`{"a":{"b":"y"}}`, "a.b")
	f.Add(`{"a":{"b":1}}`, "a.b")
	f.Add(`{"a":null}`, "a.b")
	f.Add(`["a"]`, "0")
	f.Add(`"str"`, "a")
	f.Add(`{"":{"":""}}`, ".")
	f.Add(`{"a":`, "a")
	f.Add(`{}`, "")
	f.Add(`{"a":{"b":{"c":"deep"}}}`, "a.b.c.d")

	f.Fuzz(func(t *testing.T, doc, path string) {
		ctx := quietCtx()
		mem, err := NewInMemoryFromMap(ctx, map[string]string{"s": doc})
		if err != nil {
			t.Fatal(err)
		}
		sm, err := WrapJSONExpander(ctx, mem)
		if err != nil {
			t.Fatal(err)
		}

		got, err := sm.GetSecretValue(ctx, "s."+path)
		if err != nil {
			if got != "" {
				t.Fatalf("GetSecretValue(s.%q) returned value %q with error %v", path, got, err)
			}
			return
		}
		if !json.Valid([]byte(doc)) {
			t.Fatalf("GetSecretValue(s.%q) succeeded on invalid JSON %q", path, doc)
		}
	})
}
