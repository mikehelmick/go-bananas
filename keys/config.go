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

package keys

// Config configures which key manager to use. The struct tags are compatible
// with [github.com/sethvargo/go-envconfig].
type Config struct {
	// Type selects the registered provider by name (e.g. "FILESYSTEM",
	// "GOOGLE_CLOUD_KMS").
	Type string `env:"KEY_MANAGER, default=FILESYSTEM"`

	// CreateHSMKeys requests HSM-level protection when creating keys, where the
	// provider supports it. Adherence is provider-dependent and best-effort.
	CreateHSMKeys bool `env:"CREATE_HSM_KEYS, default=true"`

	// FilesystemRoot is the root path for the FILESYSTEM provider.
	FilesystemRoot string `env:"KEY_FILESYSTEM_ROOT"`
}
