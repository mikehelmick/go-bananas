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

package form

import (
	"bytes"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fuzzAddress struct {
	City string `form:"city" validate:"max=16"`
	Zip  int    `form:"zip"`
}

type fuzzDest struct {
	Age     int         `form:"age" validate:"gte=0"`
	Tags    []string    `form:"tags"`
	Address fuzzAddress `form:"address"`
	Count   *int        `form:"count"`
	When    time.Time   `form:"when"`
	Name    string      `form:"name" validate:"required"`
}

// fuzzMaxBodyBytes is small so the fuzzer reaches the cap easily.
const fuzzMaxBodyBytes = 512

// FuzzBind posts an arbitrary body with an arbitrary Content-Type through Bind.
// Bind must not panic, must reject a urlencoded body over the configured cap,
// must never keep more than the cap of a multipart body, and must never return
// field errors alongside an error.
func FuzzBind(f *testing.F) {
	const boundary = "x"
	multipart := func(parts ...string) []byte {
		var b strings.Builder
		for i := 0; i+1 < len(parts); i += 2 {
			b.WriteString("--" + boundary + "\r\nContent-Disposition: form-data; name=\"" + parts[i] + "\"\r\n\r\n" + parts[i+1] + "\r\n")
		}
		b.WriteString("--" + boundary + "--\r\n")
		return []byte(b.String())
	}
	const urlencoded = "application/x-www-form-urlencoded"
	const mp = "multipart/form-data; boundary=" + boundary

	f.Add(urlencoded, []byte("name=a&age=3&tags=x&tags=y&address.city=c&address.zip=1&count=2&when=x"))
	f.Add(urlencoded, []byte("age=notanumber&count=1e9999&address.zip=-"))
	f.Add(urlencoded, []byte("tags.0=a&tags.99999999=b"))
	f.Add(urlencoded, []byte("address=1&address.city.x=2&when.wall=1"))
	f.Add(urlencoded, []byte("%zz=1&a=%"))
	f.Add(urlencoded, bytes.Repeat([]byte("a"), fuzzMaxBodyBytes+1))
	f.Add(urlencoded+"; charset=utf-8", []byte("name=a"))
	f.Add("APPLICATION/X-WWW-FORM-URLENCODED", []byte("name=a"))
	f.Add(mp, multipart("name", "a", "age", "x", "tags", "t"))
	f.Add(mp, append(multipart("name", "a"), bytes.Repeat([]byte("z"), fuzzMaxBodyBytes)...))
	f.Add(mp, multipart("name", strings.Repeat("a", fuzzMaxBodyBytes)))
	f.Add("multipart/form-data", []byte("--x--"))
	f.Add("multipart/form-data; boundary=", []byte(""))
	f.Add("text/plain", []byte("name=a"))
	f.Add("", []byte("name=a"))
	f.Add(";;;", []byte(""))

	f.Fuzz(func(t *testing.T, contentType string, body []byte) {
		r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
		r.Header.Set("Content-Type", contentType)

		var dst fuzzDest
		errs, err := Bind(r, &dst, WithMaxBodyBytes(fuzzMaxBodyBytes))
		if err != nil {
			if errs != nil {
				t.Fatalf("Bind returned field errors %v alongside error %v", errs, err)
			}
		} else if errs == nil {
			t.Fatal("Bind returned neither field errors nor an error")
		}

		mt, _, perr := mime.ParseMediaType(contentType)
		if perr == nil && mt == "application/x-www-form-urlencoded" && len(body) > fuzzMaxBodyBytes && err == nil {
			t.Fatalf("urlencoded body of %d bytes over the %d-byte cap bound without error", len(body), fuzzMaxBodyBytes)
		}

		// A multipart parse may stop at the closing boundary without reading
		// the epilogue, so a long body can succeed; what it kept must still
		// fit under the cap.
		if err == nil && r.MultipartForm != nil {
			kept := 0
			for k, vs := range r.MultipartForm.Value {
				kept += len(k)
				for _, v := range vs {
					kept += len(v)
				}
			}
			if kept > fuzzMaxBodyBytes {
				t.Fatalf("multipart parse kept %d bytes, over the %d-byte cap", kept, fuzzMaxBodyBytes)
			}
		}
	})
}
