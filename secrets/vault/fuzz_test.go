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
	"net/url"
	"reflect"
	"testing"
)

func FuzzParseRef(f *testing.F) {
	for _, s := range []string{
		"secret/data/x",
		"secret/data/x?version=3",
		"secret/data/x?version=",
		"secret/data/x?version=1&version=2",
		"secret/%zz",
		"https://evil.example/x",
		"//evil.example/x",
		"secret/x#frag",
		"?version=1",
		"",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, name string) {
		pth, params, err := parseRef(name)
		pth2, params2, err2 := parseRef(name)
		if (err == nil) != (err2 == nil) || pth != pth2 || !reflect.DeepEqual(params, params2) {
			t.Fatalf("unstable parse of %q", name)
		}
		if err != nil {
			return
		}
		if u, err := url.Parse(name); err != nil || u.Scheme != "" || u.Host != "" || u.Fragment != "" {
			t.Fatalf("parseRef(%q) accepted a scheme, host or fragment", name)
		}
		if params == nil {
			return
		}
		v, ok := params["version"]
		if len(params) != 1 || !ok || len(v) != 1 || v[0] == "" {
			t.Fatalf("parseRef(%q) params = %v, want exactly one non-empty version", name, params)
		}
	})
}
