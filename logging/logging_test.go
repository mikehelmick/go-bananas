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
	"os"
	"testing"
)

// handlerKind classifies the slog handler backing a logger so tests can assert
// on the selected output format without coupling to stderr.
func handlerKind(l *slog.Logger) string {
	switch l.Handler().(type) {
	case *slog.TextHandler:
		return "text"
	case *slog.JSONHandler:
		return "json"
	default:
		return "other"
	}
}

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

func TestNewLoggerFromConfig(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		cfg      Config
		wantKind string
		debugOn  bool
		infoOn   bool
	}{
		{
			name:     "defaults_info_json",
			cfg:      Config{Level: "info", Mode: "production"},
			wantKind: "json",
			debugOn:  false,
			infoOn:   true,
		},
		{
			name:     "debug_enables_debug",
			cfg:      Config{Level: "debug", Mode: "production"},
			wantKind: "json",
			debugOn:  true,
			infoOn:   true,
		},
		{
			name:     "development_selects_text",
			cfg:      Config{Level: "info", Mode: "development"},
			wantKind: "text",
			debugOn:  false,
			infoOn:   true,
		},
		{
			name:     "zero_value_matches_defaults",
			cfg:      Config{},
			wantKind: "json",
			debugOn:  false,
			infoOn:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			l := NewLoggerFromConfig(tc.cfg)
			if got := handlerKind(l); got != tc.wantKind {
				t.Errorf("handler kind = %q, want %q", got, tc.wantKind)
			}
			if got := l.Enabled(context.Background(), slog.LevelDebug); got != tc.debugOn {
				t.Errorf("debug enabled = %v, want %v", got, tc.debugOn)
			}
			if got := l.Enabled(context.Background(), slog.LevelInfo); got != tc.infoOn {
				t.Errorf("info enabled = %v, want %v", got, tc.infoOn)
			}
		})
	}
}

func TestNewLoggerFromEnv_ReadsEnv(t *testing.T) {
	// No t.Parallel: this test mutates the environment via t.Setenv.
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_MODE", "development")

	l := NewLoggerFromEnv()

	if got := handlerKind(l); got != "text" {
		t.Errorf("handler kind = %q, want %q (LOG_MODE=development)", got, "text")
	}
	if !l.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected debug to be enabled with LOG_LEVEL=debug")
	}
}

func TestNewLoggerFromEnv_Defaults(t *testing.T) {
	// No t.Parallel: this test mutates the environment via t.Setenv.
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("LOG_MODE", "")

	l := NewLoggerFromEnv()

	// Empty/unset env must reproduce historical defaults: info level, JSON.
	if got := handlerKind(l); got != "json" {
		t.Errorf("handler kind = %q, want %q (default)", got, "json")
	}
	if l.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected debug to be disabled by default")
	}
	if !l.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("expected info to be enabled by default")
	}
}

func TestNewLoggerFromEnv_Unset(t *testing.T) {
	// No t.Parallel: this test mutates the environment. t.Setenv registers
	// restoration of the pre-test value; we then Unsetenv so the body runs with
	// the vars genuinely absent (not merely empty), guarding the historical
	// "unset => info + JSON" behavior that the refactor to go-envconfig must
	// preserve. t.Setenv cannot unset, hence the explicit Unsetenv.
	t.Setenv("LOG_LEVEL", "placeholder")
	t.Setenv("LOG_MODE", "placeholder")
	if err := os.Unsetenv("LOG_LEVEL"); err != nil {
		t.Fatalf("unset LOG_LEVEL: %v", err)
	}
	if err := os.Unsetenv("LOG_MODE"); err != nil {
		t.Fatalf("unset LOG_MODE: %v", err)
	}

	l := NewLoggerFromEnv()

	if got := handlerKind(l); got != "json" {
		t.Errorf("handler kind = %q, want %q (vars unset)", got, "json")
	}
	if l.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected debug to be disabled when vars are unset")
	}
	if !l.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("expected info to be enabled when vars are unset")
	}
}
