# Fifteen targets, no more. `make check` is what CI runs and what a pull
# request must pass; everything else is a convenience.
#
# The fifteenth is `e2e`, gate 10, which arrived with the stage that gave it
# something to check. It is not a prerequisite of `check`: it needs a browser
# and it takes a minute, and a gate that a laptop cannot run is a gate people
# learn to skip. CI runs both.

.DEFAULT_GOAL := help
.PHONY: help build test vet run e2e check-loc check-packages check-gucs fmt-check check fmt image up down

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

help: ## List the targets
	@grep -hE '^[a-z][a-z0-9-]*:.*## ' $(MAKEFILE_LIST) | sed 's/:.*## /\t/' | expand -t 18

build: ## Compile every package (a check; `make image` builds the artifact)
	go build -o /dev/null ./...

test: ## Run the tests against the compose Postgres
	go test -count=1 ./...

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

check-loc: ## Fail when a bucket exceeds its line ceiling
	go run ./tools/locbudget --check

check-packages: ## Fail when the app links too many first-party packages
	./scripts/check_packages.sh

check-gucs: ## Fail when anything outside kit/db writes a tenancy setting
	./scripts/check_gucs.sh

fmt-check: ## Fail when any file is not gofmt'd
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then echo "NOT FORMATTED:"; echo "$$out"; exit 1; fi; \
	echo "gofmt clean"

# Gate 6 is a line in this recipe rather than a fifteenth target, because there
# are fourteen and the count is one of the rules. Make runs the prerequisites
# first, so it goes last; it costs milliseconds and needs nothing built, so
# where it goes does not matter.
check: build vet fmt-check test check-loc check-packages check-gucs ## Everything a pull request must pass
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
