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

package server

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestNewAndAddr(t *testing.T) {
	t.Parallel()

	s, err := New("0") // OS-assigned port
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.Port() == "" || s.Port() == "0" {
		t.Errorf("expected a concrete port, got %q", s.Port())
	}
	if s.Addr() == "" {
		t.Error("expected a non-empty Addr")
	}
}

func TestGracefulShutdown(t *testing.T) {
	t.Parallel()

	s, err := New("0")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- s.ServeHTTPHandler(ctx, handler)
	}()

	// The server should be accepting requests.
	addr := "http://" + s.Addr()
	deadline := time.Now().Add(2 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		resp, err := http.Get(addr)
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			got = string(b)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got != "ok" {
		t.Fatalf("expected the server to respond with ok, got %q", got)
	}

	// Cancelling the context should gracefully stop ServeHTTP and return nil.
	cancel()
	select {
	case err := <-serveErr:
		if err != nil {
			t.Fatalf("ServeHTTPHandler returned error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not shut down in time")
	}
}
