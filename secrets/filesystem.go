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
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mikehelmick/go-bananas/internal/util"
)

func init() {
	RegisterManager("FILESYSTEM", NewFilesystem)
}

var _ SecretVersionManager = (*Filesystem)(nil)

// Filesystem is a filesystem-backed secret manager, intended for local
// development and testing. Each secret is a file under the configured root.
type Filesystem struct {
	root string
	mu   sync.Mutex
}

// NewFilesystem creates a filesystem-backed secret manager rooted at
// cfg.FilesystemRoot (created if it does not exist).
func NewFilesystem(_ context.Context, cfg *Config) (SecretManager, error) {
	root := cfg.FilesystemRoot
	if root != "" {
		if err := os.MkdirAll(root, 0o700); err != nil {
			return nil, err
		}
	}

	return &Filesystem{root: root}, nil
}

// GetSecretValue returns the contents of the file at root/name.
func (sm *Filesystem) GetSecretValue(_ context.Context, name string) (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	pth, err := util.SafeJoin(sm.root, name)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(pth)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	return string(b), nil
}

// CreateSecretVersion writes a new version file under root/parent and returns
// its reference.
func (sm *Filesystem) CreateSecretVersion(_ context.Context, parent string, data []byte) (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	root, err := util.SafeJoin(sm.root, parent)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("failed to create parent for secret version: %w", err)
	}

	version := strconv.FormatInt(time.Now().UnixNano(), 10)
	pth := filepath.Join(root, version)
	if err := os.WriteFile(pth, data, 0o600); err != nil {
		return "", fmt.Errorf("failed to create secret file: %w", err)
	}
	return strings.TrimPrefix(strings.TrimPrefix(pth, sm.root), string(filepath.Separator)), nil
}

// DestroySecretVersion removes the version file. A missing file is not an error.
func (sm *Filesystem) DestroySecretVersion(_ context.Context, name string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	pth, err := util.SafeJoin(sm.root, name)
	if err != nil {
		return err
	}
	if err := os.Remove(pth); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to destroy secret version: %w", err)
	}
	return nil
}
