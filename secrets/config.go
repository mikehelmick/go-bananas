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

import "time"

// Config configures which secret manager to use and how. The struct tags are
// compatible with [github.com/sethvargo/go-envconfig].
type Config struct {
	// Type selects the registered provider by name (e.g. "FILESYSTEM",
	// "IN_MEMORY", "GOOGLE_SECRET_MANAGER").
	Type string `env:"SECRET_MANAGER, default=IN_MEMORY"`

	// SecretsDir is where secrets resolved to files (via the "?target=file"
	// suffix) are written.
	SecretsDir string `env:"SECRETS_DIR, default=/var/run/secrets"`

	// SecretCacheTTL is how long resolved secret values are cached.
	SecretCacheTTL time.Duration `env:"SECRET_CACHE_TTL, default=5m"`

	// SecretExpansion enables JSON expansion of secret values (see
	// [WrapJSONExpander]).
	SecretExpansion bool `env:"SECRET_EXPANSION, default=false"`

	// FilesystemRoot is the root path for the FILESYSTEM provider.
	FilesystemRoot string `env:"SECRET_FILESYSTEM_ROOT"`
}
