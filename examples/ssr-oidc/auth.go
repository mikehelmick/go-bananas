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

package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/mikehelmick/go-bananas/render"
	"github.com/mikehelmick/go-bananas/response"
	gbsession "github.com/mikehelmick/go-bananas/session"
	"github.com/mikehelmick/go-bananas/webctx"
	"golang.org/x/oauth2"
)

// randString returns a URL-safe random string with n bytes of entropy.
func randString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// User is the authenticated principal stored on the session. It is the concrete
// type the application's Authenticator returns.
type User struct {
	Subject string
	Email   string
	Name    string
}

func init() {
	// Registered so the user can be gob-encoded into the secure-cookie session.
	gob.Register(&User{})
}

const (
	sessionUserKey     = "user"
	sessionStateKey    = "oidc_state"
	sessionNonceKey    = "oidc_nonce"
	sessionVerifierKey = "oidc_verifier"
)

// resetSession replaces the session's value map with a fresh one. It is used at
// the login/logout privilege boundary to defend against session fixation: any
// pre-authentication state (including the CSRF token, which the CSRF middleware
// then regenerates) is discarded.
func resetSession(r *http.Request) {
	if s := webctx.SessionFromContext(r.Context()); s != nil {
		s.Values = make(map[any]any)
	}
}

// storeUser saves u on the request's session.
func storeUser(r *http.Request, u *User) {
	if s := webctx.SessionFromContext(r.Context()); s != nil {
		s.Values[sessionUserKey] = u
	}
}

// userFromSession returns the user stored on the request's session, or nil.
func userFromSession(r *http.Request) *User {
	s := webctx.SessionFromContext(r.Context())
	if s == nil {
		return nil
	}
	u, _ := s.Values[sessionUserKey].(*User)
	return u
}

// SessionAuthenticator implements [middleware.Authenticator] by returning the
// user that the OIDC callback (or dev login) stored on the session. This is the
// seam the framework uses to gate protected routes; it has no dependency on the
// identity provider itself.
type SessionAuthenticator struct{}

// Authenticate returns the session user, or (nil, nil) when the request is
// anonymous.
func (SessionAuthenticator) Authenticate(r *http.Request) (any, error) {
	if u := userFromSession(r); u != nil {
		return u, nil
	}
	return nil, nil
}

// OIDC wires an OpenID Connect authorization-code flow. It is optional: when the
// server is started without OIDC configuration, it is nil and the login routes
// report that sign-in is unavailable.
type OIDC struct {
	verifier *oidc.IDTokenVerifier
	oauth2   oauth2.Config
	renderer *render.Renderer
}

// NewOIDC discovers the provider at issuer and builds the auth-code flow.
func NewOIDC(ctx context.Context, h *render.Renderer, issuer, clientID, clientSecret, redirectURL string) (*OIDC, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery failed: %w", err)
	}
	return &OIDC{
		verifier: provider.Verifier(&oidc.Config{ClientID: clientID}),
		oauth2: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  redirectURL,
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
		renderer: h,
	}, nil
}

// Login starts the flow: it stores a random state and nonce on the session and
// redirects the browser to the provider's authorization endpoint.
func (o *OIDC) Login(w http.ResponseWriter, r *http.Request) {
	session := webctx.SessionFromContext(r.Context())
	if session == nil {
		response.MissingSession(w, r, o.renderer)
		return
	}

	state, err := randString(32)
	if err != nil {
		response.InternalError(w, r, o.renderer, err)
		return
	}
	nonce, err := randString(32)
	if err != nil {
		response.InternalError(w, r, o.renderer, err)
		return
	}
	// PKCE: a per-flow verifier defends against authorization-code injection
	// (RFC 9700), even for confidential clients.
	verifier := oauth2.GenerateVerifier()

	session.Values[sessionStateKey] = state
	session.Values[sessionNonceKey] = nonce
	session.Values[sessionVerifierKey] = verifier

	authURL := o.oauth2.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

// Callback completes the flow: it validates state, exchanges the code, verifies
// the ID token and nonce, stores the resulting user on the session, and
// redirects to the profile page.
func (o *OIDC) Callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	session := webctx.SessionFromContext(ctx)
	if session == nil {
		response.MissingSession(w, r, o.renderer)
		return
	}

	// Clear the in-flight flow state on every exit, so a failed callback cannot
	// be replayed and forces a fresh /login.
	defer func() {
		delete(session.Values, sessionStateKey)
		delete(session.Values, sessionNonceKey)
		delete(session.Values, sessionVerifierKey)
	}()

	wantState, _ := session.Values[sessionStateKey].(string)
	if wantState == "" || r.URL.Query().Get("state") != wantState {
		response.BadRequest(w, r, o.renderer)
		return
	}

	verifier, _ := session.Values[sessionVerifierKey].(string)
	oauth2Token, err := o.oauth2.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		response.InternalError(w, r, o.renderer, fmt.Errorf("token exchange failed: %w", err))
		return
	}
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		response.InternalError(w, r, o.renderer, fmt.Errorf("missing id_token in token response"))
		return
	}
	idToken, err := o.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		response.InternalError(w, r, o.renderer, fmt.Errorf("id token verification failed: %w", err))
		return
	}

	wantNonce, _ := session.Values[sessionNonceKey].(string)
	if idToken.Nonce != wantNonce {
		response.BadRequest(w, r, o.renderer)
		return
	}

	var claims struct {
		Subject string `json:"sub"`
		Email   string `json:"email"`
		Name    string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		response.InternalError(w, r, o.renderer, err)
		return
	}

	// Reset the session at the authentication boundary (fixation defense), then
	// establish the authenticated principal.
	resetSession(r)
	storeUser(r, &User{Subject: idToken.Subject, Email: claims.Email, Name: claims.Name})

	gbsession.Flash(session).Alert("Welcome, %s!", claims.Name)
	http.Redirect(w, r, "/me", http.StatusSeeOther)
}
