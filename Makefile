# Twelve targets, no more. `make check` is what CI runs and what a pull request
# must pass; everything else is a convenience.

.DEFAULT_GOAL := help
.PHONY: help build test vet run check-loc check-packages check fmt image up down

# Tests talk to a real Postgres, as two roles: the owner runs migrations, the
# app role is subject to row-level security so the isolation tests mean
# something. `make up` starts a database that matches these defaults, on the
# same PLATFORMKIT_PG_PORT, so overriding the port once moves both.
PLATFORMKIT_PG_PORT ?= 5432
PLATFORMKIT_TEST_ADMIN_URL ?= postgres://postgres:platformkit@localhost:$(PLATFORMKIT_PG_PORT)/platformkit?sslmode=disable
PLATFORMKIT_TEST_DATABASE_URL ?= postgres://platformkit_app:platformkit@localhost:$(PLATFORMKIT_PG_PORT)/platformkit?sslmode=disable
export PLATFORMKIT_TEST_ADMIN_URL
export PLATFORMKIT_TEST_DATABASE_URL

help: ## List the targets
	@grep -hE '^[a-z][a-z-]*:.*## ' $(MAKEFILE_LIST) | sed 's/:.*## /\t/' | expand -t 18

build: ## Compile every package (a check; `make image` builds the artifact)
	go build -o /dev/null ./...

test: ## Run the tests against the compose Postgres
	go test ./...

vet: ## Run go vet
	go vet ./...

run: ## Run the reference app (exists from stage E2)
	cd apps/platformkit && go run .

check-loc: ## Fail when a bucket exceeds its line ceiling
	go run ./tools/locbudget --check

check-packages: ## Fail when the app links too many first-party packages
	./scripts/check_packages.sh

check: build vet test check-loc check-packages ## Everything a pull request must pass

fmt: ## Format every package
	go fmt ./...

image: ## Build the container image (needs apps/platformkit, stage E2)
	docker build -f deploy/Dockerfile -t platformkit:dev .

up: ## Start Postgres and NATS
	docker compose up -d

down: ## Stop Postgres and NATS and drop their volumes
	docker compose down -v
