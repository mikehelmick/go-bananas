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
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/mikehelmick/go-bananas/logging"
	"github.com/mikehelmick/go-bananas/render"
	"github.com/mikehelmick/go-bananas/response"
	sess "github.com/mikehelmick/go-bananas/session"
	"github.com/mikehelmick/go-bananas/webctx"
)

const (
	// defaultSessionName is the cookie name used by [RequireSession].
	defaultSessionName = "go-bananas-session"
	splitValuePrefix   = "_gbs-"
)

// RequireNamedSession is like [RequireSession] but uses a specific session
// (cookie) name instead of the default.
func RequireNamedSession(store sessions.Store, name string, splitValues []any, h *render.Renderer) mux.MiddlewareFunc {
	return buildHandler(store, name, splitValues, h)
}

// RequireSession loads or creates a session from store, stores it on the request
// context, and ensures the session's flash data is available to templates. Any
// handler that uses sessions, flash messages, or CSRF must be wrapped with this.
//
// splitValues names session keys whose values are large enough to warrant their
// own cookie (browsers cap individual cookies at ~4KB); each listed key is split
// out into a separate companion cookie on save and rejoined on load.
func RequireSession(store sessions.Store, splitValues []any, h *render.Renderer) mux.MiddlewareFunc {
	return buildHandler(store, defaultSessionName, splitValues, h)
}

func splitCookieName(key any) string {
	return fmt.Sprintf("%s%v", splitValuePrefix, key)
}

// joinSplitSessionCookie retrieves a companion cookie holding a single value that
// was previously split out of the main session cookie, folding its value back
// into session.
func joinSplitSessionCookie(r *http.Request, store sessions.Store, key any, session *sessions.Session, logger *slog.Logger) *sessions.Session {
	name := splitCookieName(key)
	splitSession, err := store.Get(r, name)
	if err != nil {
		logger.Warn("failed to get split session cookie; creating a new one", "name", name, "error", err)
		splitSession, _ = store.New(r, name)
	}
	if splitSession.Values != nil {
		if v, ok := splitSession.Values[key]; ok {
			session.Values[key] = v
		}
	}
	return splitSession
}

// splitSessionCookie moves the value for key out of the main session and into
// splitSession so it can be saved as its own cookie.
func splitSessionCookie(session, splitSession *sessions.Session, key any) {
	splitSession.Values = make(map[any]any)
	if value, ok := session.Values[key]; ok {
		splitSession.Values[key] = value
		delete(session.Values, key)
	}
}

func buildHandler(store sessions.Store, name string, splitValues []any, h *render.Renderer) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			logger := logging.Named(logging.FromContext(ctx), "middleware.RequireSession")

			// Get or create a session from the store.
			session, err := store.Get(r, name)
			if err != nil {
				logger.Error("failed to get session", "error", err)

				// Per the spec, Get can return an error but never a nil session. We
				// discard the error to ensure we always have a session.
				session, _ = store.New(r, name)
			}
			splitSessions := make(map[any]*sessions.Session)
			for _, key := range splitValues {
				splitSessions[key] = joinSplitSessionCookie(r, store, key, session, logger)
			}

			// Save the flash in the template map.
			m := webctx.TemplateMapFromContext(ctx)
			m["flash"] = sess.Flash(session)
			ctx = webctx.WithTemplateMap(ctx, m)

			// Save the session on the context.
			ctx = webctx.WithSession(ctx, session)
			r = r.Clone(ctx)

			// Ensure the session is saved at least once. This is passed to the
			// before-first-byte writer AND called after the middleware executes to
			// ensure the session is always sent. saveErr is declared outside the
			// closure so the result persists across the repeated (once-guarded)
			// calls.
			var (
				once    sync.Once
				saveErr error
			)
			saveSession := func() error {
				once.Do(func() {
					session := webctx.SessionFromContext(ctx)
					if session == nil {
						return
					}
					// Split and save individual cookies.
					for key, splitSession := range splitSessions {
						splitSessionCookie(session, splitSession, key)
						if err := splitSession.Save(r, w); err != nil {
							saveErr = errors.Join(saveErr, fmt.Errorf("save split session %s%v: %w", splitValuePrefix, key, err))
						}
					}
					sess.StoreLastActivity(session, time.Now())
					if err := session.Save(r, w); err != nil {
						saveErr = errors.Join(saveErr, err)
					}
				})
				return saveErr
			}

			bfbw := &beforeFirstByteWriter{
				w:      w,
				before: saveSession,
				logger: logger,
			}

			next.ServeHTTP(bfbw, r)

			// Ensure the session is saved - this happens if no bytes were written
			// (e.g. a redirect or empty body). If the response was already committed
			// by bfbw, it has handled the failure (a 500), so avoid double-writing.
			if err := saveSession(); err != nil && !bfbw.wroteHeader {
				response.InternalError(w, r, h, err)
				return
			}
		})
	}
}

// CheckSessionIdleNoAuth enforces an idle-timeout on the session independent of
// authentication. If the time since the session's last activity exceeds idleTTL,
// onIdle is invoked (e.g. to redirect to a login or logout page, or to write a
// 401) and the request does not proceed. Use it on routes that have no other
// auth check; routes behind [RequireAuthenticated] can enforce idleness there
// instead. Install it after [RequireSession].
func CheckSessionIdleNoAuth(idleTTL time.Duration, onIdle http.HandlerFunc) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			if session := webctx.SessionFromContext(ctx); session != nil {
				if t := sess.LastActivity(session); !t.IsZero() && time.Since(t) > idleTTL {
					onIdle(w, r)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// beforeFirstByteWriter is an http.ResponseWriter that runs a hook before the
// first byte (or header) is written, used to persist the session cookie before
// the body is committed. If the hook fails, the response is committed as a 500
// rather than the handler's intended status, so a session-save failure is never
// silently returned as success.
type beforeFirstByteWriter struct {
	w http.ResponseWriter

	before func() error
	logger *slog.Logger

	// wroteHeader records whether a status line has been committed to the
	// underlying writer, so the caller can avoid double-committing the response.
	wroteHeader bool
}

func (w *beforeFirstByteWriter) Header() http.Header {
	return w.w.Header()
}

func (w *beforeFirstByteWriter) WriteHeader(c int) {
	if w.wroteHeader {
		return
	}
	if err := w.before(); err != nil {
		// The session could not be saved. Do not return the handler's intended
		// (likely 2xx) status with a missing/invalid session cookie; send a 500.
		w.logger.Error("session save failed before response commit; sending 500", "error", err)
		w.wroteHeader = true
		w.w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.wroteHeader = true
	w.w.WriteHeader(c)
}

func (w *beforeFirstByteWriter) Write(b []byte) (int, error) {
	if err := w.before(); err != nil {
		return 0, err
	}
	w.wroteHeader = true
	return w.w.Write(b)
}

// Unwrap exposes the underlying writer so http.ResponseController (and thus
// optional interfaces like http.Flusher and http.Hijacker) keep working through
// this wrapper.
func (w *beforeFirstByteWriter) Unwrap() http.ResponseWriter {
	return w.w
}
