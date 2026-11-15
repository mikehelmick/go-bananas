# Copyright 2020 the Exposure Notifications Verification Server authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

GOFMT_FILES = $(shell go list -f '{{.Dir}}' ./...)
HTML_FILES = $(shell find . -name \*.html)
GO_FILES = $(shell find . -name \*.go)
MD_FILES = $(shell find . -name \*.md)
PO_FILES = $(shell find . -name \*.po)

# diff-check runs git-diff and fails if there are any changes.
diff-check:
	@FINDINGS="$$(git status -s -uall)" ; \
		if [ -n "$${FINDINGS}" ]; then \
			echo "Changed files:\n\n" ; \
			echo "$${FINDINGS}\n\n" ; \
			echo "Diffs:\n\n" ; \
			git diff ; \
			git diff --cached ; \
			exit 1 ; \
		fi
.PHONY: diff-check

generate:
	@go generate ./...
.PHONY: generate

generate-check: generate diff-check
.PHONY: generate-check

# lint uses the same linter as CI and tries to report the same results running
# locally. There is a chance that CI detects linter errors that are not found
# locally, but it should be rare.
GOLANGCI_LINT_VERSION = v2.15.0

lint:
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@golangci-lint run --config .golangci.yaml
.PHONY: lint

tabcheck:
	@FINDINGS="$$(awk '/\t/ {printf "%s:%s:found tab character\n",FILENAME,FNR}' $(HTML_FILES))"; \
		if [ -n "$${FINDINGS}" ]; then \
			echo "$${FINDINGS}\n\n"; \
			exit 1; \
		fi
.PHONY: tabcheck

test:
	@go test \
		-count=1 \
		-short \
		-shuffle=on \
		-tags=google \
		-timeout=5m \
		./...
.PHONY: test

test-acc:
	@go test \
		-count=1 \
		-race \
		-shuffle=on \
		-tags=google \
		-timeout=10m \
		./... \
		-coverprofile=./coverage.out
.PHONY: test-acc

# test-examples builds and tests the nested example modules. They are separate
# modules that use a local replace of the framework, so the root module's
# ./... does not cover them and dependabot cannot see the drift a framework
# dependency bump causes in their go.mod.
test-examples:
	@for mod in $$(find ./examples -name go.mod); do \
		dir="$$(dirname $$mod)"; \
		echo "==> $$dir"; \
		(cd "$$dir" && go build ./... && go test \
			-count=1 \
			-shuffle=on \
			-timeout=5m \
			./...) || exit 1; \
	done
.PHONY: test-examples

# fuzz runs every native Go fuzz target for FUZZTIME each, one target at a
# time because -fuzz accepts a single package and target. It covers the root
# module and the nested examples/ssr-oidc module, and stops on the first
# failure. Minimization is capped so a crasher does not stall the run.
FUZZTIME ?= 30s

fuzz:
	@for dir in . ./examples/ssr-oidc; do \
		tags=""; \
		if [ "$$dir" = "." ]; then tags="-tags=google"; fi; \
		list="$$(cd "$$dir" && go test $$tags -list '^Fuzz' ./...)" || { echo "$$list"; exit 1; }; \
		targets="$$(echo "$$list" | awk '/^Fuzz/ { names = names " " $$1; next } /^ok/ { n = split(names, a, " "); for (i = 1; i <= n; i++) print $$2, a[i]; names = "" }')"; \
		echo "$$targets" | while read -r pkg name; do \
			[ -n "$$pkg" ] || continue; \
			echo "==> $$pkg $$name"; \
			(cd "$$dir" && go test $$tags \
				-run '^$$' \
				-fuzz "^$$name$$" \
				-fuzztime $(FUZZTIME) \
				-fuzzminimizetime 5s \
				"$$pkg") || exit 1; \
		done || exit 1; \
	done
.PHONY: fuzz

test-coverage:
	@go tool cover -func=./coverage.out
.PHONY: test-coverage

zapcheck:
	@go install github.com/sethvargo/zapw/cmd/zapw
	@zapw ./...
.PHONY: zapcheck

pofmt:
	@go run ./tools/pofmt $(PO_FILES)
.PHONY: pofmt
