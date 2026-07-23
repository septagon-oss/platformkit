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
TOOLS_DIR := $(CURDIR)/.tmp-tools
STATICCHECK_BIN := $(TOOLS_DIR)/staticcheck-$(STATICCHECK_VERSION)
GOVULNCHECK_BIN := $(TOOLS_DIR)/govulncheck-$(GOVULNCHECK_VERSION)
GOSEC_BIN := $(TOOLS_DIR)/gosec-$(GOSEC_VERSION)
STATICCHECK ?= $(STATICCHECK_BIN)
GOVULNCHECK ?= $(GOVULNCHECK_BIN)
GOSEC ?= $(GOSEC_BIN)

ifeq ($(origin STATICCHECK),file)
STATICCHECK_PREREQ := $(STATICCHECK_BIN)
endif
ifeq ($(origin GOVULNCHECK),file)
GOVULNCHECK_PREREQ := $(GOVULNCHECK_BIN)
endif
ifeq ($(origin GOSEC),file)
GOSEC_PREREQ := $(GOSEC_BIN)
endif

.PHONY: build fmt-check test vet staticcheck race coverage govulncheck gosec security verify release-check

$(GOTMPDIR) $(TOOLS_DIR):
	mkdir -p "$@"

$(STATICCHECK_BIN): | $(GOTMPDIR) $(TOOLS_DIR)
	$(GO_BASE_ENV) GOBIN="$(TOOLS_DIR)" $(GO) install honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)
	mv "$(TOOLS_DIR)/staticcheck" "$@"

$(GOVULNCHECK_BIN): | $(GOTMPDIR) $(TOOLS_DIR)
	$(GO_BASE_ENV) GOBIN="$(TOOLS_DIR)" $(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	mv "$(TOOLS_DIR)/govulncheck" "$@"

$(GOSEC_BIN): | $(GOTMPDIR) $(TOOLS_DIR)
	$(GO_BASE_ENV) GOBIN="$(TOOLS_DIR)" $(GO) install github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION)
	mv "$(TOOLS_DIR)/gosec" "$@"

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

staticcheck: $(STATICCHECK_PREREQ) | $(GOTMPDIR)
	@packages="$$( $(GO_ENV) $(GO) list ./... )"; \
	if [[ -z "$$packages" ]]; then \
		echo "staticcheck: go list returned no packages"; \
		exit 1; \
	fi; \
	$(GO_ENV) $(STATICCHECK) $$packages

race: | $(GOTMPDIR)
	$(RACE_ENV) $(GO) test -race -count=1 ./...

coverage: | $(GOTMPDIR)
	$(GO_ENV) $(GO) test -coverpkg=./... -covermode=atomic -coverprofile="$(GOTMPDIR)/coverage.out" ./...
	$(GO_ENV) $(GO) tool cover -func="$(GOTMPDIR)/coverage.out"
	@total="$$( $(GO_ENV) $(GO) tool cover -func="$(GOTMPDIR)/coverage.out" | awk '/^total:/ { gsub("%", "", $$3); print $$3 }' )"; \
	awk -v got="$$total" -v minimum="$(COVERAGE_MIN)" 'BEGIN { if (got + 0 < minimum + 0) { printf "coverage %.1f%% is below %.1f%% floor\n", got, minimum; exit 1 } }'

govulncheck: $(GOVULNCHECK_PREREQ) | $(GOTMPDIR)
	@packages="$$( $(GO_ENV) $(GO) list ./... )"; \
	if [[ -z "$$packages" ]]; then \
		echo "govulncheck: go list returned no packages"; \
		exit 1; \
	fi; \
	$(GO_ENV) $(GOVULNCHECK) $$packages

gosec: $(GOSEC_PREREQ) | $(GOTMPDIR)
	@packages="$$( $(GO_ENV) $(GO) list ./... )"; \
	if [[ -z "$$packages" ]]; then \
		echo "gosec: go list returned no packages"; \
		exit 1; \
	fi; \
	$(GO_ENV) $(GOSEC) -quiet $$packages

security: govulncheck gosec

verify: fmt-check vet staticcheck test race build

release-check: verify coverage security
