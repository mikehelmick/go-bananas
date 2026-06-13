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

package form_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"

	"github.com/mikehelmick/go-bananas/form"
)

// Signup is a destination struct with form and validate tags.
type Signup struct {
	Name  string `form:"name" validate:"required"`
	Email string `form:"email" validate:"required,email"`
	Age   int    `form:"age" validate:"gte=18"`
}

// ExampleBind demonstrates binding a form submission, then printing the
// per-field validation errors in a deterministic order.
func ExampleBind() {
	// Build a request with a missing name, a malformed email, and an
	// under-age value.
	values := url.Values{}
	values.Set("email", "nope")
	values.Set("age", "12")
	r := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var dst Signup
	errs, err := form.Bind(r, &dst)
	if err != nil {
		// An unprocessable request (oversized body, malformed content type).
		fmt.Println("bad request:", err)
		return
	}

	if !errs.Any() {
		fmt.Println("ok")
		return
	}

	// Sort field names for stable output.
	fields := make([]string, 0, len(errs))
	for field := range errs {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		for _, msg := range errs.Get(field) {
			fmt.Printf("%s: %s\n", field, msg)
		}
	}

	// Output:
	// age: is too small
	// email: must be a valid email address
	// name: is required
}
