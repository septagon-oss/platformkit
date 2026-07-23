SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c

GO ?= go
GOTMPDIR ?= $(CURDIR)/.tmp-go-tmp
GO_BASE_ENV := GOWORK=off GOFLAGS=-buildvcs=false GOTMPDIR="$(GOTMPDIR)" TMPDIR="$(GOTMPDIR)"
GO_ENV := CGO_ENABLED=0 $(GO_BASE_ENV)
RACE_ENV := CGO_ENABLED=1 $(GO_BASE_ENV)
GO_FILES := $(shell find . -path './.tmp*' -prune -o -type f -name '*.go' -print | sort)
STATICCHECK_VERSION ?= v0.7.0
GOVULNCHECK_VERSION ?= v1.6.0
GOSEC_VERSION ?= v2.28.0
COVERAGE_MIN ?= 40.0
STATICCHECK ?= go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)
GOVULNCHECK ?= go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
GOSEC ?= go run github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION)

.PHONY: build fmt-check test vet staticcheck race coverage govulncheck gosec security verify release-check

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
	$(GO_ENV) $(GO) test -count=1 ./...

vet: | $(GOTMPDIR)
	$(GO_ENV) $(GO) vet ./...

staticcheck: | $(GOTMPDIR)
	$(GO_ENV) $(STATICCHECK) ./...

race: | $(GOTMPDIR)
	$(RACE_ENV) $(GO) test -race -count=1 ./...

coverage: | $(GOTMPDIR)
	$(GO_ENV) $(GO) test -coverpkg=./... -covermode=atomic -coverprofile="$(GOTMPDIR)/coverage.out" ./...
	$(GO_ENV) $(GO) tool cover -func="$(GOTMPDIR)/coverage.out"
	@total="$$( $(GO_ENV) $(GO) tool cover -func="$(GOTMPDIR)/coverage.out" | awk '/^total:/ { gsub("%", "", $$3); print $$3 }' )"; \
	awk -v got="$$total" -v minimum="$(COVERAGE_MIN)" 'BEGIN { if (got + 0 < minimum + 0) { printf "coverage %.1f%% is below %.1f%% floor\n", got, minimum; exit 1 } }'

govulncheck: | $(GOTMPDIR)
	$(GO_ENV) $(GOVULNCHECK) ./...

gosec: | $(GOTMPDIR)
	$(GO_ENV) $(GOSEC) -quiet ./...

security: govulncheck gosec

verify: fmt-check vet staticcheck test race build

release-check: verify coverage security
