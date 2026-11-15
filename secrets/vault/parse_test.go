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
	"reflect"
	"testing"
)

func TestParseRefNoVersion(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"secret/data/x", "secret/data/x?version=", "secret/data/x?other=1"} {
		pth, params, err := parseRef(name)
		if err != nil {
			t.Fatalf("parseRef(%q): %v", name, err)
		}
		if pth != "secret/data/x" {
			t.Errorf("parseRef(%q) path = %q, want %q", name, pth, "secret/data/x")
		}
		if params != nil {
			t.Errorf("parseRef(%q) params = %v, want nil (latest version)", name, params)
		}
	}
}

func TestParseRefVersion(t *testing.T) {
	t.Parallel()

	pth, params, err := parseRef("secret/data/x?version=3")
	if err != nil {
		t.Fatal(err)
	}
	if pth != "secret/data/x" {
		t.Errorf("path = %q, want %q", pth, "secret/data/x")
	}
	if want := map[string][]string{"version": {"3"}}; !reflect.DeepEqual(params, want) {
		t.Errorf("params = %v, want %v", params, want)
	}
}

func TestParseRefInvalid(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"secret/%zz",
		"https://evil.example/secret/data/x",
		"//evil.example/secret/data/x",
		"secret/data/x#frag",
		"file:secret",
	} {
		if _, _, err := parseRef(name); err == nil {
			t.Errorf("parseRef(%q) = nil error, want error", name)
		}
	}
}
