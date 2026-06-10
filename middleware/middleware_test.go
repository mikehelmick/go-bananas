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

package middleware

import (
	"io/fs"
	"net/http"
	"testing"
	"testing/fstest"

	"github.com/gorilla/sessions"
	"github.com/mikehelmick/go-bananas/cookiestore"
	"github.com/mikehelmick/go-bananas/render"
)

// testRenderer builds a renderer with the error templates the response helpers
// reference.
func testRenderer(t *testing.T) *render.Renderer {
	t.Helper()
	fsys := fs.FS(fstest.MapFS{
		"400.html": &fstest.MapFile{Data: []byte(`{{define "400"}}bad request{{end}}`)},
		"401.html": &fstest.MapFile{Data: []byte(`{{define "401"}}unauthorized{{end}}`)},
		"404.html": &fstest.MapFile{Data: []byte(`{{define "404"}}not found{{end}}`)},
		"500.html": &fstest.MapFile{Data: []byte(`{{define "500"}}error{{end}}`)},
	})
	h, err := render.New(fsys, render.WithLogger(nil))
	if err != nil {
		t.Fatalf("render.New: %v", err)
	}
	return h
}

// testStore returns a cookie session store with a fixed key, suitable for tests.
func testStore() sessions.Store {
	entropy := func() ([][]byte, error) {
		return [][]byte{make([]byte, 64)}, nil
	}
	return cookiestore.New(entropy, &sessions.Options{Path: "/", MaxAge: 3600})
}

// okHandler writes 200 OK with the given body.
func okHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})
}
