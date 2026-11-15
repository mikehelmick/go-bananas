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
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mikehelmick/go-bananas/logging"
	"github.com/mikehelmick/go-bananas/webctx"
)

// endpointFor sends r through mw and returns the serverEndpoint template value.
func endpointFor(t testing.TB, mw func(http.Handler) http.Handler, r *http.Request) string {
	t.Helper()
	var got any
	mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = webctx.TemplateMapFromContext(r.Context())["serverEndpoint"]
	})).ServeHTTP(httptest.NewRecorder(), r)
	s, ok := got.(string)
	if !ok {
		t.Fatalf("serverEndpoint = %#v, want a string", got)
	}
	return s
}

func TestTemplateEndpointAllowedHosts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		devMode  bool
		endpoint string
		allowed  []string
		target   string
		host     string
		want     string
	}{
		{name: "allowlisted_host", allowed: []string{"a.com"}, host: "a.com", want: "https://a.com"},
		{name: "allowlisted_host_case", allowed: []string{"a.com"}, host: "A.com", want: "https://a.com"},
		{name: "allowlisted_host_port", allowed: []string{"a.com:8443"}, host: "a.com:8443", want: "https://a.com:8443"},
		{name: "ftp_absolute_uri", allowed: []string{"a.com"}, target: "ftp://a.com/x", want: "https://a.com"},
		{name: "localhost_allowlisted_is_https", allowed: []string{"localhost"}, host: "localhost", want: "https://localhost"},
		{name: "bad_entry_skipped", allowed: []string{"https://a.com", "b.com"}, host: "a.com", want: "https://b.com"},
		{name: "only_bad_entries", allowed: []string{"https://a.com", ""}, host: "a.com", want: ""},
		{name: "path_entry_skipped", allowed: []string{"a.com/x", "b.com"}, host: "a.com", want: "https://b.com"},
		{name: "space_entry_skipped", allowed: []string{"a.com evil.com", "b.com"}, host: "a.com", want: "https://b.com"},
		{name: "tab_entry_skipped", allowed: []string{"a.com\t", "b.com"}, host: "a.com", want: "https://b.com"},
		{name: "second_allowlisted_host", allowed: []string{"a.com", "b.com"}, host: "b.com", want: "https://b.com"},
		{name: "unlisted_host_falls_back", allowed: []string{"a.com"}, host: "evil.com", want: "https://a.com"},
		{name: "dev_mode_derives", devMode: true, host: "evil.com", want: "https://evil.com"},
		{name: "dev_mode_localhost", devMode: true, host: "localhost:8080", want: "http://localhost:8080"},
		{name: "configured_wins", endpoint: "https://x/", allowed: []string{"a.com"}, host: "evil.com", want: "https://x"},
		{name: "configured_wins_dev", devMode: true, endpoint: "https://x", host: "evil.com", want: "https://x"},
		{name: "nothing_usable", host: "evil.com", want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mw := PopulateTemplateVariables(TemplateConfig{
				DevMode:        tc.devMode,
				ServerEndpoint: tc.endpoint,
				AllowedHosts:   tc.allowed,
			})
			target := "/"
			if tc.target != "" {
				target = tc.target
			}
			r := httptest.NewRequest(http.MethodGet, target, nil)
			if tc.host != "" {
				r.Host = tc.host
			}
			r = r.WithContext(logging.WithLogger(r.Context(), slog.New(slog.DiscardHandler)))

			if got := endpointFor(t, mw, r); got != tc.want {
				t.Errorf("serverEndpoint = %q, want %q", got, tc.want)
			}
		})
	}
}

// lockedBuffer is a bytes.Buffer safe for concurrent writes.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestTemplateEndpointWarnsOnce(t *testing.T) {
	t.Parallel()

	var buf lockedBuffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	mw := PopulateTemplateVariables(TemplateConfig{})

	for range 2 {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Host = "evil.com"
		r = r.WithContext(logging.WithLogger(r.Context(), logger))

		if got := endpointFor(t, mw, r); got != "" {
			t.Errorf("serverEndpoint = %q, want empty", got)
		}
	}

	if n := strings.Count(buf.String(), "serverEndpoint derived from Host header"); n != 1 {
		t.Errorf("warning logged %d times, want 1; log:\n%s", n, buf.String())
	}
}
