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

package webctx

// OS is a client operating system inferred from a request's User-Agent header by
// the AddOperatingSystemFromUserAgent middleware.
type OS int

const (
	// OSUnknown indicates the operating system could not be determined.
	OSUnknown OS = iota
	// OSIOS indicates an Apple iOS client.
	OSIOS
	// OSAndroid indicates an Android client.
	OSAndroid
)

// String returns a human-readable name for the operating system.
func (o OS) String() string {
	switch o {
	case OSIOS:
		return "iOS"
	case OSAndroid:
		return "Android"
	case OSUnknown:
		return "Unknown"
	default:
		return "Unknown"
	}
}
