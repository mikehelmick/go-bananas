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

package logging_test

import (
	"context"

	"github.com/mikehelmick/go-bananas/logging"
)

func ExampleFromContext() {
	// Store a logger on a context (typically done once per request by
	// middleware), then retrieve it deeper in the call stack.
	ctx := logging.WithLogger(context.Background(), logging.DefaultLogger())

	logger := logging.FromContext(ctx)
	logger.Info("handling request", "path", "/")

	// Components identify themselves with a named logger.
	logging.Named(logger, "middleware.CSRF").Debug("validated token")
}
