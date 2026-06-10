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
	"encoding/json"
	"fmt"
	"strings"
)

// JSONExpander wraps a [SecretManager] and adds JSON field extraction: a secret
// name containing a period selects a field from a JSON-valued secret.
type JSONExpander struct {
	sm SecretManager
}

// WrapJSONExpander wraps sm with JSON-expansion behavior (see [JSONExpander]).
func WrapJSONExpander(_ context.Context, sm SecretManager) (SecretManager, error) {
	return &JSONExpander{sm: sm}, nil
}

// GetSecretValue returns the named secret. If name contains a period, the part
// before the first period names a secret whose value is parsed as JSON, and the
// remaining dotted path selects a (possibly nested) string field.
//
// For example, if the secret "psqlcreds" holds
// {"username":"gandalf","password":"abc"}, then GetSecretValue(ctx,
// "psqlcreds.username") returns "gandalf".
func (sm *JSONExpander) GetSecretValue(ctx context.Context, name string) (string, error) {
	parts := strings.Split(name, ".")
	if len(parts) == 1 {
		return sm.sm.GetSecretValue(ctx, name)
	}
	secretName := parts[0]
	jsonExpansionPath := parts[1:]

	smValue, err := sm.sm.GetSecretValue(ctx, secretName)
	if err != nil {
		return "", err
	}

	var root map[string]any
	if err := json.Unmarshal([]byte(smValue), &root); err != nil {
		return "", err
	}

	// Walk the dotted path: every element except the last must resolve to a
	// nested object, and the final element must resolve to a string.
	var cur any = root
	for i, p := range jsonExpansionPath {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", fmt.Errorf("cannot expand %q: %q is not an object", name, strings.Join(jsonExpansionPath[:i], "."))
		}
		cur, ok = m[p]
		if !ok {
			return "", fmt.Errorf("cannot expand %q: missing key %q", name, p)
		}
	}

	stringValue, ok := cur.(string)
	if !ok {
		return "", fmt.Errorf("cannot expand %q: value is not a string (got %T)", name, cur)
	}
	return stringValue, nil
}
