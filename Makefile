# make check is the required source gate; browser and capacity checks have
# separate targets because they need their own runtime fixtures.

.DEFAULT_GOAL := help

# Select the same compiler and tools even when PATH contains a newer Go release.
# Child scripts and their Go subprocesses inherit this exact module version.
export GOTOOLCHAIN := go$(shell sed -n 's/^go //p' go.mod)
.PHONY: help build test vet run e2e load-test check-loc check-packages check-gucs fmt-check check fmt image up down

# Tests talk to a real Postgres, as two roles: the owner runs migrations, the
# app role is subject to row-level security so the isolation tests mean
# something. `make up` starts a database that matches these defaults, on the
# same PLATFORMKIT_PG_PORT, so overriding the port once moves both.
PLATFORMKIT_PG_PORT ?= 5432
PLATFORMKIT_NATS_PORT ?= 4222
PLATFORMKIT_TEST_ADMIN_URL ?= postgres://postgres:platformkit@localhost:$(PLATFORMKIT_PG_PORT)/platformkit?sslmode=disable
PLATFORMKIT_TEST_DATABASE_URL ?= postgres://platformkit_app:platformkit@localhost:$(PLATFORMKIT_PG_PORT)/platformkit?sslmode=disable
# The JetStream transport is tested against the same NATS `make up` starts. The
# test fails rather than skips when this is unset: a suite that quietly skips
# the transport it ships proves nothing.
PLATFORMKIT_TEST_NATS_URL ?= nats://localhost:$(PLATFORMKIT_NATS_PORT)
export PLATFORMKIT_TEST_ADMIN_URL
export PLATFORMKIT_TEST_DATABASE_URL
export PLATFORMKIT_TEST_NATS_URL

# Local feedback uses Go's package/dependency cache. The full check below always
# runs fresh, independently of these local selectors or an earlier test goal.
TEST_PACKAGES ?= ./...
TEST_FLAGS ?=
# gotestsum options apply only to local feedback; --watch keeps the default
# all-package scope so Go's cache also checks consumers of an edited package.
TEST_OPTIONS ?=
# Runner reports: GOTESTSUM_JSONFILE=/tmp/pkit-tests.jsonl and
# GOTESTSUM_JUNITFILE=/tmp/pkit-tests.xml (overwritten on each run).
# Inspect timings: go tool gotestsum tool slowest --jsonfile /tmp/pkit-tests.jsonl --num 10

help: ## List the targets
	@grep -hE '^[a-z][a-z0-9-]*:.*## ' $(MAKEFILE_LIST) | sed 's/:.*## /\t/' | expand -t 18

build: ## Compile every package (a check; `make image` builds the artifact)
	go build -o /dev/null ./...

test: ## Test selected packages, reusing successful results when inputs match
	go tool gotestsum $(TEST_OPTIONS) --packages='$(TEST_PACKAGES)' -- $(TEST_FLAGS)

vet: ## Run go vet
	go vet ./...

run: config.yaml ## Run the reference app; a missing config.yaml is created from the example
	cd apps/platformkit && go run . --config ../../config.yaml

# A first run has no config.yaml, and the example is the development
# configuration `make up` matches, so the first run gets a copy of it. The rule
# has no prerequisite on purpose: a file that exists is up to date, so an edited
# config.yaml is never overwritten, whatever the example's timestamp says.
config.yaml:
	cp config.example.yaml config.yaml

e2e: ## Gate 10: boot the app on a database of its own and drive it with a browser
	./scripts/e2e.sh

load-test: ## Compare bounded tenant work and database pool capacity
	go test ./kit/jobs -run '^$$' -bench '^BenchmarkPerTenantCapacity$$' -benchtime=2s -count=3 -timeout=3m

check-loc: ## Fail when a bucket exceeds its line ceiling
	go run ./tools/locbudget --check

check-packages: ## Fail when the app links too many first-party packages
	./scripts/check_packages.sh

check-gucs: ## Fail when anything outside kit/db writes a tenancy setting
	./scripts/check_gucs.sh

fmt-check: ## Fail when any file is not gofmt'd
	@goroot="$$(go env GOROOT)" || exit $$?; \
	out="$$("$$goroot/bin/gofmt" -l .)" || exit $$?; \
	if [ -n "$$out" ]; then echo "NOT FORMATTED:"; echo "$$out"; exit 1; fi; \
	echo "gofmt clean"

# Do not share the local test target as a prerequisite: in `make test check`,
# Make would consider it complete even if that earlier run was filtered/cached.
check: build vet fmt-check check-loc check-packages check-gucs ## Everything a pull request must pass
	go mod tidy -diff
	go tool gotestsum --packages='./...' -- -count=1
	bash scripts/check_architecture_test.sh
	./scripts/check_imports.sh

fmt: ## Format every package
	go fmt ./...

image: ## Build the container image
	docker build -f deploy/Dockerfile -t platformkit:dev .

up: ## Start Postgres and NATS, and wait for both to be healthy
	docker compose up -d --wait

down: ## Stop Postgres and NATS and drop their volumes
	docker compose down -v
