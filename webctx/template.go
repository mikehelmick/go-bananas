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

package webctx

import (
	"context"
	"fmt"
)

// TemplateMap is the data map passed to HTML templates. Middleware accumulates
// shared values on it (flash messages, CSRF fields, request/trace IDs, locale,
// and so on) so templates can reference them by key.
type TemplateMap map[string]any

// Title sets the page title on the template map. f is a [fmt.Sprintf] format
// string applied to args. If a title already exists, the new value is prepended
// in the form "new | existing", so nested layouts can build a breadcrumb-style
// title. An empty format string is a no-op.
func (m TemplateMap) Title(f string, args ...any) {
	if f == "" {
		return
	}

	s := f
	if len(args) > 0 {
		s = fmt.Sprintf(f, args...)
	}

	if current := m["title"]; current != nil && current != "" {
		m["title"] = fmt.Sprintf("%s | %s", s, current)
		return
	}

	m["title"] = s
}

// WithTemplateMap returns a copy of ctx carrying the given template map.
func WithTemplateMap(ctx context.Context, m TemplateMap) context.Context {
	return context.WithValue(ctx, contextKeyTemplate, m)
}

// TemplateMapFromContext returns the template map stored on ctx. If none exists,
// it returns a new, empty map (never nil), so callers can populate it
// unconditionally.
func TemplateMapFromContext(ctx context.Context) TemplateMap {
	m, ok := ctx.Value(contextKeyTemplate).(TemplateMap)
	if !ok {
		return make(TemplateMap)
	}
	return m
}
