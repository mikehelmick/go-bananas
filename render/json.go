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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// RenderJSON renders data as JSON with the given status code, buffering first so
// a marshaling error never produces a partial response.
//
// If data is nil and code is a 2xx, the body is `{"ok":true}`; for a non-2xx
// code with nil data, the body is `{"error":"<status text>"}`. An error value
// (or a slice of errors, or an error that unwraps to several via
// [errors.Join]) is rendered as `{"error":...}` or `{"errors":[...]}`.
//
// If marshaling fails, a generic 500 JSON response is returned; in dev mode the
// error detail is included. The code must be in the renderer's allowed set.
func (r *Renderer) RenderJSON(w http.ResponseWriter, code int, data any) {
	if !r.AllowedResponseCode(code) {
		r.logger.Error("unregistered response code", "code", code)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		msg := escapeJSON(fmt.Sprintf("%d is not a registered response code", code))
		fmt.Fprintf(w, jsonErrTmpl, msg)
		return
	}

	// Avoid marshaling nil data.
	if data == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)

		// Return an OK response.
		if code >= http.StatusOK && code < http.StatusMultipleChoices {
			fmt.Fprint(w, jsonOKResp)
			return
		}

		// Return an error with the generic HTTP text as the error.
		msg := escapeJSON(http.StatusText(code))
		fmt.Fprintf(w, jsonErrTmpl, msg)
		return
	}

	// Special-case joined/multi-errors (e.g. errors.Join).
	if typ, ok := data.(interface{ Unwrap() []error }); ok {
		data = typ.Unwrap()
	}
	if typ, ok := data.([]error); ok {
		msgs := make([]string, 0, len(typ))
		for _, err := range typ {
			msgs = append(msgs, err.Error())
		}
		data = &multiError{Errors: msgs}
	}

	// If the provided value was a single error, marshal it accordingly.
	if typ, ok := data.(error); ok {
		data = &singleError{Error: typ.Error()}
	}

	// Acquire a buffer.
	b := r.rendererPool.Get().(*bytes.Buffer)
	b.Reset()
	defer r.rendererPool.Put(b)

	// Render into the buffer.
	if err := json.NewEncoder(b).Encode(data); err != nil {
		r.logger.Error("failed to marshal json", "error", err)

		msg := "An internal error occurred."
		if r.debug {
			msg = err.Error()
		}
		msg = escapeJSON(msg)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, jsonErrTmpl, msg)
		return
	}

	// Rendering worked; flush to the response.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if _, err := b.WriteTo(w); err != nil {
		// The header and content type are already committed, so the best we can do
		// is log the error.
		r.logger.Error("failed to write json to response", "error", err)
	}
}

// RenderJSON500 renders err as a JSON 500 response. In production it shows a
// generic message; in dev mode it shows the actual error.
func (r *Renderer) RenderJSON500(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError

	if r.debug {
		r.RenderJSON(w, code, map[string]string{
			"error": err.Error(),
		})
		return
	}

	r.RenderJSON(w, code, map[string]string{
		"error": http.StatusText(code),
	})
}

// escapeJSON does primitive JSON string escaping for use with the Printf-based
// error template.
func escapeJSON(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}

// jsonErrTmpl is the template used to return a JSON error. It is rendered with
// Printf, not json.Encode, so values must be escaped by the caller.
const jsonErrTmpl = `{"error":"%s"}`

// jsonOKResp is the return value for empty data responses.
const jsonOKResp = `{"ok":true}`

type singleError struct {
	Error string `json:"error,omitempty"`
}

type multiError struct {
	Errors []string `json:"errors,omitempty"`
}
