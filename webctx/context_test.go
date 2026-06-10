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

package webctx

import (
	"context"
	"testing"

	"github.com/gorilla/sessions"
)

func TestSessionRoundTrip(t *testing.T) {
	t.Parallel()

	if got := SessionFromContext(context.Background()); got != nil {
		t.Fatalf("expected nil session when absent, got %v", got)
	}

	want := sessions.NewSession(nil, "test")
	ctx := WithSession(context.Background(), want)
	if got := SessionFromContext(ctx); got != want {
		t.Fatalf("SessionFromContext = %v, want %v", got, want)
	}
}

func TestStringValueRoundTrips(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		with func(context.Context, string) context.Context
		from func(context.Context) string
	}{
		{"nonce", WithNonce, NonceFromContext},
		{"requestID", WithRequestID, RequestIDFromContext},
		{"traceID", WithTraceID, TraceIDFromContext},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.from(context.Background()); got != "" {
				t.Fatalf("expected empty string when absent, got %q", got)
			}
			ctx := tc.with(context.Background(), "value-"+tc.name)
			if got := tc.from(ctx); got != "value-"+tc.name {
				t.Fatalf("%s = %q, want %q", tc.name, got, "value-"+tc.name)
			}
		})
	}
}

func TestRequestAndTraceIDMirrorTemplateMap(t *testing.T) {
	t.Parallel()

	ctx := WithRequestID(context.Background(), "req-1")
	ctx = WithTraceID(ctx, "trace-1")

	m := TemplateMapFromContext(ctx)
	if m["requestID"] != "req-1" {
		t.Errorf("template map requestID = %v, want req-1", m["requestID"])
	}
	if m["traceID"] != "trace-1" {
		t.Errorf("template map traceID = %v, want trace-1", m["traceID"])
	}
}

func TestPrincipalRoundTrip(t *testing.T) {
	t.Parallel()

	if got := PrincipalFromContext(context.Background()); got != nil {
		t.Fatalf("expected nil principal when absent, got %v", got)
	}

	type user struct{ Email string }
	want := &user{Email: "a@example.com"}
	ctx := WithPrincipal(context.Background(), want)
	got, ok := PrincipalFromContext(ctx).(*user)
	if !ok || got != want {
		t.Fatalf("PrincipalFromContext = %v, want %v", got, want)
	}
}

func TestOperatingSystemRoundTrip(t *testing.T) {
	t.Parallel()

	if got := OperatingSystemFromContext(context.Background()); got != OSUnknown {
		t.Fatalf("expected OSUnknown when absent, got %v", got)
	}
	ctx := WithOperatingSystem(context.Background(), OSIOS)
	if got := OperatingSystemFromContext(ctx); got != OSIOS {
		t.Fatalf("OperatingSystemFromContext = %v, want OSIOS", got)
	}
}

func TestOSString(t *testing.T) {
	t.Parallel()

	cases := map[OS]string{
		OSUnknown: "Unknown",
		OSIOS:     "iOS",
		OSAndroid: "Android",
		OS(99):    "Unknown",
	}
	for os, want := range cases {
		if got := os.String(); got != want {
			t.Errorf("OS(%d).String() = %q, want %q", int(os), got, want)
		}
	}
}

func TestTemplateMapTitle(t *testing.T) {
	t.Parallel()

	m := make(TemplateMap)

	m.Title("") // no-op
	if _, ok := m["title"]; ok {
		t.Fatal("empty title should be a no-op")
	}

	m.Title("Users")
	if m["title"] != "Users" {
		t.Fatalf("title = %v, want Users", m["title"])
	}

	// A subsequent title is prepended.
	m.Title("Edit %d", 7)
	if m["title"] != "Edit 7 | Users" {
		t.Fatalf("title = %v, want \"Edit 7 | Users\"", m["title"])
	}
}

func TestTemplateMapFromContextNeverNil(t *testing.T) {
	t.Parallel()

	m := TemplateMapFromContext(context.Background())
	if m == nil {
		t.Fatal("expected a non-nil empty template map")
	}
	m["k"] = "v" // must be writable
}
