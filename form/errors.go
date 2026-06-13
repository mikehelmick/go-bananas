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

package form

// Errors maps an HTML form field name to its human-readable validation
// messages. The keys are the form field names (the value of the struct's
// "form" tag), which makes Errors convenient to consume directly from HTML
// templates when re-rendering a form with inline error messages.
//
// The zero value of Errors is not usable for writing; construct one with
// make(Errors) or rely on Bind to allocate it. Read methods (Get, Has, Any)
// are safe to call on a nil Errors.
type Errors map[string][]string

// Add appends msg to the list of messages associated with field. It must be
// called on an initialized (non-nil) Errors; Bind always passes an initialized
// map, and a Validator implementation should construct one with make(Errors).
func (e Errors) Add(field, msg string) {
	e[field] = append(e[field], msg)
}

// Get returns the messages recorded for field, or nil if there are none.
// The nil return is intentional: ranging over a nil slice in a template (or
// in Go) is a no-op, so callers can write {{range .Errors.Get "email"}}
// without a guard.
func (e Errors) Get(field string) []string {
	if e == nil {
		return nil
	}
	return e[field]
}

// Has reports whether field has at least one message recorded.
func (e Errors) Has(field string) bool {
	if e == nil {
		return false
	}
	return len(e[field]) > 0
}

// Any reports whether any field has at least one message recorded. It is the
// canonical way to ask "did validation fail?" after a call to Bind.
func (e Errors) Any() bool {
	return len(e) > 0
}

// Merge copies all of the messages in o into e, appending to any existing
// messages for a given field. It is used to fold bespoke Validator results
// over tag-based results. Merging a nil or empty o is a no-op.
func (e Errors) Merge(o Errors) {
	for field, msgs := range o {
		e[field] = append(e[field], msgs...)
	}
}
