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

package aws

import (
	"strings"
	"testing"
)

func FuzzParseRef(f *testing.F) {
	for _, s := range []string{
		"n",
		"n@v#AWSPREVIOUS",
		"n@v",
		"n#AWSCURRENT",
		"arn:aws:secretsmanager:us-east-1:123456789012:secret:n",
		"a@b#c@d",
		"\xff@\xfe",
		"",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, name string) {
		id, version, stage := parseRef(name)
		id2, version2, stage2 := parseRef(name)
		if id != id2 || version != version2 || stage != stage2 {
			t.Fatalf("unstable parse of %q", name)
		}
		for _, part := range []string{id, version, stage} {
			if strings.ContainsAny(part, "@#") {
				t.Fatalf("parseRef(%q) part %q contains a separator", name, part)
			}
		}
		want := len(name) - strings.Count(name, "@") - strings.Count(name, "#")
		if got := len(id) + len(version) + len(stage); got != want {
			t.Fatalf("parseRef(%q) lost or gained bytes: %d != %d", name, got, want)
		}
	})
}
