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

package server_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/mikehelmick/go-bananas/server"
)

func ExampleReadyzHandler() {
	// Register named readiness checks; details of failures are logged
	// server-side while the public body lists only the failing names.
	h := server.ReadyzHandler(map[string]func(context.Context) error{
		"database": func(_ context.Context) error {
			return errors.New("dial tcp: connection refused") // logged, not returned
		},
		"cache": func(_ context.Context) error { return nil },
	})

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	fmt.Println(w.Code)
	fmt.Print(w.Body.String())
	// Output:
	// 503
	// {"failed":["database"],"status":"unavailable"}
}
