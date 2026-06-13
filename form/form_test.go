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

package form

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
)

// signup is a representative destination struct used across the table tests.
type signup struct {
	Name  string `form:"name" validate:"required"`
	Email string `form:"email" validate:"required,email"`
	Age   int    `form:"age" validate:"gte=18"`
}

// withValidator embeds bespoke cross-field validation.
type withValidator struct {
	Password string `form:"password"`
	Confirm  string `form:"confirm"`
}

func (w *withValidator) Validate() Errors {
	e := make(Errors)
	if w.Password != w.Confirm {
		e.Add("confirm", "must match password")
	}
	return e
}

// withValueValidator implements Validator on the value receiver, exercising the
// pointer/value method-set detection in asValidator.
type withValueValidator struct {
	Field string `form:"field"`
}

func (w withValueValidator) Validate() Errors {
	e := make(Errors)
	if w.Field == "" {
		e.Add("field", "value validator fired")
	}
	return e
}

// newFormRequest builds a urlencoded POST request from values.
func newFormRequest(values url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func TestBind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// build returns the request and a fresh dst pointer.
		build        func() (*http.Request, any)
		opts         []Option
		wantErr      bool
		wantAny      bool
		wantFields   map[string][]string
		wantContains map[string]string
		check        func(t *testing.T, dst any)
	}{
		{
			name: "valid",
			build: func() (*http.Request, any) {
				v := url.Values{}
				v.Set("name", "Ada")
				v.Set("email", "ada@example.com")
				v.Set("age", "42")
				return newFormRequest(v), &signup{}
			},
			wantErr: false,
			wantAny: false,
			check: func(t *testing.T, dst any) {
				t.Helper()
				s := dst.(*signup)
				if s.Name != "Ada" || s.Email != "ada@example.com" || s.Age != 42 {
					t.Fatalf("struct not populated: %+v", s)
				}
			},
		},
		{
			name: "type mismatch",
			build: func() (*http.Request, any) {
				v := url.Values{}
				v.Set("name", "Ada")
				v.Set("email", "ada@example.com")
				v.Set("age", "abc")
				return newFormRequest(v), &signup{}
			},
			wantErr: false,
			wantAny: true,
			// A non-numeric "age" produces a conversion error; because the
			// field stays at its zero value the gte rule also trips, so we
			// assert containment rather than an exact match.
			wantContains: map[string]string{"age": "must be a number"},
		},
		{
			name: "tag violations use form names",
			build: func() (*http.Request, any) {
				v := url.Values{}
				v.Set("email", "not-an-email")
				v.Set("age", "10")
				return newFormRequest(v), &signup{}
			},
			wantErr: false,
			wantAny: true,
			wantFields: map[string][]string{
				"name":  {"is required"},
				"email": {"must be a valid email address"},
				"age":   {"is too small"},
			},
		},
		{
			name: "pointer validator merged",
			build: func() (*http.Request, any) {
				v := url.Values{}
				v.Set("password", "secret")
				v.Set("confirm", "different")
				return newFormRequest(v), &withValidator{}
			},
			wantErr:    false,
			wantAny:    true,
			wantFields: map[string][]string{"confirm": {"must match password"}},
		},
		{
			name: "value validator merged",
			build: func() (*http.Request, any) {
				v := url.Values{}
				return newFormRequest(v), &withValueValidator{}
			},
			wantErr:    false,
			wantAny:    true,
			wantFields: map[string][]string{"field": {"value validator fired"}},
		},
		{
			name: "messages override",
			build: func() (*http.Request, any) {
				v := url.Values{}
				v.Set("email", "ada@example.com")
				v.Set("age", "20")
				return newFormRequest(v), &signup{}
			},
			opts:       []Option{WithMessages(map[string]string{"required": "cannot be blank"})},
			wantErr:    false,
			wantAny:    true,
			wantFields: map[string][]string{"name": {"cannot be blank"}},
		},
		{
			name: "body too large is fatal",
			build: func() (*http.Request, any) {
				v := url.Values{}
				v.Set("name", strings.Repeat("x", 2048))
				v.Set("email", "ada@example.com")
				v.Set("age", "20")
				return newFormRequest(v), &signup{}
			},
			opts:    []Option{WithMaxBodyBytes(16)},
			wantErr: true,
		},
		{
			name: "bad content type is fatal",
			build: func() (*http.Request, any) {
				// A multipart content type with a non-multipart body fails to
				// parse, producing the unprocessable-request path.
				r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not multipart"))
				r.Header.Set("Content-Type", "multipart/form-data; boundary=xyz")
				return r, &signup{}
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r, dst := tc.build()
			errs, err := Bind(r, dst, tc.opts...)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected fatal error, got nil (errs=%v)", errs)
				}
				if errs != nil {
					t.Fatalf("expected nil Errors on fatal path, got %v", errs)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected fatal error: %v", err)
			}
			if got := errs.Any(); got != tc.wantAny {
				t.Fatalf("Any() = %v, want %v (errs=%v)", got, tc.wantAny, errs)
			}
			for field, want := range tc.wantFields {
				got := errs.Get(field)
				if len(got) != len(want) {
					t.Fatalf("field %q = %v, want %v", field, got, want)
				}
				for i := range want {
					if got[i] != want[i] {
						t.Fatalf("field %q[%d] = %q, want %q", field, i, got[i], want[i])
					}
				}
			}
			for field, want := range tc.wantContains {
				got := errs.Get(field)
				if !slices.Contains(got, want) {
					t.Fatalf("field %q = %v, want to contain %q", field, got, want)
				}
			}
			if tc.check != nil {
				tc.check(t, dst)
			}
		})
	}
}

func TestErrors(t *testing.T) {
	t.Parallel()

	t.Run("nil reads are safe", func(t *testing.T) {
		t.Parallel()
		var e Errors
		if e.Any() {
			t.Fatal("nil Errors should report Any() == false")
		}
		if e.Has("x") {
			t.Fatal("nil Errors should report Has() == false")
		}
		if got := e.Get("x"); got != nil {
			t.Fatalf("nil Errors Get() = %v, want nil", got)
		}
	})

	t.Run("add get has any", func(t *testing.T) {
		t.Parallel()
		e := make(Errors)
		e.Add("email", "is required")
		e.Add("email", "must be valid")
		if !e.Has("email") {
			t.Fatal("Has(email) = false")
		}
		if !e.Any() {
			t.Fatal("Any() = false")
		}
		if got := e.Get("email"); len(got) != 2 {
			t.Fatalf("Get(email) = %v, want 2 messages", got)
		}
		if e.Has("name") {
			t.Fatal("Has(name) = true, want false")
		}
	})

	t.Run("merge", func(t *testing.T) {
		t.Parallel()
		a := make(Errors)
		a.Add("x", "one")
		b := make(Errors)
		b.Add("x", "two")
		b.Add("y", "three")
		a.Merge(b)
		if got := a.Get("x"); len(got) != 2 || got[0] != "one" || got[1] != "two" {
			t.Fatalf("Get(x) = %v", got)
		}
		if got := a.Get("y"); len(got) != 1 || got[0] != "three" {
			t.Fatalf("Get(y) = %v", got)
		}
	})
}
