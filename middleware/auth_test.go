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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mikehelmick/go-bananas/webctx"
)

// authFunc adapts a function to the Authenticator interface.
type authFunc func(*http.Request) (any, error)

func (f authFunc) Authenticate(r *http.Request) (any, error) { return f(r) }

func TestRequireAuthenticated(t *testing.T) {
	t.Parallel()

	h := testRenderer(t)

	type principal struct{ email string }

	cases := []struct {
		name      string
		auth      Authenticator
		wantCode  int
		wantPrinc bool
	}{
		{
			name:      "authenticated",
			auth:      authFunc(func(*http.Request) (any, error) { return &principal{"a@example.com"}, nil }),
			wantCode:  http.StatusOK,
			wantPrinc: true,
		},
		{
			name:     "anonymous",
			auth:     authFunc(func(*http.Request) (any, error) { return nil, nil }),
			wantCode: http.StatusUnauthorized,
		},
		{
			name:     "error",
			auth:     authFunc(func(*http.Request) (any, error) { return nil, errors.New("provider down") }),
			wantCode: http.StatusInternalServerError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var gotPrincipal any
			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				gotPrincipal = webctx.PrincipalFromContext(r.Context())
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Accept", "text/html")
			w := httptest.NewRecorder()

			RequireAuthenticated(tc.auth, h)(next).ServeHTTP(w, req)

			if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", w.Code, tc.wantCode)
			}
			if tc.wantPrinc && gotPrincipal == nil {
				t.Error("expected a principal on the context")
			}
			if !tc.wantPrinc && gotPrincipal != nil {
				t.Error("did not expect a principal on the context")
			}
		})
	}
}
