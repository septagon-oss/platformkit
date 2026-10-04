# make check is the required source gate; browser and capacity checks have
# separate targets because they need their own runtime fixtures.

.DEFAULT_GOAL := help

# Select the same compiler and tools even when PATH contains a different Go
# release. Child scripts and their Go subprocesses inherit this exact version.
# It is go.mod's `toolchain` line rather than its `go` line: `go` is the floor a
# consumer of this module has to clear, `toolchain` is the version this repository
# is built and gated with, and it is the line the go command and actions/setup-go
# (go-version-file) already read as that. They differ because a gate needs a
# compiler that can identify the checkout it is building, and the `.git` of a git
# worktree is a file — one go1.27 reads (go.dev/issue/58218) and go1.26 walks past,
# asking git about the first parent directory with a .git of its own instead, which
# stamps another repository's revision into the binary or fails the build outright.
export GOTOOLCHAIN := $(shell sed -n 's/^toolchain //p' go.mod)
.PHONY: help build test vet run e2e mobile-e2e rehearse backup restore-drill load-test check check-race check-loc check-packages check-gucs check-fixtures check-versions check-e2e-guards fmt-check check fmt image up trace down

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
# modules/file's S3 adapter is tested against the S3-compatible store `make up`
# starts, and fails rather than skips without it for the same reason. Its port is
# not part of the allocated pair the test URLs are built from: nothing else in this
# stack answers on 8333, so the default needs no allocation, and an operator who
# runs two stacks side by side overrides it the way they override the other two.
PLATFORMKIT_S3_PORT ?= 8333
PLATFORMKIT_TEST_S3_ENDPOINT ?= localhost:$(PLATFORMKIT_S3_PORT)
PLATFORMKIT_TEST_S3_BUCKET_PREFIX ?= platformkit-test
PLATFORMKIT_TEST_S3_ACCESS_KEY ?= pkittest
PLATFORMKIT_TEST_S3_SECRET_KEY ?= pkittestsecret
export PLATFORMKIT_TEST_ADMIN_URL
export PLATFORMKIT_TEST_DATABASE_URL
export PLATFORMKIT_TEST_NATS_URL
export PLATFORMKIT_TEST_S3_ENDPOINT
export PLATFORMKIT_TEST_S3_BUCKET_PREFIX
export PLATFORMKIT_TEST_S3_ACCESS_KEY
export PLATFORMKIT_TEST_S3_SECRET_KEY

