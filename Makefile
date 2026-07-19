SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c

GO ?= go
GOTMPDIR ?= $(CURDIR)/.tmp-go-tmp
GO_ENV := CGO_ENABLED=0 GOTMPDIR="$(GOTMPDIR)" TMPDIR="$(GOTMPDIR)"
GO_FILES := $(shell find . -path './.tmp*' -prune -o -type f -name '*.go' -print | sort)

.PHONY: build fmt-check test vet verify

$(GOTMPDIR):
	mkdir -p "$@"

build: | $(GOTMPDIR)
	$(GO_ENV) $(GO) build -o "$(GOTMPDIR)/platformkit" .

fmt-check:
	@unformatted="$$(gofmt -l $(GO_FILES))"; \
	if [[ -n "$$unformatted" ]]; then \
		echo "Go files need formatting:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

test: | $(GOTMPDIR)
	$(GO_ENV) $(GO) test ./...

vet: | $(GOTMPDIR)
	$(GO_ENV) $(GO) vet ./...

verify: fmt-check vet test build
