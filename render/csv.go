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
	"fmt"
	"net/http"
)

// CSVMarshaler is implemented by values that can render themselves as CSV. It is
// the input to [Renderer.RenderCSV].
type CSVMarshaler interface {
	MarshalCSV() ([]byte, error)
}

// RenderCSV renders data as a downloadable CSV file with the given status code
// and filename. It buffers the marshaled bytes before writing, so a marshaling
// error does not produce a partial response. If filename is empty, "data.csv" is
// used. On marshaling failure a 500 is returned (with the error detail in dev
// mode).
func (r *Renderer) RenderCSV(w http.ResponseWriter, code int, filename string, data CSVMarshaler) {
	if !r.AllowedResponseCode(code) {
		r.logger.Error("unregistered response code", "code", code)
		w.Header().Set("Content-Type", "text/csv")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "%d is not a registered response code", code)
		return
	}

	// Avoid marshaling nil data.
	if data == nil {
		w.Header().Set("Content-Type", "text/csv")
		w.WriteHeader(code)
		return
	}

	b, err := data.MarshalCSV()
	if err != nil {
		r.logger.Error("failed to marshal csv", "error", err)

		msg := "An internal error occurred."
		if r.debug {
			msg = err.Error()
		}

		w.Header().Set("Content-Type", "text/csv")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "%s", msg)
		return
	}

	if filename == "" {
		filename = "data.csv"
	}

	// Rendering worked; flush to the response as a download.
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment;filename=%s", filename))
	w.WriteHeader(code)
	if _, err := w.Write(b); err != nil {
		// The header and content type are already committed, so the best we can do
		// is log the error.
		r.logger.Error("failed to write csv to response", "error", err)
	}
}
