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
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/mikehelmick/go-bananas/internal/util"
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

	// splitBindingValueKey is the session-value key for the random ID that
	// binds split cookies to their main session. It is a plain string, which
	// gob encodes without registration, so go-bananas v1.0.1 can still decode
	// sessions that carry it.
	splitBindingValueKey = "_gbs_binding"
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
// out into a separate companion cookie on save and rejoined on load. Each
// companion cookie is bound to the main session by a random ID and is joined
// only alongside that session; resetting the session's Values map unbinds it,
// and deleting the session (Options.MaxAge < 0) expires it.
//
// The binding ID is stored in the session values under the string key
// "_gbs_binding", which is reserved; handlers must not set or remove it, and
// must not copy it into a new session at a login reset (assign a fresh Values
// map instead).
//
// Split cookies written by go-bananas v1.0.1 and earlier carry no binding, so
// their values are dropped once after upgrading and must be set again.
func RequireSession(store sessions.Store, splitValues []any, h *render.Renderer) mux.MiddlewareFunc {
	return buildHandler(store, defaultSessionName, splitValues, h)
}

func splitCookieName(key any) string {
	return fmt.Sprintf("%s%v", splitValuePrefix, key)
}

// joinSplitSessionCookie retrieves a companion cookie holding a single value that
// was previously split out of the main session cookie, folding its value back
// into session. The value is joined only when the companion cookie carries the
// main session's binding ID and the main session is not new, so a split cookie
// replayed alone, or after its session was reset, is dropped. An unjoined split
// cookie is expired on save.
func joinSplitSessionCookie(r *http.Request, store sessions.Store, key any, session *sessions.Session, logger *slog.Logger) *sessions.Session {
	name := splitCookieName(key)
	splitSession, err := store.Get(r, name)
	if err != nil {
		logger.Warn("failed to get split session cookie; creating a new one", "name", name, "error", err)
		splitSession, _ = store.New(r, name)
	}
	v, ok := splitSession.Values[key]
	if !ok {
		return splitSession
	}
	id, splitID := splitBindingID(session), splitBindingID(splitSession)
	if session.IsNew || id == "" || subtle.ConstantTimeCompare([]byte(id), []byte(splitID)) != 1 {
		logger.Debug("dropping split session cookie not bound to this session", "name", name)
		return splitSession
	}
	session.Values[key] = v
	return splitSession
}

// splitSessionCookie moves the value for key out of the main session and into
// splitSession, bound to the main session's ID, so it can be saved as its own
// cookie. It reports whether splitSession needs saving.
//
// If the main session has no value for key, or is being deleted
// (Options.MaxAge < 0), the split cookie is expired instead, but only when
// the request carried it; otherwise there is nothing to save.
func splitSessionCookie(r *http.Request, session, splitSession *sessions.Session, key any, id string) bool {
	value, ok := session.Values[key]
	delete(session.Values, key)
	splitSession.Values = make(map[any]any)

	deleting := session.Options != nil && session.Options.MaxAge < 0
	if ok && !deleting {
		splitSession.Values[key] = value
		splitSession.Values[splitBindingValueKey] = id
		return true
	}

	if _, err := r.Cookie(splitCookieName(key)); err != nil {
		return false
	}
	var opts sessions.Options
	if splitSession.Options != nil {
		opts = *splitSession.Options
	}
	opts.MaxAge = -1
	splitSession.Options = &opts
	return true
}

// splitBindingID returns the binding ID stored on session, or "".
func splitBindingID(session *sessions.Session) string {
	id, _ := session.Values[splitBindingValueKey].(string)
	return id
}

// ensureSplitBindingID returns the session's binding ID, generating and
// storing a new one if it has none (a new session, or a Values map the
// handler reset or replaced).
func ensureSplitBindingID(session *sessions.Session) (string, error) {
	if id := splitBindingID(session); id != "" {
		return id, nil
	}
	b, err := util.RandomBytes(32)
	if err != nil {
		return "", fmt.Errorf("generate split binding ID: %w", err)
	}
	id := base64.RawURLEncoding.EncodeToString(b)
	if session.Values == nil {
		session.Values = make(map[any]any)
	}
	session.Values[splitBindingValueKey] = id
	return id, nil
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
					// Split and save individual cookies, bound to the session's
					// binding ID. A handler that reset or replaced the Values map
					// gets a new ID, which unbinds every earlier split cookie.
					if len(splitSessions) > 0 {
						id, err := ensureSplitBindingID(session)
						if err != nil {
							saveErr = err
							return
						}
						for key, splitSession := range splitSessions {
							if !splitSessionCookie(r, session, splitSession, key, id) {
								continue
							}
							if err := splitSession.Save(r, w); err != nil {
								saveErr = errors.Join(saveErr, fmt.Errorf("save split session %s%v: %w", splitValuePrefix, key, err))
							}
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
