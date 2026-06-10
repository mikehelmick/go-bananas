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

package util

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SafeJoin joins name onto root, guaranteeing the result stays within root. It
// rejects names that traverse outside root (e.g. "../etc/passwd"), guarding the
// filesystem-backed secret and key managers against path traversal via
// attacker- or config-influenced names.
func SafeJoin(root, name string) (string, error) {
	joined := filepath.Join(root, name)

	// filepath.Join cleans the path; ensure it is still within root. An empty
	// root means the current working directory, so compare against ".".
	base := root
	if base == "" {
		base = "."
	}
	rel, err := filepath.Rel(base, joined)
	if err != nil {
		return "", fmt.Errorf("invalid path %q: %w", name, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the root directory", name)
	}
	return joined, nil
}
