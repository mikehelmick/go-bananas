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

import "testing"

func TestParseRef(t *testing.T) {
	t.Parallel()

	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:n"
	cases := []struct {
		name               string
		id, version, stage string
	}{
		{"n", "n", "", ""},
		{"n@v#AWSPREVIOUS", "n", "v", "AWSPREVIOUS"},
		{"n@v", "n", "v", ""},
		{"n#AWSCURRENT", "n", "", "AWSCURRENT"},
		{arn, arn, "", ""},
		{"", "", "", ""},
	}

	for _, tc := range cases {
		id, version, stage := parseRef(tc.name)
		if id != tc.id || version != tc.version || stage != tc.stage {
			t.Errorf("parseRef(%q) = (%q, %q, %q), want (%q, %q, %q)",
				tc.name, id, version, stage, tc.id, tc.version, tc.stage)
		}
	}
}
