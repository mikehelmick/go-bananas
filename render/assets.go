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
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	htmltemplate "html/template"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	texttemplate "text/template"
)

const sriPrefix = "sha512-"

var cssIncludeTmpl = texttemplate.Must(texttemplate.New(`cssIncludeTmpl`).Parse(strings.TrimSpace(`
{{ range . -}}
<link rel="stylesheet" href="/{{.Path}}?{{.BuildID}}"
  integrity="{{.SRI}}" crossorigin="anonymous" referrerpolicy="no-referrer" />
{{ end }}
`)))

var jsIncludeTmpl = texttemplate.Must(texttemplate.New(`jsIncludeTmpl`).Parse(strings.TrimSpace(`
{{ range . -}}
<script defer src="/{{.Path}}?{{.BuildID}}"
  integrity="{{.SRI}}" crossorigin="anonymous" referrerpolicy="no-referrer"></script>
{{ end }}
`)))

// asset represents a javascript or css asset.
type asset struct {
	// Path is the virtual path, relative to the URL root.
	Path string

	// SRI is the Subresource Integrity hash.
	SRI string

	// BuildID is the renderer's build identifier, used for cache-busting.
	BuildID string
}

// assetIncludeTag searches the renderer's FS for all assets of the given search
// type and renders tmpl. In non-debug mode the result is memoized on the
// Renderer (cache), so each Renderer maintains its own cache rather than sharing
// process-global state.
func (r *Renderer) assetIncludeTag(search string, tmpl *texttemplate.Template, cache *htmltemplate.HTML) func() (htmltemplate.HTML, error) {
	return func() (htmltemplate.HTML, error) {
		if !r.debug {
			r.assetCacheLock.Lock()
			defer r.assetCacheLock.Unlock()
			if *cache != "" {
				return *cache, nil
			}
		}

		entries, err := fs.ReadDir(r.fs, search)
		if err != nil {
			return "", fmt.Errorf("failed to read entries: %w", err)
		}

		list := make([]*asset, 0, len(entries))
		for _, entry := range entries {
			name := entry.Name()
			pth := filepath.Join(search, name)

			f, err := r.fs.Open(pth)
			if err != nil {
				return "", fmt.Errorf("failed to open %s: %w", name, err)
			}

			integrity, err := generateSRI(f)
			if err != nil {
				return "", fmt.Errorf("failed to generate SRI for %s: %w", name, err)
			}

			list = append(list, &asset{
				Path:    pth,
				SRI:     integrity,
				BuildID: r.buildID,
			})
		}

		var b bytes.Buffer
		if err := tmpl.Execute(&b, list); err != nil {
			return "", fmt.Errorf("failed to render %s asset: %w", search, err)
		}
		result := htmltemplate.HTML(b.String())

		if !r.debug {
			*cache = result
		}

		return result, nil
	}
}

// generateSRI generates a Subresource Integrity hash from r, closing it.
func generateSRI(r io.ReadCloser) (string, error) {
	defer r.Close()

	h := sha512.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return sriPrefix + base64.RawStdEncoding.EncodeToString(h.Sum(nil)), nil
}
