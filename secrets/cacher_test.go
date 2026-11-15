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

package secrets

import (
	"bytes"
	"context"
	"runtime"
	"sync"
	"testing"
	"time"
)

// gatedSM records the ctx of every GetSecretValue call and blocks each call
// until release is closed, then returns "s3cret".
type gatedSM struct {
	mu      sync.Mutex
	ctxs    []context.Context
	started chan struct{}
	release chan struct{}
}

func (g *gatedSM) GetSecretValue(ctx context.Context, _ string) (string, error) {
	g.mu.Lock()
	g.ctxs = append(g.ctxs, ctx)
	first := len(g.ctxs) == 1
	g.mu.Unlock()
	if first {
		close(g.started)
	}
	<-g.release
	return "s3cret", nil
}

func (g *gatedSM) calls() []context.Context {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]context.Context(nil), g.ctxs...)
}

// waitForParkedCacherCall blocks until some goroutine is parked inside
// Cacher.GetSecretValue waiting on another caller's in-flight fetch (blocked in
// sync.WaitGroup.Wait under cache.WriteThruLookup). The cache's waiter count is
// unexported, so this inspects goroutine stacks instead. It matches on the
// WaitGroup frame, so it must change if cache's in-flight call stops using a
// sync.WaitGroup.
func waitForParkedCacherCall(t *testing.T) {
	t.Helper()

	var (
		sep         = []byte("\n\n")
		waitFrame   = []byte("sync.(*WaitGroup).Wait")
		lookupFrame = []byte("cache.(*Cache[...]).WriteThruLookup")
		cacherFrame = []byte("secrets.(*Cacher).GetSecretValue")
	)
	buf := make([]byte, 1<<20)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		n := runtime.Stack(buf, true)
		for _, g := range bytes.Split(buf[:n], sep) {
			if bytes.Contains(g, waitFrame) && bytes.Contains(g, lookupFrame) && bytes.Contains(g, cacherFrame) {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for a caller to park on the in-flight fetch")
}

func TestFetchContextDeadline(t *testing.T) {
	t.Parallel()

	later := time.Now().Add(time.Hour)
	wantFetchTimeout := func() (time.Time, time.Time) {
		now := time.Now()
		return now.Add(cacherFetchTimeout - time.Second), now.Add(cacherFetchTimeout + 5*time.Second)
	}
	cases := []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		want func() (time.Time, time.Time)
	}{
		{
			name: "no deadline",
			ctx:  func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
			want: wantFetchTimeout,
		},
		{
			name: "earlier deadline",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), time.Second)
			},
			want: wantFetchTimeout,
		},
		{
			name: "later deadline",
			ctx:  func() (context.Context, context.CancelFunc) { return context.WithDeadline(context.Background(), later) },
			want: func() (time.Time, time.Time) { return later, later },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			wantMin, wantMax := tc.want()
			ctx, cancelCaller := tc.ctx()
			fctx, cancel := fetchContext(ctx)
			defer cancel()
			cancelCaller()

			if err := fctx.Err(); err != nil {
				t.Errorf("fetch ctx err = %v after cancelling the caller, want nil", err)
			}
			d, ok := fctx.Deadline()
			if !ok || d.Before(wantMin) || d.After(wantMax) {
				t.Errorf("fetch deadline = %v (set %v), want in [%v, %v]", d, ok, wantMin, wantMax)
			}
		})
	}
}

func TestCacherCancelledCallerDoesNotFailOthers(t *testing.T) {
	t.Parallel()

	underlying := &gatedSM{started: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(underlying.release) }) }
	t.Cleanup(release)

	sm, err := WrapCacher(context.Background(), underlying, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	type result struct {
		val string
		err error
	}

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	resA := make(chan result, 1)
	go func() {
		v, err := sm.GetSecretValue(ctxA, "k")
		resA <- result{v, err}
	}()
	<-underlying.started

	resB := make(chan result, 1)
	go func() {
		v, err := sm.GetSecretValue(context.Background(), "k")
		resB <- result{v, err}
	}()
	waitForParkedCacherCall(t)

	cancelA()
	calls := underlying.calls()
	if len(calls) != 1 {
		t.Fatalf("underlying called %d times, want 1", len(calls))
	}
	if err := calls[0].Err(); err != nil {
		t.Errorf("fetch ctx err = %v after cancelling caller A, want nil (detached)", err)
	}
	if _, ok := calls[0].Deadline(); !ok {
		t.Error("fetch ctx has no deadline, want one bounded by cacherFetchTimeout")
	}
	release()

	for name, ch := range map[string]chan result{"A (cancelled)": resA, "B (live)": resB} {
		select {
		case r := <-ch:
			if r.err != nil || r.val != "s3cret" {
				t.Errorf("caller %s: got (%q, %v), want (%q, nil)", name, r.val, r.err, "s3cret")
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("caller %s did not return", name)
		}
	}
	if got := len(underlying.calls()); got != 1 {
		t.Errorf("underlying called %d times in total, want 1", got)
	}
}
