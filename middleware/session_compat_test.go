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
	"encoding/gob"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	sess "github.com/mikehelmick/go-bananas/session"
	"github.com/mikehelmick/go-bananas/webctx"
)

// capturingStore issues new sessions and records the gob encoding of each
// session's values on Save, the encoding cookie stores persist.
type capturingStore struct {
	saved map[string][]byte
}

func (s *capturingStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	return s.New(r, name)
}

func (s *capturingStore) New(_ *http.Request, name string) (*sessions.Session, error) {
	session := sessions.NewSession(s, name)
	session.Options = &sessions.Options{Path: "/"}
	session.IsNew = true
	return session, nil
}

func (s *capturingStore) Save(_ *http.Request, _ http.ResponseWriter, session *sessions.Session) error {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(session.Values); err != nil {
		return fmt.Errorf("gob encode: %w", err)
	}
	s.saved[session.Name()] = buf.Bytes()
	return nil
}

// v10Decoder is a program whose gob registry holds only the session-value
// types go-bananas v1.0.x registered. It decodes a session value map from
// stdin and prints one sorted "key=type:value" line per entry.
const v10Decoder = `package main

import (
	"encoding/gob"
	"fmt"
	"os"
	"sort"
)

type sessionKey string
type flashKey string

func main() {
	gob.RegisterName("github.com/mikehelmick/go-bananas/session.sessionKey", sessionKey(""))
	gob.RegisterName("github.com/mikehelmick/go-bananas/flash.flashKey", flashKey(""))
	gob.Register(map[string]struct{}{})

	var values map[any]any
	if err := gob.NewDecoder(os.Stdin).Decode(&values); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var lines []string
	for k, v := range values {
		lines = append(lines, fmt.Sprintf("%v=%T:%v", k, v, v))
	}
	sort.Strings(lines)
	for _, l := range lines {
		fmt.Println(l)
	}
}
`

// TestSessionReadableByV10 encodes a session that binds a split cookie
// and decodes the main and split cookies with a gob registry holding only
// the v1.0.1 types. A v1.0.x instance must be able to read both during a
// rollback; an unregistered key type would fail the decode and drop the
// whole session.
func TestSessionReadableByV10(t *testing.T) {
	t.Parallel()

	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("go toolchain not found: %v", err)
	}

	store := &capturingStore{saved: make(map[string][]byte)}
	r := mux.NewRouter()
	r.Use(RequireSession(store, []any{"user"}, testRenderer(t)))
	var values map[any]any
	r.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		s := webctx.SessionFromContext(r.Context())
		s.Values = map[any]any{"user": "alice", "theme": "dark"}
		sess.StoreRegion(s, "us")
		values = s.Values
		w.WriteHeader(http.StatusOK)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/login", nil))

	// The binding ID is stored under the documented reserved plain string key,
	// which gob encodes without registration. The literal pins that contract.
	const bindingKey = "_gbs_binding"
	id, ok := values[bindingKey].(string)
	if !ok || id == "" {
		t.Fatalf("main session has no split binding under %q: %v", bindingKey, values)
	}

	decoder := buildV10Decoder(t, goBin)
	for name, want := range map[string]map[any]any{
		defaultSessionName:      values,
		splitCookieName("user"): {"user": "alice", bindingKey: id},
	} {
		encoded, ok := store.saved[name]
		if !ok {
			t.Fatalf("session %q was not saved", name)
		}
		got := decodeWithV10(t, decoder, encoded)
		if w := formatValues(want); !slices.Equal(got, w) {
			t.Errorf("v1.0.x decoded %q values:\n%s\nwant:\n%s", name, strings.Join(got, "\n"), strings.Join(w, "\n"))
		}
	}
}

// buildV10Decoder compiles v10Decoder and returns the binary's path.
func buildV10Decoder(t *testing.T, goBin string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module v10decoder\n\ngo 1.21\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(v10Decoder), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "v10decoder")
	cmd := exec.CommandContext(t.Context(), goBin, "build", "-o", bin, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=", "GOPROXY=off", "GOTOOLCHAIN=local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build v1.0.x decoder: %v\n%s", err, out)
	}
	return bin
}

// decodeWithV10 runs the v1.0.x decoder on encoded and returns its lines.
func decodeWithV10(t *testing.T, decoder string, encoded []byte) []string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), decoder)
	cmd.Stdin = bytes.NewReader(encoded)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("v1.0.x registry failed to decode the session: %v\n%s", err, stderr.String())
	}
	return strings.Split(strings.TrimSpace(stdout.String()), "\n")
}

// formatValues renders values the way v10Decoder prints them.
func formatValues(values map[any]any) []string {
	lines := make([]string, 0, len(values))
	for k, v := range values {
		lines = append(lines, fmt.Sprintf("%v=%T:%v", k, v, v))
	}
	slices.Sort(lines)
	return lines
}
