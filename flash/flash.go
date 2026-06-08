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

// Package flash implements one-shot "flash" messages: alerts, errors, and
// warnings that are stored on a session, surfaced once on the next render, and
// then automatically cleared on read. Messages are deduplicated, so adding the
// same text twice shows it once.
//
// A Flash is backed by a session's values map, so it survives a redirect (the
// classic post/redirect/get pattern) and is compatible with
// [github.com/gorilla/sessions].
package flash

import (
	"encoding/gob"
	"fmt"
	"maps"
	"sort"
	"strings"
)

// flashKey is a custom type for inserting data into a map.
type flashKey string

const (
	flashKeyAlert   flashKey = "_alert"
	flashKeyError   flashKey = "_error"
	flashKeyWarning flashKey = "_warning"
)

func init() {
	gob.Register(flashKey(""))
	gob.Register(map[string]struct{}{})
}

// Flash is a collection of messages that are discarded on read. It is designed
// to be compatible with a session's values map.
type Flash struct {
	values map[any]any
}

// New creates a new flash handler backed by the provided values map (typically a
// session's Values). If values is nil, a new map is allocated.
func New(values map[any]any) *Flash {
	if values == nil {
		values = make(map[any]any)
	}
	return &Flash{values}
}

// Error adds a new error message to the upcoming flash. msg is a [fmt.Sprintf]
// format string applied to vars.
func (f *Flash) Error(msg string, vars ...any) {
	f.add(flashKeyError, msg, vars...)
}

// Errors returns and clears the list of error messages, if any.
func (f *Flash) Errors() []string {
	return f.get(flashKeyError)
}

// Warning adds a new warning message to the upcoming flash. msg is a
// [fmt.Sprintf] format string applied to vars.
func (f *Flash) Warning(msg string, vars ...any) {
	f.add(flashKeyWarning, msg, vars...)
}

// Warnings returns and clears the list of warning messages, if any.
func (f *Flash) Warnings() []string {
	return f.get(flashKeyWarning)
}

// Alert adds a new alert message to the upcoming flash. msg is a [fmt.Sprintf]
// format string applied to vars.
func (f *Flash) Alert(msg string, vars ...any) {
	f.add(flashKeyAlert, msg, vars...)
}

// Alerts returns and clears the list of alert messages, if any.
func (f *Flash) Alerts() []string {
	return f.get(flashKeyAlert)
}

// Clear removes all items from the flash. It is rare to call Clear since flashes
// are cleared automatically upon reading.
func (f *Flash) Clear() {
	delete(f.values, flashKeyAlert)
	delete(f.values, flashKeyError)
	delete(f.values, flashKeyWarning)
}

// Clone copies this flash data into the provided target values map.
func (f *Flash) Clone(values map[any]any) {
	maps.Copy(values, f.values)
}

// add inserts the message into the upcoming flash for the given key, ensuring
// duplicate messages are not added.
func (f *Flash) add(key flashKey, msg string, vars ...any) {
	if _, ok := f.values[key]; !ok {
		f.values[key] = make(map[string]struct{})
	}
	m := fmt.Sprintf(msg, vars...)
	f.values[key].(map[string]struct{})[m] = struct{}{}
}

// get returns the messages stored at the key, clearing them in the process. The
// returned messages are sorted case-insensitively.
func (f *Flash) get(key flashKey) []string {
	if v, ok := f.values[key]; ok {
		delete(f.values, key)

		m := v.(map[string]struct{})
		flashes := make([]string, 0, len(m))
		for k := range m {
			flashes = append(flashes, k)
		}

		sort.Slice(flashes, func(i, j int) bool {
			return strings.ToLower(flashes[i]) < strings.ToLower(flashes[j])
		})

		return flashes
	}
	return nil
}
