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

// Package session provides typed, nil-safe accessors for the values the
// framework stores on a [github.com/gorilla/sessions.Session], plus a helper to
// obtain the session's [github.com/mikehelmick/go-bananas/flash.Flash].
//
// Storing values through these helpers keeps key names and value types
// consistent between the middleware that writes them and the handlers that read
// them. Every accessor tolerates a nil session.
package session

import (
	"encoding/gob"
	"time"

	"github.com/gorilla/sessions"
	"github.com/mikehelmick/go-bananas/flash"
)

// sessionKey is a unique type to avoid overwriting other values in the session
// map and to make accidental modification harder.
type sessionKey string

func init() {
	// The session value map is gob-encoded by cookie stores. Register the key
	// type so values stored under these keys can be serialized.
	gob.Register(sessionKey(""))
}

const (
	sessionKeyCSRFToken    = sessionKey("csrfToken")
	sessionKeyLastActivity = sessionKey("lastActivity")
	sessionKeyNonce        = sessionKey("nonce")
	sessionKeyRegion       = sessionKey("region")
)

// Flash returns the [flash.Flash] for the provided session, initializing the
// session's value map if necessary. It tolerates a nil session.
func Flash(session *sessions.Session) *flash.Flash {
	var values map[any]any
	if session != nil {
		if session.Values == nil {
			session.Values = make(map[any]any)
		}
		values = session.Values
	}
	return flash.New(values)
}

// StoreCSRFToken stores the CSRF token on the session. A nil session or
// zero-length token is a no-op.
func StoreCSRFToken(session *sessions.Session, token []byte) {
	if session == nil || len(token) == 0 {
		return
	}
	session.Values[sessionKeyCSRFToken] = token
}

// ClearCSRFToken removes the CSRF token from the session.
func ClearCSRFToken(session *sessions.Session) {
	sessionClear(session, sessionKeyCSRFToken)
}

// CSRFToken returns the CSRF token stored on the session, or nil if absent or
// malformed (in which case it is also cleared).
func CSRFToken(session *sessions.Session) []byte {
	v := sessionGet(session, sessionKeyCSRFToken)
	if v == nil {
		return nil
	}
	t, ok := v.([]byte)
	if !ok {
		delete(session.Values, sessionKeyCSRFToken)
		return nil
	}
	return t
}

// StoreLastActivity stores the time of the user's last activity, used to track
// idle session timeouts. A nil session is a no-op.
func StoreLastActivity(session *sessions.Session, t time.Time) {
	if session == nil {
		return
	}
	session.Values[sessionKeyLastActivity] = t.Unix()
}

// ClearLastActivity removes the last-activity time from the session.
func ClearLastActivity(session *sessions.Session) {
	sessionClear(session, sessionKeyLastActivity)
}

// LastActivity returns the time of the user's last activity, or the zero time if
// absent or malformed (in which case it is also cleared).
func LastActivity(session *sessions.Session) time.Time {
	v := sessionGet(session, sessionKeyLastActivity)
	if v == nil {
		return time.Time{}
	}
	i, ok := v.(int64)
	if !ok || i == 0 {
		delete(session.Values, sessionKeyLastActivity)
		return time.Time{}
	}
	return time.Unix(i, 0)
}

// StoreNonce stores the session's current nonce value. A nil session is a no-op.
func StoreNonce(session *sessions.Session, nonce string) {
	if session == nil {
		return
	}
	session.Values[sessionKeyNonce] = nonce
}

// ClearNonce removes the nonce from the session.
func ClearNonce(session *sessions.Session) {
	sessionClear(session, sessionKeyNonce)
}

// Nonce returns the current nonce from the session, or "" if absent or
// malformed.
func Nonce(session *sessions.Session) string {
	v := sessionGet(session, sessionKeyNonce)
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// StoreRegion stores the current operating region in the session. A nil session
// is a no-op.
func StoreRegion(session *sessions.Session, region string) {
	if session == nil {
		return
	}
	session.Values[sessionKeyRegion] = region
}

// ClearRegion removes the region from the session.
func ClearRegion(session *sessions.Session) {
	sessionClear(session, sessionKeyRegion)
}

// Region returns the current region from the session, or "" if absent or
// malformed.
func Region(session *sessions.Session) string {
	v := sessionGet(session, sessionKeyRegion)
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// sessionGet returns the value stored at key, or nil if the session or its value
// map is nil.
func sessionGet(session *sessions.Session, key sessionKey) any {
	if session == nil || session.Values == nil {
		return nil
	}
	return session.Values[key]
}

// sessionClear deletes the value stored at key. A nil session is a no-op.
func sessionClear(session *sessions.Session, key sessionKey) {
	if session == nil {
		return
	}
	delete(session.Values, key)
}
