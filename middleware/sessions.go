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
	"encoding/gob"
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
)

// RequireNamedSession is like [RequireSession] but uses a specific session
// (cookie) name instead of the default.
func RequireNamedSession(store sessions.Store, name string, splitValues []any, h *render.Renderer) mux.MiddlewareFunc {
	return buildHandler(store, name, splitValues, h)
}

// RequireSession loads or creates a session from store, stores it on the request
// context, and ensures the session's flash data is available to templates. Any
// handler that uses sessions, flash messages, or CSRF must be wrapped with this.
// It records a created-at time on sessions that lack one, which
// [CheckSessionMaxAge] reads.
//
// splitValues names session keys whose values are large enough to warrant their
// own cookie (browsers cap individual cookies at ~4KB); each listed key is split
// out into a separate companion cookie on save and rejoined on load. Each
// companion cookie is bound to the main session by a random ID and is joined
// only alongside that session; resetting or deleting the session unbinds or
// expires it. One split key per router is supported.
//
// Split cookies written by go-bananas v1.0.x carry no binding and are dropped.
// During a rolling deploy, a visitor moving between v1.0.x and v1.1 instances
// loses split values each time, so finish the rollout before relying on them.
func RequireSession(store sessions.Store, splitValues []any, h *render.Renderer) mux.MiddlewareFunc {
	return buildHandler(store, defaultSessionName, splitValues, h)
}

func splitCookieName(key any) string {
	return fmt.Sprintf("%s%v", splitValuePrefix, key)
}

// joinSplitSessionCookie retrieves a companion cookie holding a single value that
// was previously split out of the main session cookie, folding its value back
// into session. The value is joined only when the companion cookie carries the
// main session's ID and the main session is not new, so a split cookie replayed
// alone, or after its session was reset (login, logout, expiry), is dropped.
// Split cookies from go-bananas v1.0.x carry no ID and are dropped once.
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
		storeSplitBindingID(splitSession, id)
		return true
	}

	if _, err := r.Cookie(splitCookieName(key)); err != nil {
		return false
	}
	opts := *splitSession.Options
	opts.MaxAge = -1
	splitSession.Options = &opts
	return true
}

// splitBindingKey is the session-value key type for the ID that binds split
// cookies to their main session. It is private to this package.
type splitBindingKey string

const splitBindingIDKey = splitBindingKey("splitBindingID")

func init() {
	// Session values are gob-encoded by cookie stores; register the key type.
	gob.Register(splitBindingKey(""))
}

// splitBindingID returns the binding ID stored on session, or "".
func splitBindingID(session *sessions.Session) string {
	id, _ := session.Values[splitBindingIDKey].(string)
	return id
}

// storeSplitBindingID stores id as session's binding ID.
func storeSplitBindingID(session *sessions.Session, id string) {
	if session.Values == nil {
		session.Values = make(map[any]any)
	}
	session.Values[splitBindingIDKey] = id
}

// ensureSessionID returns the session's random binding ID, generating and
// storing one if it has none (a new session, or a value map the handler
// replaced).
func ensureSessionID(session *sessions.Session) (string, error) {
	if id := splitBindingID(session); id != "" {
		return id, nil
	}
	id, err := util.RandomToken(32)
	if err != nil {
		return "", fmt.Errorf("generate session ID: %w", err)
	}
	storeSplitBindingID(session, id)
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
			// Stamp the creation time on new sessions, and on sessions issued
			// before created-at existed, so CheckSessionMaxAge can bound them.
			stampCreatedAt(session)
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
					// Split and save individual cookies, bound to the session ID. A
					// handler that replaced the value map gets a new ID, which
					// unbinds every earlier split cookie.
					if len(splitSessions) > 0 {
						id, err := ensureSessionID(session)
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
					// The handler may have replaced the value map (e.g. on login),
					// which starts a new session lifetime.
					stampCreatedAt(session)
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

// stampCreatedAt records now as the session's creation time if it has none.
func stampCreatedAt(session *sessions.Session) {
	if sess.CreatedAt(session).IsZero() {
		sess.StoreCreatedAt(session, time.Now())
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

// CheckSessionMaxAge enforces an absolute lifetime on the session. If the
// session was created more than maxAge ago, its values are cleared and
// onExpired is invoked; the request does not proceed. [RequireSession] then
// saves the cleared session with a new created-at time.
//
// Install it after [RequireSession] and before [HandleCSRF], so an expired
// session is cleared before HandleCSRF validates it or hands its token to
// templates; otherwise a request on an expired session would be checked
// against, and render, a token that is cleared moments later. Make onExpired
// redirect (for example a 303 to the login page) rather than render a page:
// HandleCSRF has not run, so a page rendered there has no CSRF token and its
// forms would fail. The redirected request starts a fresh session.
//
// The age counts from when [RequireSession] first stamped the session, which
// is usually the visitor's first anonymous request. At the login boundary,
// reset the session (assign a new Values map, which also defends against
// session fixation) or call session.StoreCreatedAt with the current time, so
// the lifetime starts at login.
//
// A session with no created-at time (for example one issued before go-bananas
// v1.1.0) is treated as created now and stamped. A maxAge of zero or less
// disables the check.
//
// Cookie sessions cannot be revoked one at a time; this bounds how long a
// copied cookie stays useful. Rotating the cookie keys revokes all sessions.
func CheckSessionMaxAge(maxAge time.Duration, onExpired http.HandlerFunc) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if session := webctx.SessionFromContext(r.Context()); session != nil && maxAge > 0 {
				stampCreatedAt(session)
				if time.Since(sess.CreatedAt(session)) > maxAge {
					clear(session.Values)
					onExpired(w, r)
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
