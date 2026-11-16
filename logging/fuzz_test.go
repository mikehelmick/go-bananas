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
	"log/slog"
	"testing"
)

// FuzzLevelFromString checks that any input maps to one of the four supported
// levels.
func FuzzLevelFromString(f *testing.F) {
	for _, s := range []string{"", "debug", "INFO", " Warn ", "warning", "error", "fatal", "-4", "\x00", "\xff", "DEBUG\n", "İnfo"} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		switch got := LevelFromString(s); got {
		case slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError:
		default:
			t.Fatalf("LevelFromString(%q) = %v, want Debug, Info, Warn or Error", s, got)
		}
	})
}
