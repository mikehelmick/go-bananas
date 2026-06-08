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

package middleware_test

import (
	"fmt"
	"net/http"
	"testing/fstest"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/mikehelmick/go-bananas/cookiestore"
	"github.com/mikehelmick/go-bananas/logging"
	"github.com/mikehelmick/go-bananas/middleware"
	"github.com/mikehelmick/go-bananas/render"
)

// ExampleRecovery demonstrates assembling the recommended middleware chain on a
// gorilla/mux router. The order matters: Recovery is outermost, request/trace
// IDs and the logger come next, then security and session middleware.
func ExampleRecovery() {
	h, err := render.New(fstest.MapFS{
		"500.html": &fstest.MapFile{Data: []byte(`{{define "500"}}error{{end}}`)},
	})
	if err != nil {
		panic(err)
	}

	store := cookiestore.New(func() ([][]byte, error) {
		return [][]byte{make([]byte, 64)}, nil
	}, &sessions.Options{Path: "/"})

	r := mux.NewRouter()
	r.Use(middleware.Recovery(h))
	r.Use(middleware.PopulateRequestID(h))
	r.Use(middleware.PopulateTraceID())
	r.Use(middleware.PopulateLogger(logging.DefaultLogger()))
	r.Use(middleware.SecureHeaders(false, middleware.ServerTypeHTML))
	r.Use(middleware.GzipResponse())
	r.Use(middleware.RequireSession(store, nil, h))
	r.Use(middleware.HandleCSRF(h))
	r.Use(middleware.InjectCurrentPath())

	r.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "hello")
	})

	fmt.Println("router configured")
	// Output: router configured
}
