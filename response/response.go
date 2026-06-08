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

package response

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/mikehelmick/go-bananas/internal/util"
	"github.com/mikehelmick/go-bananas/logging"
	"github.com/mikehelmick/go-bananas/render"
	"github.com/mikehelmick/go-bananas/webctx"
)

var (
	errBadRequest     = errors.New("bad request")
	errUnauthorized   = errors.New("unauthorized")
	errNotFound       = errors.New("not found")
	errMissingSession = errors.New("session missing in request context")
)

// Back redirects to the request's referrer with a 303 See Other. If the referrer
// is missing, unparseable, or points at a different host, it redirects to "/"
// instead, preventing open-redirect abuse.
func Back(w http.ResponseWriter, r *http.Request, h *render.Renderer) {
	logger := logging.Named(logging.FromContext(r.Context()), "response.Back")

	ref := r.Header.Get("Referer")
	if ref == "" {
		logger.Warn("referrer is empty")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	refURL, err := url.Parse(ref)
	if err != nil {
		logger.Warn("failed to parse referrer URL", "error", err)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	if got, want := refURL.Host, r.Host; got != want {
		logger.Warn("referrer host mismatch", "got", got, "want", want)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, ref, http.StatusSeeOther)
}

// SeeOther issues a 303 See Other redirect to path. It is a convenience for
// callers that need an [http.HandlerFunc]-style redirect, such as the onIdle
// handler of the session-idle middleware.
func SeeOther(w http.ResponseWriter, r *http.Request, path string) {
	http.Redirect(w, r, path, http.StatusSeeOther)
}

// InternalError logs err and renders a 500 response negotiated for the client
// (HTML, JSON, or plain text).
func InternalError(w http.ResponseWriter, r *http.Request, h *render.Renderer, err error) {
	logging.FromContext(r.Context()).Error("internal error", "error", err)

	accept := acceptList(r)
	switch {
	case prefixInList(accept, ContentTypeHTML):
		h.RenderHTML500(w, err)
	case prefixInList(accept, ContentTypeJSON):
		h.RenderJSON500(w, err)
	default:
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

// NotFound renders a 404 response negotiated for the client.
func NotFound(w http.ResponseWriter, r *http.Request, h *render.Renderer) {
	accept := acceptList(r)
	switch {
	case prefixInList(accept, ContentTypeHTML):
		m := webctx.TemplateMapFromContext(r.Context())
		m.Title("%s", http.StatusText(http.StatusNotFound))
		h.RenderHTMLStatus(w, http.StatusNotFound, "404", m)
	case prefixInList(accept, ContentTypeJSON):
		h.RenderJSON(w, http.StatusNotFound, errNotFound)
	default:
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	}
}

// Unauthorized renders a 401 response negotiated for the client. The framework
// always returns 401 (even when authentication is present but authorization
// fails).
func Unauthorized(w http.ResponseWriter, r *http.Request, h *render.Renderer) {
	accept := acceptList(r)
	switch {
	case prefixInList(accept, ContentTypeHTML):
		m := webctx.TemplateMapFromContext(r.Context())
		m.Title("%s", http.StatusText(http.StatusUnauthorized))
		h.RenderHTMLStatus(w, http.StatusUnauthorized, "401", m)
	case prefixInList(accept, ContentTypeJSON):
		h.RenderJSON(w, http.StatusUnauthorized, errUnauthorized)
	default:
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
	}
}

// BadRequest renders a 400 response negotiated for the client.
func BadRequest(w http.ResponseWriter, r *http.Request, h *render.Renderer) {
	accept := acceptList(r)
	switch {
	case prefixInList(accept, ContentTypeHTML):
		h.RenderHTMLStatus(w, http.StatusBadRequest, "400", nil)
	case prefixInList(accept, ContentTypeJSON):
		h.RenderJSON(w, http.StatusBadRequest, errBadRequest)
	default:
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
	}
}

// MissingSession returns an internal error indicating no session was present on
// the request context. It usually signals that the session middleware was not
// installed before the handler that needs it.
func MissingSession(w http.ResponseWriter, r *http.Request, h *render.Renderer) {
	InternalError(w, r, h, errMissingSession)
}

// RealHostFromRequest finds the "best" host for the request. The host may be in
// the request URL (when absolute) or in the Host header; when developing locally
// it may be missing entirely. The scheme defaults to http for localhost and
// https otherwise, and default ports are stripped.
func RealHostFromRequest(r *http.Request) string {
	u := r.URL
	scheme := u.Scheme
	host := u.Hostname()
	port := u.Port()

	// If the URI was not absolute, pull the host/port from the HTTP request.
	if !u.IsAbs() {
		hu := &url.URL{Host: r.Host}
		host, port = hu.Hostname(), hu.Port()
	}

	// Default to http on localhost, https elsewhere. The localhost check is not
	// robust, but it is good enough.
	if scheme == "" {
		if host == "localhost" || host == "127.0.0.1" {
			scheme = "http"
		} else {
			scheme = "https"
		}
	}

	// Strip default ports. Compare against the resolved scheme (u.Scheme is empty
	// for a typical non-absolute inbound request URL).
	if (scheme == "https" && port == "443") ||
		(scheme == "http" && port == "80") {
		port = ""
	}

	if port == "" {
		return fmt.Sprintf("%s://%s", scheme, host)
	}
	return fmt.Sprintf("%s://%s:%s", scheme, host, port)
}

// TracedHTTPClient returns an HTTP client whose requests carry the X-Request-ID
// and X-Cloud-Trace-Context headers from the request context, to correlate
// service-to-service calls. It should be used for internal calls, not for
// upstream third-party APIs.
func TracedHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: &withInjectedDataRoundTripper{util.DefaultHTTPTransport()},
	}
}

type withInjectedDataRoundTripper struct {
	original http.RoundTripper
}

func (t *withInjectedDataRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	ctx := r.Context()

	if requestID := webctx.RequestIDFromContext(ctx); requestID != "" {
		r.Header.Set("X-Request-ID", requestID)
	}
	if traceID := webctx.TraceIDFromContext(ctx); traceID != "" {
		r.Header.Set("X-Cloud-Trace-Context", traceID)
	}

	return t.original.RoundTrip(r)
}
