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

package logging

import (
	"context"
	"log/slog"
	"testing"
)

func TestFromContext_DefaultWhenAbsent(t *testing.T) {
	t.Parallel()

	got := FromContext(context.Background())
	if got == nil {
		t.Fatal("expected a non-nil default logger")
	}
	if got != DefaultLogger() {
		t.Fatal("expected FromContext to return the default logger when none is set")
	}
}

func TestWithLogger_RoundTrip(t *testing.T) {
	t.Parallel()

	want := TestLogger(t)
	ctx := WithLogger(context.Background(), want)

	if got := FromContext(ctx); got != want {
		t.Fatalf("FromContext did not return the stored logger")
	}
}

func TestLevelFromString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{" info ", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},
		{"bogus", slog.LevelInfo},
	}

	for _, tc := range cases {
		if got := LevelFromString(tc.in); got != tc.want {
			t.Errorf("LevelFromString(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestNamed(t *testing.T) {
	t.Parallel()

	// Named must not panic and must return a usable logger.
	l := Named(TestLogger(t), "component.sub")
	if l == nil {
		t.Fatal("expected a non-nil named logger")
	}
	l.Info("hello")
}
