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
	"fmt"
	"html"
	"net/http"
)

// RenderHTML renders the named HTML template with a 200 OK status. It is
// shorthand for [Renderer.RenderHTMLStatus] with [http.StatusOK].
func (r *Renderer) RenderHTML(w http.ResponseWriter, tmpl string, data any) {
	r.RenderHTMLStatus(w, http.StatusOK, tmpl, data)
}

// RenderHTMLStatus renders the named HTML template with the given status code.
// It renders into a pooled buffer first and only flushes to w on success, so a
// template error never produces a partial response.
//
// If template execution fails, a generic 500 page is returned; in dev mode the
// error detail is included. If flushing the buffer fails, the error is logged
// but no recovery is attempted. The code must be in the renderer's allowed set
// (see [Renderer.AllowedResponseCode]).
func (r *Renderer) RenderHTMLStatus(w http.ResponseWriter, code int, tmpl string, data any) {
	if !r.AllowedResponseCode(code) {
		r.logger.Error("unregistered response code", "code", code)

		w.WriteHeader(http.StatusInternalServerError)
		msg := fmt.Sprintf("%d is not a registered response code", code)
		fmt.Fprintf(w, htmlErrTmpl, msg)
		return
	}

	if r.debug {
		if err := r.loadTemplates(); err != nil {
			r.logger.Error("failed to reload templates in renderer", "error", err)

			msg := html.EscapeString(err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, htmlErrTmpl, msg)
			return
		}
	}

	// Acquire a buffer.
	b := r.rendererPool.Get().(*bytes.Buffer)
	b.Reset()
	defer r.rendererPool.Put(b)

	// Render into the buffer.
	if err := r.executeHTMLTemplate(b, tmpl, data); err != nil {
		r.logger.Error("failed to execute html template", "error", err)

		msg := "An internal error occurred."
		if r.debug {
			msg = err.Error()
		}
		msg = html.EscapeString(msg)

		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, htmlErrTmpl, msg)
		return
	}

	// Rendering worked; flush to the response.
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.WriteHeader(code)
	if _, err := b.WriteTo(w); err != nil {
		// The header and content type are already committed, so the best we can do
		// is log the error.
		r.logger.Error("failed to write html to response", "error", err)
	}
}

// RenderHTML500 renders err using the "500" template with a 500 status. In
// production it shows a generic message; in dev mode it shows the actual error.
func (r *Renderer) RenderHTML500(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError

	if r.debug {
		r.RenderHTMLStatus(w, code, "500", map[string]string{
			"title": http.StatusText(http.StatusInternalServerError),
			"error": err.Error(),
		})
		return
	}

	r.RenderHTMLStatus(w, code, "500", map[string]string{
		"title": http.StatusText(http.StatusInternalServerError),
		"error": http.StatusText(code),
	})
}

// htmlErrTmpl is the template used to return an HTML error. It is rendered with
// Printf, not html/template, so values must be escaped by the caller.
const htmlErrTmpl = `
<html>
  <head>
    <title>Internal server error</title>
  </head>

  <body>
    <h1>Internal server error</h1>
    <p style="font-family:monospace">%s</p>
  </body>
</html>
`
