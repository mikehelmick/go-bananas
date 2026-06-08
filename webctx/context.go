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

// Package webctx provides typed helpers for storing and retrieving
// request-scoped values on a [context.Context].
//
// The framework's middleware populates these values (session, template map,
// request and trace IDs, locale, operating system, nonce, and an authenticated
// principal) so handlers and templates can read them without re-deriving them.
// Every accessor is nil-safe: retrieving an absent value returns a sensible zero
// (nil, an empty string, or an empty [TemplateMap]).
package webctx

import (
	"context"

	"github.com/gorilla/sessions"
	"github.com/leonelquinteros/gotext"
)

// contextKey is a unique unexported type to avoid clashing with other packages
// that store values on a context.
type contextKey string

const (
	contextKeyLocale    = contextKey("locale")
	contextKeyNonce     = contextKey("nonce")
	contextKeyOS        = contextKey("os")
	contextKeyPrincipal = contextKey("principal")
	contextKeyRequestID = contextKey("requestID")
	contextKeySession   = contextKey("session")
	contextKeyTemplate  = contextKey("template")
	contextKeyTraceID   = contextKey("traceID")
)

// WithSession stores the session on the context for retrieval later via
// [SessionFromContext].
func WithSession(ctx context.Context, session *sessions.Session) context.Context {
	return context.WithValue(ctx, contextKeySession, session)
}

// SessionFromContext retrieves the session from the context. If no session
// exists, or the stored value is of the wrong type, it returns nil.
func SessionFromContext(ctx context.Context) *sessions.Session {
	v, ok := ctx.Value(contextKeySession).(*sessions.Session)
	if !ok {
		return nil
	}
	return v
}

// WithNonce stores a Content-Security-Policy nonce on the context.
func WithNonce(ctx context.Context, nonce string) context.Context {
	return context.WithValue(ctx, contextKeyNonce, nonce)
}

// NonceFromContext retrieves the nonce from the context, or "" if none is set.
func NonceFromContext(ctx context.Context) string {
	v, ok := ctx.Value(contextKeyNonce).(string)
	if !ok {
		return ""
	}
	return v
}

// WithRequestID stores the request ID on the context and mirrors it onto the
// template map under "requestID".
func WithRequestID(ctx context.Context, id string) context.Context {
	m := TemplateMapFromContext(ctx)
	m["requestID"] = id
	ctx = WithTemplateMap(ctx, m)
	return context.WithValue(ctx, contextKeyRequestID, id)
}

// RequestIDFromContext retrieves the request ID from the context, or "" if none
// is set.
func RequestIDFromContext(ctx context.Context) string {
	v, ok := ctx.Value(contextKeyRequestID).(string)
	if !ok {
		return ""
	}
	return v
}

// WithTraceID stores the trace ID on the context and mirrors it onto the
// template map under "traceID".
func WithTraceID(ctx context.Context, id string) context.Context {
	m := TemplateMapFromContext(ctx)
	m["traceID"] = id
	ctx = WithTemplateMap(ctx, m)
	return context.WithValue(ctx, contextKeyTraceID, id)
}

// TraceIDFromContext retrieves the trace ID from the context, or "" if none is
// set.
func TraceIDFromContext(ctx context.Context) string {
	v, ok := ctx.Value(contextKeyTraceID).(string)
	if !ok {
		return ""
	}
	return v
}

// WithLocale stores the translator/locale to use for this request.
func WithLocale(ctx context.Context, locale gotext.Translator) context.Context {
	return context.WithValue(ctx, contextKeyLocale, locale)
}

// LocaleFromContext returns the translator from the context, or nil if none is
// set.
func LocaleFromContext(ctx context.Context) gotext.Translator {
	v, ok := ctx.Value(contextKeyLocale).(gotext.Translator)
	if !ok {
		return nil
	}
	return v
}

// WithPrincipal stores the authenticated principal on the context. The principal
// is an opaque value supplied by an
// [github.com/mikehelmick/go-bananas/middleware.Authenticator]; its concrete
// type is defined by the application.
func WithPrincipal(ctx context.Context, principal any) context.Context {
	return context.WithValue(ctx, contextKeyPrincipal, principal)
}

// PrincipalFromContext retrieves the authenticated principal from the context,
// or nil if the request is anonymous.
func PrincipalFromContext(ctx context.Context) any {
	return ctx.Value(contextKeyPrincipal)
}

// WithOperatingSystem stores the detected client operating system on the
// context.
func WithOperatingSystem(ctx context.Context, os OS) context.Context {
	return context.WithValue(ctx, contextKeyOS, os)
}

// OperatingSystemFromContext retrieves the client operating system from the
// context. If none is set, it returns [OSUnknown].
func OperatingSystemFromContext(ctx context.Context) OS {
	v, ok := ctx.Value(contextKeyOS).(OS)
	if !ok {
		return OSUnknown
	}
	return v
}
