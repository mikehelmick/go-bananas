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

// Package logging provides a context-scoped logger built on the standard
// library's [log/slog].
//
// A logger is carried on a [context.Context] with [WithLogger] and retrieved
// with [FromContext]. When no logger is present on the context, [FromContext]
// returns the process [DefaultLogger] so callers never have to nil-check.
//
// Middleware in this framework stores a request logger on the context, so
// handlers can simply call logging.FromContext(r.Context()).
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/sethvargo/go-envconfig"
)

// contextKey is a private type to prevent collisions in the context map.
type contextKey string

// loggerKey points to the value in the context where the logger is stored.
const loggerKey = contextKey("logger")

var (
	// defaultLogger is the process-wide default logger, initialized once on first
	// use by DefaultLogger.
	defaultLogger     *slog.Logger
	defaultLoggerOnce sync.Once
)

// NewLogger creates a new logger at the given level. When development is true
// the logger writes human-readable text to standard error; otherwise it writes
// structured JSON to standard error.
func NewLogger(level slog.Level, development bool) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if development {
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}

	return slog.New(handler)
}

// Config controls logger construction from the environment. The struct tags are
// compatible with [github.com/sethvargo/go-envconfig].
type Config struct {
	// Level is the minimum log level ("debug", "info", "warn"/"warning", or
	// "error"). Unrecognized values fall back to info (see [LevelFromString]).
	Level string `env:"LOG_LEVEL, default=info"`

	// Mode selects the output format. "development" produces human-readable text
	// output; any other value (including the "production" default) produces
	// structured JSON.
	Mode string `env:"LOG_MODE, default=production"`
}

// NewLoggerFromConfig builds a *slog.Logger from c. The Level is resolved via
// [LevelFromString] and a Mode of "development" (case-insensitive, trimmed)
// selects the text handler; any other Mode selects the JSON handler.
func NewLoggerFromConfig(c Config) *slog.Logger {
	level := LevelFromString(c.Level)
	development := strings.EqualFold(strings.TrimSpace(c.Mode), "development")
	return NewLogger(level, development)
}

// NewLoggerFromEnv creates a new logger from the environment. It reads LOG_LEVEL
// (debug, info, warn, or error; defaulting to info) to determine the level and
// LOG_MODE (development for text output; defaulting to production/JSON) to
// determine the output format. Environment parsing is delegated to [Config] and
// [NewLoggerFromConfig].
func NewLoggerFromEnv() *slog.Logger {
	// The Config must start as the zero value: go-envconfig only populates a
	// field (including applying its "default=" tag) when the field is currently
	// the zero value, so pre-seeding would suppress reading the environment.
	//
	// On success, the "default=" tags yield Level="info"/Mode="production". On a
	// Process error (rare, since all fields have defaults) we proceed gracefully
	// with whatever was populated: an empty Level resolves to info via
	// LevelFromString and an empty Mode yields JSON output, which exactly
	// reproduces the historical unset-environment defaults.
	var c Config
	_ = envconfig.Process(context.Background(), &c)
	return NewLoggerFromConfig(c)
}

// DefaultLogger returns the process-wide default logger, constructing it from
// the environment on first use via [NewLoggerFromEnv].
func DefaultLogger() *slog.Logger {
	defaultLoggerOnce.Do(func() {
		defaultLogger = NewLoggerFromEnv()
	})
	return defaultLogger
}

// WithLogger returns a copy of ctx that carries the provided logger. Retrieve it
// with [FromContext].
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// FromContext returns the logger stored on ctx by [WithLogger]. If no logger is
// present, it returns [DefaultLogger].
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return logger
	}
	return DefaultLogger()
}

// Named returns a logger that tags every record with a "logger" attribute set to
// name. It is the slog equivalent of a named/sub logger and is typically used to
// identify the component emitting a log line, e.g. Named(l, "middleware.CSRF").
func Named(l *slog.Logger, name string) *slog.Logger {
	return l.With("logger", name)
}

// LevelFromString converts a case-insensitive level string ("debug", "info",
// "warn"/"warning", "error") to an [slog.Level]. Unrecognized or empty values
// map to [slog.LevelInfo].
func LevelFromString(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// TestLogger returns a logger that writes debug-level text output through the
// provided [testing.TB], so log lines are attributed to the running test and
// hidden unless the test fails or runs with -v.
func TestLogger(tb testing.TB) *slog.Logger {
	tb.Helper()
	handler := slog.NewTextHandler(testWriter{tb}, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(handler)
}

// testWriter adapts a testing.TB to io.Writer, routing each write to tb.Log.
type testWriter struct {
	tb testing.TB
}

var _ io.Writer = testWriter{}

func (w testWriter) Write(p []byte) (int, error) {
	w.tb.Helper()
	w.tb.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}
