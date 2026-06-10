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

// Package middleware provides composable [github.com/gorilla/mux] middleware for
// building server-side-rendered web applications.
//
// Each constructor returns a [github.com/gorilla/mux.MiddlewareFunc] (an
// http.Handler decorator), so they can be registered on a router with Use or
// chained by hand. Most ordering is flexible, but a few have prerequisites,
// noted on each function. A typical chain is:
//
//	Recovery → PopulateRequestID → PopulateTraceID → PopulateLogger →
//	SecureHeaders → GzipResponse → RequireSession → HandleCSRF →
//	PopulateTemplateVariables → InjectCurrentPath → ProcessLocale
//
// Authentication is intentionally pluggable: implement the [Authenticator]
// interface (for example, an OIDC client) and install [RequireAuthenticated].
// The framework itself stays free of any specific auth provider dependency.
package middleware