# The shared store kit/cache's Valkey adapter speaks. `make up` starts it on this
# port, and the suite reads the address below.
#
# Unlike PLATFORMKIT_TEST_NATS_URL, this one is exported only when something answers
# there. The difference is the two kernels' own, not a weaker standard: the worker
# transport every journey exercises has to be present for its suite to mean anything,
# while kit/cache boots without a store at all — the in-process adapter is a complete
# deployment for one process — so a checkout with no cache server runs every case in
# this repository but the three that name one, and its skip line says which. Whoever
# exports this variable at a store that is not answering gets those three cases as
# failures rather than a skip: an address you set yourself is a promise.
PLATFORMKIT_VALKEY_PORT ?= 6379
PLATFORMKIT_TEST_VALKEY_URL ?= $(shell timeout 2 bash -c 'exec 3<>/dev/tcp/localhost/$(PLATFORMKIT_VALKEY_PORT)' 2>/dev/null && echo redis://localhost:$(PLATFORMKIT_VALKEY_PORT))
export PLATFORMKIT_TEST_VALKEY_URL

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

# The commit Go stamps into a binary is read: it is what the admin shell's footer
# renders (modules/admin/internal/mount.go's version()), so `run`, `e2e`, `rehearse`
# and `image` are left to stamp theirs. This goal keeps no binary — the linker
# writes to /dev/null — so the stamp it would compute is a field nothing reads, and
# computing it would make whether this repository compiles depend on whether git can
# answer for the directory the source sits in. Nothing in this file assigns GOFLAGS.
# A source tree outside every repository — an unpacked archive — needs no flag for
# that reason either: with no repository above it there is nothing for Go to ask, and
# it stamps nothing. The tree that needs the flag is one with no repository of its
# own sitting under a directory whose `.git` git refuses to read — a home directory
# holding an unreadable `.git`, say. Stamping then asks about that parent instead,
# and the build dies with `error obtaining VCS status: exit status 128`. GOFLAGS
# reaches a goal's `go` subprocess from the caller, so such a tree builds with the
# flag its caller passes: `GOFLAGS=-buildvcs=false make run`. Only the four goals
# above keep a binary, so only they can be without a revision.
build: ## Compile every package (a check; `make image` builds the artifact)
	go build -buildvcs=false -o /dev/null ./...

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

# The device journey is its own goal, and its own CI job, because it needs an
# emulator the check job's container does not have: the host the pkit-ci runners use
# has one (/dev/kvm, an x86_64 system image and an AVD are measured there), and a
# journey that silently skipped would leave the rate in e2e/maestro/flows.json
# looking like a number nobody earned. See .gitea/workflows/mobile.yml.
mobile-e2e: ## Boot the app on a database of its own and drive it with one device flow
	./scripts/mobile_e2e.sh

# A release step, not a pull-request gate: it needs psql, pg_dump and pg_restore, a
# database it may create and drop, and the previous release's revision to copy from.
# What it answers is the question no other goal here can — what this release's
# migrations cost against a table the size the installation actually has — and it is
# the step ADR 0011 requires before a version is published. REHEARSE_ARGS="--base-ref
# <ref>" (or --dump <file>); scripts/rehearse_migrations.sh names the four exit codes.
rehearse: ## Apply this tree's pending migrations to a copy of a production-shaped database
	./scripts/rehearse_migrations.sh $(REHEARSE_ARGS)

# The other half of "can this installation be put back". `make backup` writes one
# dump — with the grants on its tables, so the restore can be opened as the
# application and not only as its owner — plus a copy of the on-disk byte store and
# a manifest of digests; `make restore-drill` puts a backup (or a fresh one) into a
# scratch database, compares every object with the installation's own store and
# every table both as the owner and through the application's role, and prints
# restore_drill_pass_ratio. Operator steps in the same class as rehearse — psql,
# pg_dump, pg_restore and a database it may create and drop — and for the same
# reason they are not in `check`: `check` is what a pull request must pass on a
# source tree, and these need a running cluster. A backup is one database, never one
# tenant: the rows and the bytes of every tenant live together, so `TENANT` is
# refused rather than silently dropped.
PLATFORMKIT_FILES_DIR ?= data/files
# Passed only when the directory is there: a deployment on an object store has no
# byte store on this disk to carry or compare, and one whose files.dir points
# somewhere else overrides by putting its own --files in BACKUP_ARGS or DRILL_ARGS.
STORE_ARGS = $(if $(wildcard $(PLATFORMKIT_FILES_DIR)),--files $(PLATFORMKIT_FILES_DIR))
refuse-tenant = @if [ -n "$$TENANT" ]; then echo "$(1): one backup is one database, not one tenant — TENANT is not read; point --url at the database you mean" >&2; exit 2; fi

backup: ## Write one dump of the database and a copy of the byte store, with a manifest of digests
	$(call refuse-tenant,backup)
	./scripts/backup.sh $(STORE_ARGS) $(BACKUP_ARGS)

restore-drill: ## Put a backup back and prove the bytes and the application's read of the tables came back
	$(call refuse-tenant,restore drill)
	./scripts/restore_drill.sh $(STORE_ARGS) $(DRILL_ARGS)

load-test: ## Compare bounded tenant work and database pool capacity
	go test ./kit/jobs -run '^$$' -bench '^BenchmarkPerTenantCapacity$$' -benchtime=2s -count=3 -timeout=3m

check-loc: ## Fail when a bucket exceeds its line ceiling
	go run ./tools/locbudget --check

check-packages: ## Fail when the app links too many first-party packages
	./scripts/check_packages.sh

check-gucs: ## Fail when anything outside kit/db writes a tenancy setting
	./scripts/check_gucs.sh

check-ui: ## Fail when a layer's markup reaches past the tokens or emits a class it never styles
	./scripts/check_ui_layers.sh

# Each design test embeds a real Go program and builds it before anything is
# observed, so those programs are consumers of kit/httpx and ui/screens that live in
# no Go package: a kernel change that renames a field they compose breaks the design
# suite rather than the compiler. This compiles every one of them — no browser, no
# database, ten seconds — with the tooling's own runner. It stays out of `check`
# because it needs the tooling's own `npm ci`, which a Go-only loop should not have
# to pay for; CI runs that same file as a step of its own, before its browser steps.
check-fixtures: ## Compile the Go program every design test embeds
	cd tools/designexport/openpencil && node --import ./register.mjs --test fixtures-compile.test.mjs

check-versions: ## Fail when go.mod replaces a dependency or a go.work file is present
	./scripts/check_versions.sh

# The rehearsal and the public-API comparison, both of which existed as steps
# nobody was forced to run: `make rehearse` needed an operator who remembered it,
# and the apidiff workflow said "deliberately not a required check" and ran on a
# Sunday. Both answer a question no other gate here can — what this release's
# migrations cost on a table the size the installation actually has, and which
# exported name a pinned consumer compiled against last time — and a question
# answered only after a version is published is a review of the damage.
#
# The base is v1.1.0 for both, because that is the release consumers are on and
# it is the tag scripts/PUBLIC-API.md already names. The seed is the fixture the
# rehearsal script documents, because a migration measured against an empty table
# is a migration measured against nothing.
#
# The cost is measured, not assumed: against a copy of the v1.1.0 ledger seeded
# with 10,000 rows per table, the rehearsal of this tree's nine pending files ran
# in 19 s of wall clock (commit b7a0354's Verified: line names the run), and
# check-apidiff takes about a minute. Both sit inside the 75-minute ceiling
# 8a3297a set for the check job. When that ceiling gets tight, T-0219 splits the
# job; nothing is removed from `check` to fit it.
REHEARSE_BASE ?= v1.1.0
REHEARSE_ARGS ?= --base-ref $(REHEARSE_BASE) --seed scripts/testdata/rehearse/seed.sql
APIDIFF_BASELINE ?= scripts/baselines/public-api-$(REHEARSE_BASE).json

check-rehearse: ## Apply this tree's pending migrations to a copy of the previous release's database
	REHEARSE_RECEIPT="$${REHEARSE_RECEIPT:-$$(mktemp)}" ./scripts/rehearse_migrations.sh $(REHEARSE_ARGS)

# The baseline is the reviewed set of incompatibilities the release already owed
# at the merge base of this change; `--baseline` then refuses a *new* one and a
# vanished one, so a documented breaking line cannot hide an accidental one.
# Regenerating it is its own deliberate act: --write-baseline at the base commit,
# committed alone, with the diff of the list as the review.
check-apidiff: ## Fail on an exported API change beyond the reviewed baseline
	python3 scripts/check_public_api.py $(REHEARSE_BASE) HEAD --baseline $(APIDIFF_BASELINE)

fmt-check: ## Fail when any file is not gofmt'd
	@goroot="$$(go env GOROOT)" || exit $$?; \
	out="$$("$$goroot/bin/gofmt" -l .)" || exit $$?; \
	if [ -n "$$out" ]; then echo "NOT FORMATTED:"; echo "$$out"; exit 1; fi; \
	echo "gofmt clean"

# Do not share the local test target as a prerequisite: in `make test check`,
# Make would consider it complete even if that earlier run was filtered/cached.
# The concurrency kernel under the race detector: the outbox claims and the
# relay, the request's lazy transaction, the advisory locks, the limit counters,
# the router's per-request state. Five package guides tell a contributor to run
# `go test -race` by hand; a claim nobody runs is not a gate, so this is the
# target that runs them and CI calls it as a step of its own. It is separate
# from `check` rather than inside it because -race roughly doubles the suite and a
# local loop should not pay that to find out whether one file compiles.
#
# The two module packages are in the standing list rather than in one change's
# override, because the advisory lock over a tenant's administration is shipped
# code, not a branch: modules/auth takes it before the read that decides and
# modules/user takes it after the subject's row lock, in the opposite order, and a
# lock held past — or released before — the commit that needed it is invisible to
# every other gate in this file. apps/platformkit is here because that is where the
# two halves are composed and where the only test that can watch a write in one
# module queue behind a write in the other lives.
# RACE_PACKAGES overrides the list when a change reaches somewhere else.
# modules/change is in the standing list rather than one change's override because
# the whole point of the object is a lock: the proposal is taken FOR UPDATE before
# the subject is, and two applies of two proposals over one subject settle by that
# order. An author who cannot run the two commands concurrently cannot tell a lock
# that works from a lock that is merely written down.
RACE_PACKAGES ?= ./kit/events/... ./kit/db/... ./kit/limit ./kit/jobs ./kit/httpx \
	./modules/auth/internal/... ./modules/user/internal/... ./modules/admin/... \
	./modules/change/... ./apps/platformkit
check-race: ## Run the concurrency kernel under -race
	go test -race -count=1 $(RACE_PACKAGES)

# Gate 10's own port choice runs inside `check` rather than only inside gate 10, because the three
# promises it makes — the port it refuses, the listener it is willing to serve through, and the port
# it hands Playwright — otherwise fail as a wrong number in a browser run rather than as a red gate
# here. See scripts/free_port.sh for why the answer is no longer the literal 8099 and why a 200 on
# /health is not enough to answer it, and scripts/free_port_test.sh for the sixteen cases.
#
# scripts/e2e_guards_run_before_the_gate_test.sh asks a question about check-e2e-guards rather than
# about gate 10: whether the two port refusals below are run at all, by the goal and by CI, in the
# one window of the job where node and a browser exist. It starts nothing — it reads `make -n
# check-e2e-guards` and .gitea/workflows/ci.yml — so unlike its two subjects it needs no node and
# belongs here, where dropping them from the goal is red before a merge rather than in the job that
# dropped them.
#
# scripts/ci_checkout_history_test.sh asks which history each CI job fetches and which of its steps
# reads it. It is here because the answer was wrong in the direction that costs a delivery: at
# b6f1e93 the design job spent all 45 of its minutes inside actions/checkout's full-history fetch —
# every branch and every tag, for a job whose steps read no git object — and the forge refused the
# head with `failed step: Run actions/checkout@…` and never ran the suite. The case reads the
# workflows and the tree, starts nothing, and refuses either half of the mistake: a job left fetching
# history nothing reads, and a job narrowed while a step still walks `base..HEAD`.
check: build vet fmt-check check-loc check-packages check-gucs check-ui check-versions check-rehearse check-apidiff ## Everything a pull request must pass
	go mod tidy -diff
	go tool gotestsum --packages='./...' -- -count=1
	bash scripts/check_architecture_test.sh
	bash scripts/check_budget_ratchet_test.sh
	bash scripts/ci_checkout_history_test.sh
	bash scripts/check_pin_rehearsal_test.sh
	bash scripts/free_port_test.sh
	bash scripts/e2e_guards_run_before_the_gate_test.sh
	./scripts/check_imports.sh

# Gate 10's two refusals to drive somebody else's listener, pinned as shell cases rather than as
# prose, because each one can only be shown by starting scripts/e2e.sh against a port somebody else
# is holding: a foreign listener that takes the port mid-build (scripts/e2e_port_taken_during_build_
# test.sh) and a socket table that answers every question about ownership with silence
# (scripts/e2e_unattributed_listener_test.sh). Both answer the question `free_port_test.sh` cannot:
# whether gate 10, when it is really run, refuses.
#
# They are NOT in `check`, and that is a measured reason and not an omission. Each one needs node —
# e2e.sh stops at its node check before it prints anything either case greps for, and an absent node
# would read as a broken refusal. CI installs node two steps after `make check` (`.gitea/workflows/
# ci.yml`: setup-node, then the browser, then `make e2e`), so a wiring into `check` would red that
# job for a reason that has nothing to do with the change under review. They run in the e2e job, next
# to the gate they guard, where node, the browser and Postgres all exist; 17s for both there.
check-e2e-guards: ## Run gate 10's two port-refusal pins (needs node, a browser and the test database)
	bash scripts/e2e_unattributed_listener_test.sh
	bash scripts/e2e_port_taken_during_build_test.sh

fmt: ## Format every package
	go fmt ./...

image: ## Build the container image
	docker build -f deploy/Dockerfile -t platformkit:dev .

up: ## Start Postgres, NATS, Valkey and the object store, and wait for all four to be healthy
	docker compose up -d --wait

# The collector is a profile rather than a second service in `up` because the
# two goals that must never fail — `make test` and `make check` — sit on `up`,
# and a third container is a third thing that can fail: an image pull that
# cannot be served offline, a port that is taken. A developer reads spans, not
# test results, so the wait is behind its own goal.
#
# The port follows PLATFORMKIT_PG_PORT and PLATFORMKIT_NATS_PORT: one variable
# moves the container's mapping and the two messages below, and a machine with
# 4317 taken overrides it once.
#
# The wait watches the container and not the port, and that too is measured: the
# published port is answered by docker-proxy, which accepts a connection whether
# or not the process behind it is alive, so a dial reported "up" for a collector
# that had already died of its configuration — and `--wait` reported "Healthy" for
# the same dying process, because the scratch image declares no healthcheck and has
# no shell to probe with. Status running with no restart recorded, held three
# seconds, is the readiness there is. `up` is given --force-recreate because
# Compose compares the Compose file and not the file a bind mount points at: with
# the flag left out it reported the old container "Running" a minute after that
# container's configuration had been edited under it.
PLATFORMKIT_OTLP_PORT ?= 4317
trace: ## Start the local OTLP collector that prints every span it receives
	docker compose --profile telemetry up -d --force-recreate collector
	@timeout 60 bash -c 'c=$$(docker compose --profile telemetry ps -q collector); \
	  good=0; \
	  while [ $$good -lt 3 ]; do \
	    if [ "$$(docker inspect -f "{{.State.Status}} {{.RestartCount}}" $$c)" != "running 0" ]; then exit 1; fi; \
	    good=$$((good + 1)); sleep 1; \
	  done' \
	  || { echo "collector is not up at 127.0.0.1:$(PLATFORMKIT_OTLP_PORT); its own log:"; docker compose --profile telemetry logs --tail 20 collector; exit 1; }
	@echo "collector up on 127.0.0.1:$(PLATFORMKIT_OTLP_PORT): point telemetry.otlp_endpoint at that host:port and read spans with 'docker compose logs -f collector'"

# The profile is named here as well, because `down` is the file's teardown: a
# goal that stopped two containers and left a third holding port 4317 would half
# finish the one job it has. It stays destructive of volumes, as before.
down: ## Stop Postgres, NATS, Valkey, the object store and the collector, and drop their volumes
	docker compose --profile telemetry down -v
