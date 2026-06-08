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

package session

import (
	"bytes"
	"testing"
	"time"

	"github.com/gorilla/sessions"
)

func newSession() *sessions.Session {
	s := sessions.NewSession(nil, "test")
	s.Values = make(map[any]any)
	return s
}

func TestCSRFToken(t *testing.T) {
	t.Parallel()

	s := newSession()
	if got := CSRFToken(s); got != nil {
		t.Fatalf("expected nil token when absent, got %v", got)
	}

	token := []byte("a-csrf-token")
	StoreCSRFToken(s, token)
	if got := CSRFToken(s); !bytes.Equal(got, token) {
		t.Fatalf("CSRFToken = %v, want %v", got, token)
	}

	ClearCSRFToken(s)
	if got := CSRFToken(s); got != nil {
		t.Fatalf("expected nil token after clear, got %v", got)
	}

	// A malformed value is cleared and reported as absent.
	s.Values[sessionKeyCSRFToken] = "not-bytes"
	if got := CSRFToken(s); got != nil {
		t.Fatalf("expected nil for malformed token, got %v", got)
	}
	if _, ok := s.Values[sessionKeyCSRFToken]; ok {
		t.Fatal("expected malformed token to be cleared")
	}
}

func TestLastActivity(t *testing.T) {
	t.Parallel()

	s := newSession()
	if got := LastActivity(s); !got.IsZero() {
		t.Fatalf("expected zero time when absent, got %v", got)
	}

	now := time.Now()
	StoreLastActivity(s, now)
	if got := LastActivity(s); got.Unix() != now.Unix() {
		t.Fatalf("LastActivity = %v, want %v", got.Unix(), now.Unix())
	}

	ClearLastActivity(s)
	if got := LastActivity(s); !got.IsZero() {
		t.Fatalf("expected zero time after clear, got %v", got)
	}
}

func TestNonceAndRegion(t *testing.T) {
	t.Parallel()

	s := newSession()

	if got := Nonce(s); got != "" {
		t.Fatalf("expected empty nonce, got %q", got)
	}
	StoreNonce(s, "n-1")
	if got := Nonce(s); got != "n-1" {
		t.Fatalf("Nonce = %q, want n-1", got)
	}
	ClearNonce(s)
	if got := Nonce(s); got != "" {
		t.Fatalf("expected empty nonce after clear, got %q", got)
	}

	if got := Region(s); got != "" {
		t.Fatalf("expected empty region, got %q", got)
	}
	StoreRegion(s, "us")
	if got := Region(s); got != "us" {
		t.Fatalf("Region = %q, want us", got)
	}
	ClearRegion(s)
	if got := Region(s); got != "" {
		t.Fatalf("expected empty region after clear, got %q", got)
	}
}

func TestFlash(t *testing.T) {
	t.Parallel()

	s := newSession()
	f := Flash(s)
	if f == nil {
		t.Fatal("expected a non-nil flash")
	}
	f.Error("boom")
	if got := f.Errors(); len(got) != 1 || got[0] != "boom" {
		t.Fatalf("Errors = %v, want [boom]", got)
	}
}

func TestNilSessionSafety(t *testing.T) {
	t.Parallel()

	// None of these must panic on a nil session.
	StoreCSRFToken(nil, []byte("x"))
	StoreLastActivity(nil, time.Now())
	StoreNonce(nil, "x")
	StoreRegion(nil, "x")
	ClearCSRFToken(nil)
	ClearLastActivity(nil)
	ClearNonce(nil)
	ClearRegion(nil)

	if got := CSRFToken(nil); got != nil {
		t.Errorf("CSRFToken(nil) = %v, want nil", got)
	}
	if got := LastActivity(nil); !got.IsZero() {
		t.Errorf("LastActivity(nil) = %v, want zero", got)
	}
	if got := Nonce(nil); got != "" {
		t.Errorf("Nonce(nil) = %q, want empty", got)
	}
	if f := Flash(nil); f == nil {
		t.Error("Flash(nil) should return a usable flash")
	}
}
