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
package cookiestore

import (
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
)

// New returns a new session store whose cookies are signed and encrypted with
// keys obtained from fn. If opts is nil a default set of options is used; in
// either case Path defaults to "/" and MaxAge defaults to 30 days when unset.
func New(fn EntropyFunc, opts *sessions.Options) sessions.Store {
	if opts == nil {
		opts = new(sessions.Options)
	}
	if opts.Path == "" {
		opts.Path = "/"
	}
	if opts.MaxAge <= 0 {
		opts.MaxAge = 30 * 86400 // 30d
	}

	codec := &HotCodec{
		entropyFunc: fn,
		maxAge:      opts.MaxAge,
	}

	return &sessions.CookieStore{
		Codecs:  []securecookie.Codec{codec},
		Options: opts,
	}
}
