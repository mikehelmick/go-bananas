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

package flash

import (
	"slices"
	"testing"
)

// fuzzValue returns a session value of a type chosen by kind, built from s, to
// stand in for whatever a decoded (possibly old or tampered) session holds.
func fuzzValue(kind uint8, s string) any {
	switch kind % 10 {
	case 0:
		return s
	case 1:
		return len(s)
	case 2:
		return nil
	case 3:
		return map[string]struct{}{s: {}}
	case 4:
		return map[string]struct{}(nil)
	case 5:
		return map[string]string{s: s}
	case 6:
		return []string{s}
	case 7:
		return flashKey(s)
	case 8:
		return []byte(s)
	default:
		return struct{ S string }{s}
	}
}

// FuzzFlashSession checks that no value stored under the flash keys can make a
// Flash panic, and that adding a message after any such value works.
func FuzzFlashSession(f *testing.F) {
	for kind := range uint8(10) {
		f.Add(kind, "hello")
	}
	f.Add(uint8(0), "")
	f.Add(uint8(3), "%s%d")

	keys := []flashKey{flashKeyAlert, flashKeyError, flashKeyWarning}

	f.Fuzz(func(t *testing.T, kind uint8, s string) {
		values := make(map[any]any, len(keys))
		for i, k := range keys {
			values[k] = fuzzValue(kind+uint8(i), s)
		}

		fl := New(values)
		fl.Clone(make(map[any]any))
		_ = fl.Alerts()
		_ = fl.Errors()
		_ = fl.Warnings()

		// Reset to the arbitrary values and add on top of them.
		for i, k := range keys {
			values[k] = fuzzValue(kind+uint8(i), s)
		}
		fl.Alert("a")
		fl.Error("e")
		fl.Warning("w")
		if got := fl.Errors(); !slices.Contains(got, "e") {
			t.Fatalf("Errors() = %q, want it to contain %q", got, "e")
		}
		if got := fl.Alerts(); !slices.Contains(got, "a") {
			t.Fatalf("Alerts() = %q, want it to contain %q", got, "a")
		}
		if got := fl.Warnings(); !slices.Contains(got, "w") {
			t.Fatalf("Warnings() = %q, want it to contain %q", got, "w")
		}
		fl.Clear()
	})
}
