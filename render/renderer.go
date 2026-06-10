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

// Package render renders HTML, JSON, and CSV responses from templates loaded
// out of an [io/fs.FS].
//
// A [Renderer] is created with [New] and configured with [Option] values. It
// caches parsed templates and reuses a pool of buffers so that a rendering error
// never results in a partially-written response. The default template FuncMap
// provides string, time, i18n, and asset helpers (see funcs.go); callers can add
// to or override it with [WithFuncs] and [WithTextFuncs].
//
// Templates are discovered by walking the FS: files ending in ".html" become
// HTML templates and files ending in ".txt" become text templates. Assets under
// "static/js" and "static/css" are exposed to templates as Subresource
// Integrity (SRI) tags via the jsIncludeTag and cssIncludeTag functions.
package render

import (
	"bytes"
	"fmt"
	htmltemplate "html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	texttemplate "text/template"

	"github.com/mikehelmick/go-bananas/logging"
)

// allowedResponseCodes is the set of permitted response codes. It guards against
// accidentally returning an unexpected status code.
var allowedResponseCodes = map[int]struct{}{
	http.StatusOK:                    {},
	http.StatusBadRequest:            {},
	http.StatusUnauthorized:          {},
	http.StatusNotFound:              {},
	http.StatusMethodNotAllowed:      {},
	http.StatusConflict:              {},
	http.StatusPreconditionFailed:    {},
	http.StatusRequestEntityTooLarge: {},
	http.StatusTooManyRequests:       {},
	http.StatusInternalServerError:   {},
}

// Renderer renders HTML, JSON, and CSV responses. It caches templates and uses a
// pool of buffers. A Renderer is safe for concurrent use by multiple goroutines.
type Renderer struct {
	// debug indicates templates should be reloaded on each invocation and real
	// error responses should be rendered. Do not enable in production.
	debug bool

	// logger is the fallback logger used by render methods (which take only a
	// writer, not a context).
	logger *slog.Logger

	// buildID is appended to asset URLs for cache-busting.
	buildID string

	// extraFuncs and extraTextFuncs are caller-provided funcs merged over the
	// defaults.
	extraFuncs     htmltemplate.FuncMap
	extraTextFuncs texttemplate.FuncMap

	// rendererPool is a pool of *bytes.Buffer used to render into, to prevent
	// partial responses from being sent to clients.
	rendererPool *sync.Pool

	// templates and textTemplates are the parsed template collections;
	// templatesLock guards them during hot-reload.
	templates     *htmltemplate.Template
	textTemplates *texttemplate.Template
	templatesLock sync.RWMutex

	fs fs.FS

	// jsIncludeCache and cssIncludeCache memoize the rendered asset tags in
	// non-debug mode. They are per-Renderer (guarded by assetCacheLock) so that
	// multiple Renderers in one process do not share state.
	jsIncludeCache  htmltemplate.HTML
	cssIncludeCache htmltemplate.HTML
	assetCacheLock  sync.Mutex
}

// Option configures a [Renderer] in [New].
type Option func(*Renderer)

// WithDevMode enables development mode, in which templates are reloaded on every
// render and real error details are shown. Do not enable it in production.
func WithDevMode(debug bool) Option {
	return func(r *Renderer) { r.debug = debug }
}

// WithLogger sets the fallback logger used by the renderer's methods. If unset,
// the renderer uses [logging.DefaultLogger].
func WithLogger(l *slog.Logger) Option {
	return func(r *Renderer) { r.logger = l }
}

// WithBuildID sets a build identifier appended to asset URLs (as a query string)
// for cache-busting. Change it on each deploy so clients fetch fresh assets.
func WithBuildID(id string) Option {
	return func(r *Renderer) { r.buildID = id }
}

// WithFuncs merges the provided functions into the HTML template FuncMap. The
// provided functions override the defaults on key collision, so callers can both
// add new helpers and replace built-in ones.
func WithFuncs(f htmltemplate.FuncMap) Option {
	return func(r *Renderer) {
		for k, v := range f {
			r.extraFuncs[k] = v
		}
	}
}

// WithTextFuncs merges the provided functions into the text template FuncMap,
// overriding the defaults on key collision.
func WithTextFuncs(f texttemplate.FuncMap) Option {
	return func(r *Renderer) {
		for k, v := range f {
			r.extraTextFuncs[k] = v
		}
	}
}

// New creates a new [Renderer] that loads templates from fsys, configured by the
// provided options. It returns an error if the initial template load fails.
func New(fsys fs.FS, opts ...Option) (*Renderer, error) {
	r := &Renderer{
		logger:         logging.DefaultLogger(),
		extraFuncs:     htmltemplate.FuncMap{},
		extraTextFuncs: texttemplate.FuncMap{},
		rendererPool: &sync.Pool{
			New: func() any {
				return bytes.NewBuffer(make([]byte, 0, 1024))
			},
		},
		fs: fsys,
	}

	for _, opt := range opts {
		opt(r)
	}
	if r.logger == nil {
		r.logger = logging.DefaultLogger()
	}

	// Load initial templates.
	if err := r.loadTemplates(); err != nil {
		return nil, err
	}

	return r, nil
}

// executeHTMLTemplate executes a single HTML template with the provided data.
func (r *Renderer) executeHTMLTemplate(w io.Writer, name string, data any) error {
	r.templatesLock.RLock()
	defer r.templatesLock.RUnlock()

	if r.templates == nil {
		return fmt.Errorf("no html templates are defined")
	}

	return r.templates.ExecuteTemplate(w, name, data)
}

// executeTextTemplate executes a single text template with the provided data.
func (r *Renderer) executeTextTemplate(w io.Writer, name string, data any) error {
	r.templatesLock.RLock()
	defer r.templatesLock.RUnlock()

	if r.textTemplates == nil {
		return fmt.Errorf("no text templates are defined")
	}

	return r.textTemplates.ExecuteTemplate(w, name, data)
}

// loadTemplates loads or reloads all templates.
func (r *Renderer) loadTemplates() error {
	r.templatesLock.Lock()
	defer r.templatesLock.Unlock()

	if r.fs == nil {
		return nil
	}

	htmltmpl := htmltemplate.New("").
		Option("missingkey=zero").
		Funcs(r.templateFuncs())

	texttmpl := texttemplate.New("").
		Funcs(r.textFuncs())

	if err := loadTemplates(r.fs, htmltmpl, texttmpl); err != nil {
		return fmt.Errorf("failed to load templates: %w", err)
	}

	r.templates = htmltmpl
	r.textTemplates = texttmpl
	return nil
}

func loadTemplates(fsys fs.FS, htmltmpl *htmltemplate.Template, texttmpl *texttemplate.Template) error {
	// glob does not support shopt-style globbing, so walk the whole filepath.
	return fs.WalkDir(fsys, ".", func(pth string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if strings.HasSuffix(info.Name(), ".html") {
			if _, err := htmltmpl.ParseFS(fsys, pth); err != nil {
				return fmt.Errorf("failed to parse %s: %w", pth, err)
			}
		}

		if strings.HasSuffix(info.Name(), ".txt") {
			if _, err := texttmpl.ParseFS(fsys, pth); err != nil {
				return fmt.Errorf("failed to parse %s: %w", pth, err)
			}
		}

		return nil
	})
}

// AllowedResponseCode reports whether code is a permitted response code.
func (r *Renderer) AllowedResponseCode(code int) bool {
	_, ok := allowedResponseCodes[code]
	return ok
}
