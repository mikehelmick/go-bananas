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

// Package gobananas is the documentation root for go-bananas, the application
// framework for Go that is so simple, it's Bananas.
//
// go-bananas is a lean, server-side-rendered (SSR) web framework plus a small
// application-infrastructure layer. It is assembled from focused, composable
// packages rather than a single monolith, so you import only what you need.
//
// # Web layer
//
//   - [github.com/mikehelmick/go-bananas/render]: an html/template + text/template
//     renderer with a rich default FuncMap, Subresource Integrity (SRI) asset
//     tags, and optional hot-reload in development.
//   - [github.com/mikehelmick/go-bananas/middleware]: composable gorilla/mux
//     middleware (secure headers, CSRF, sessions, gzip, request/trace IDs,
//     locale, and a pluggable Authenticator seam).
//   - [github.com/mikehelmick/go-bananas/session],
//     [github.com/mikehelmick/go-bananas/flash], and
//     [github.com/mikehelmick/go-bananas/cookiestore]: typed session accessors,
//     one-shot flash messages, and a hot-reloadable secure-cookie store.
//   - [github.com/mikehelmick/go-bananas/webctx] and
//     [github.com/mikehelmick/go-bananas/response]: request-scoped context
//     helpers and HTTP response/error helpers.
//
// # Infrastructure layer
//
//   - [github.com/mikehelmick/go-bananas/server]: a gracefully-stoppable HTTP
//     server.
//   - [github.com/mikehelmick/go-bananas/secrets] and
//     [github.com/mikehelmick/go-bananas/keys]: pluggable secret and key
//     managers. The core ships filesystem and in-memory providers; cloud
//     providers (GCP, AWS, Azure, Vault) are opt-in sub-packages you blank-import
//     to self-register, so their SDKs are only linked when used.
//   - [github.com/mikehelmick/go-bananas/cache]: a generic, TTL-based in-memory
//     cache.
//   - [github.com/mikehelmick/go-bananas/logging]: a context-scoped
//     [log/slog]-based logger.
//
// See the example application under examples/ssr-oidc for an end-to-end SSR app
// wiring these packages together, including an OIDC Authenticator.
package gobananas
