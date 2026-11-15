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

// Package cookiestore provides a [github.com/gorilla/sessions.Store] backed by
// encrypted cookies whose HMAC and encryption keys can be hot-reloaded.
//
// Unlike a plain [github.com/gorilla/sessions.CookieStore], the keys are not
// fixed at construction time. Instead they are supplied by an [EntropyFunc] that
// is consulted on every encode and decode, so keys can be rotated (or sourced
// from a secret/key manager) without restarting the process.
//
// Cookie sessions cannot be revoked one at a time: a copied cookie stays valid
// until it expires or its key is rotated out. Bound how long a session lives
// with middleware.CheckSessionMaxAge.
package cookiestore

import (
	"net/http"

	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
)

// New returns a new session store whose cookies are signed and encrypted with
// keys obtained from fn. The concrete type is [*sessions.CookieStore].
//
// opts is copied, never modified. In the copy, Path defaults to "/", MaxAge to
// 30 days, and SameSite to Lax when it is unset or [http.SameSiteDefaultMode].
// HttpOnly is always set. Secure is always set unless [WithInsecureCookies] is
// passed, in which case it is cleared. The one exception: SameSite=None always
// forces Secure, even with [WithInsecureCookies], because browsers drop
// SameSite=None cookies that are not Secure. Nil options are skipped.
//
// These defaults are applied once, here. They are not enforced later: a
// handler that edits a session's Options (session.Options), or code that edits
// the returned store's CookieStore.Options, can still turn off HttpOnly or
// Secure, or set SameSite=None without Secure.
func New(fn EntropyFunc, opts *sessions.Options, o ...Option) sessions.Store {
	var cfg config
	for _, opt := range o {
		if opt != nil {
			opt(&cfg)
		}
	}

	var options sessions.Options
	if opts != nil {
		options = *opts
	}
	if options.Path == "" {
		options.Path = "/"
	}
	if options.MaxAge <= 0 {
		options.MaxAge = 30 * 86400 // 30d
	}
	if options.SameSite == 0 || options.SameSite == http.SameSiteDefaultMode {
		options.SameSite = http.SameSiteLaxMode
	}
	options.HttpOnly = true
	options.Secure = !cfg.insecure || options.SameSite == http.SameSiteNoneMode

	codec := &HotCodec{
		entropyFunc: fn,
		maxAge:      options.MaxAge,
	}

	return &sessions.CookieStore{
		Codecs:  []securecookie.Codec{codec},
		Options: &options,
	}
}
