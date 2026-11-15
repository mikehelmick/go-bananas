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

package cookiestore

// config holds the settings that [Option] values change.
type config struct {
	insecure bool
}

// Option configures [New].
type Option func(*config)

// WithInsecureCookies stops [New] from forcing the Secure attribute on
// session cookies, so browsers send them over plain HTTP. Use it only for
// local development: a cookie without Secure can leak over any unencrypted
// connection to the same host. It has no effect on SameSite=None cookies,
// which are always Secure because browsers drop them otherwise. Like the other
// defaults [New] applies, it covers only the options passed to New, not later
// edits to a session's or the store's Options.
func WithInsecureCookies() Option {
	return func(c *config) {
		c.insecure = true
	}
}
