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
	"net/http"
	"net/url"
	"testing"
)

// FuzzCallbackState drives the OIDC callback with an arbitrary state and code
// after a real /login. Any state other than the one the app issued must be
// rejected with 400 before the code is exchanged, and the flow state must be
// gone from the saved session afterwards.
func FuzzCallbackState(f *testing.F) {
	f.Add("", "")
	f.Add("wrong", "code")
	f.Add("state with spaces & = ?", "\x00")
	f.Add("\xff\xfe", "%zz")
	f.Add("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "")

	idp := newFakeIDP(f, nil)
	a, srv := newOIDCTestApp(f, idp)

	f.Fuzz(func(t *testing.T, state, code string) {
		client := noFollowClient(nil)
		if state == startLogin(t, srv, client).Query().Get("state") {
			t.Skip("fuzzer guessed the issued state")
		}

		resp, err := client.Get(srv.URL + "/auth/callback?" +
			url.Values{"state": {state}, "code": {code}}.Encode())
		if err != nil {
			t.Fatalf("GET /auth/callback: %v", err)
		}
		_, _ = readClose(resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("callback with state %q = %d, want 400", state, resp.StatusCode)
		}
		if n := idp.tokenHits.Load(); n != 0 {
			t.Errorf("token endpoint hit %d times, want 0", n)
		}
		vals := sessionValues(t, a, srv, client)
		for _, k := range flowKeys {
			if _, ok := vals[k]; ok {
				t.Errorf("after a failed callback the session still has %q", k)
			}
		}
	})
}
