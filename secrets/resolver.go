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
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mikehelmick/go-bananas/logging"
	"github.com/sethvargo/go-envconfig"
)

const (
	// SecretPrefix marks an environment-variable value that should be resolved
	// through the secret manager.
	SecretPrefix = "secret://"

	// FileSuffix, when appended to a "secret://" value, causes the resolved
	// secret to be written to a file and the file path returned instead.
	FileSuffix = "?target=file"
)

// Resolver returns an [github.com/sethvargo/go-envconfig.MutatorFunc] that
// resolves "secret://" references through sm during configuration loading.
// Comma-separated values are resolved element by element. If sm is nil, Resolver
// returns nil (no mutation).
func Resolver(sm SecretManager, config *Config) envconfig.MutatorFunc {
	if sm == nil {
		return nil
	}

	resolver := &secretResolver{
		sm:  sm,
		dir: config.SecretsDir,
	}

	return envconfig.MutatorFunc(func(ctx context.Context, _, resolvedKey, _, currentValue string) (string, bool, error) {
		vals := strings.Split(currentValue, ",")
		resolved := make([]string, len(vals))

		for i, val := range vals {
			s, err := resolver.resolve(ctx, resolvedKey, val)
			if err != nil {
				return "", false, err
			}
			resolved[i] = s
		}

		return strings.Join(resolved, ","), false, nil
	})
}

type secretResolver struct {
	sm  SecretManager
	dir string
}

// resolve resolves a single secret reference.
func (r *secretResolver) resolve(ctx context.Context, envName, secretRef string) (string, error) {
	logger := logging.FromContext(ctx)

	if !strings.HasPrefix(secretRef, SecretPrefix) {
		return secretRef, nil
	}

	if r.sm == nil {
		return "", fmt.Errorf("env requested secrets, but no secret manager is configured")
	}

	secretRef = strings.TrimPrefix(secretRef, SecretPrefix)

	// Determine whether the value should be written to a file.
	toFile := false
	if strings.HasSuffix(secretRef, FileSuffix) {
		toFile = true
		secretRef = strings.TrimSuffix(secretRef, FileSuffix)
	}

	logger.Info("resolving secret value", "env", envName, "toFile", toFile)

	secretVal, err := r.sm.GetSecretValue(ctx, secretRef)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %q: %w", secretRef, err)
	}

	if toFile {
		if err := r.ensureSecureDir(); err != nil {
			return "", err
		}

		secretFileName := r.filenameForSecret(envName + "." + secretRef)
		secretFilePath := filepath.Join(r.dir, secretFileName)
		if err := os.WriteFile(secretFilePath, []byte(secretVal), 0o600); err != nil {
			return "", fmt.Errorf("failed to write secret file for %q: %w", envName, err)
		}

		secretVal = secretFilePath
	}

	return secretVal, nil
}

// filenameForSecret returns a stable, filesystem-safe filename for a secret.
func (r *secretResolver) filenameForSecret(name string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(name)))
}

// ensureSecureDir creates the secrets directory with 0700 permissions, erroring
// if it already exists with broader permissions.
func (r *secretResolver) ensureSecureDir() error {
	stat, err := os.Stat(r.dir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to check if secure directory %q exists: %w", r.dir, err)
	}
	if os.IsNotExist(err) {
		if err := os.MkdirAll(r.dir, 0o700); err != nil {
			return fmt.Errorf("failed to create secure directory %q: %w", r.dir, err)
		}
	} else if perm := stat.Mode().Perm(); perm&0o077 != 0 {
		// Reject any group/other access; the directory must be private to the
		// owner so resolved secret files cannot be read by other users.
		return fmt.Errorf("secure directory %q is not restricted to the owner (mode %v)", r.dir, perm)
	}
	return nil
}
