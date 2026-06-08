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
	"path"
	"strconv"
	"sync"
)

func init() {
	RegisterManager("IN_MEMORY", NewInMemory)
}

var _ SecretVersionManager = (*InMemory)(nil)

// InMemory is an in-memory secret manager, primarily used for testing.
type InMemory struct {
	mu      sync.Mutex
	secrets map[string][]byte
	counter int64
}

// NewInMemory creates an empty in-memory secret manager.
func NewInMemory(_ context.Context, _ *Config) (SecretManager, error) {
	return &InMemory{
		secrets: make(map[string][]byte),
	}, nil
}

// NewInMemoryFromMap creates an in-memory secret manager seeded from m.
func NewInMemoryFromMap(_ context.Context, m map[string]string) (SecretManager, error) {
	n := make(map[string][]byte, len(m))
	for k, v := range m {
		n[k] = []byte(v)
	}
	return &InMemory{secrets: n}, nil
}

// GetSecretValue returns the named secret, or an error if it does not exist.
func (sm *InMemory) GetSecretValue(_ context.Context, k string) (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	v, ok := sm.secrets[k]
	if !ok {
		return "", fmt.Errorf("secret does not exist")
	}
	return string(v), nil
}

// CreateSecretVersion stores data under a new version of parent and returns its
// reference.
func (sm *InMemory) CreateSecretVersion(_ context.Context, parent string, data []byte) (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.counter++
	k := path.Join(parent, strconv.FormatInt(sm.counter, 10))
	sm.secrets[k] = data
	return k, nil
}

// DestroySecretVersion removes the named secret version.
func (sm *InMemory) DestroySecretVersion(_ context.Context, k string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.secrets, k)
	return nil
}
