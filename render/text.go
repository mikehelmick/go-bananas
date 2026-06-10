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

package render

import (
	"bytes"
	"io"
)

// RenderText executes the named text template (loaded from a ".txt" file) into
// w with the provided data. Unlike the HTML/JSON helpers it writes to any
// [io.Writer] and sets no HTTP headers, so it is suitable for rendering plain
// text such as emails, config files, or CLI output. The text FuncMap is
// configured with [WithTextFuncs].
//
// It renders into a buffer first and only copies to w on success, so a template
// error never produces partial output. In dev mode templates are reloaded on
// each call.
func (r *Renderer) RenderText(w io.Writer, name string, data any) error {
	if r.debug {
		if err := r.loadTemplates(); err != nil {
			return err
		}
	}

	b := r.rendererPool.Get().(*bytes.Buffer)
	b.Reset()
	defer r.rendererPool.Put(b)

	if err := r.executeTextTemplate(b, name, data); err != nil {
		return err
	}

	_, err := b.WriteTo(w)
	return err
}
