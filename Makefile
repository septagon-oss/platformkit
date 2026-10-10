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
.PHONY: help build test vet run e2e mobile-e2e rehearse backup restore-drill load-test check check-race check-loc check-packages check-gucs check-fixtures check-versions check-e2e-guards check-pillars fmt-check check fmt image up trace down check-test-inventory check-push check-merge check-nightly flakes cover

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

# The development mail catcher compose.yaml's mailpit service publishes. The two
# ports are all this recipe says: what the application dials and what the journeys
# read are derived from them by scripts/e2e.sh, the only reader, so the name of the
# catcher exists in one file (compose.yaml) and the address is worked out in the one
# place that uses it. An operator who moves the stack moves the journeys with it.
PLATFORMKIT_MAILPIT_SMTP_PORT ?= 1025
PLATFORMKIT_MAILPIT_PORT ?= 8025
export PLATFORMKIT_MAILPIT_SMTP_PORT
export PLATFORMKIT_MAILPIT_PORT
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

# Local feedback uses Go's package/dependency cache. The full check below runs
# fresh at TEST_COUNT's default of -count=1, independently of these local
# selectors or an earlier test goal: only a caller that sets TEST_COUNT itself
# opts its own runs out of running every test.
TEST_PACKAGES ?= ./...
TEST_FLAGS ?=
# gotestsum options apply only to local feedback; --watch keeps the default
# all-package scope so Go's cache also checks consumers of an edited package.
TEST_OPTIONS ?=
# The count both full-suite goals pass to the go command. Its default is the promise CI
# depends on: -count=1, so a build cache restored across commits can never answer a test
# the commit never ran. CI sets nothing, so CI keeps this default. A caller whose own
# gate re-runs the same tree sets TEST_COUNT= empty and gets Go's test cache for the
# packages whose inputs did not change; that is the caller opting its own runs into the
# cache, never CI out of it. Local feedback (make test) still carries no count at all.
TEST_COUNT ?= -count=1
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
# emulator the check job's container does not have: the journey runs on the
# pkit-ci-android label, whose image carries an Android 35 x86_64 system image and
# the AVD the repository's PK_MOBILE_AVD names, and whose job container is started
# with the host's /dev/kvm passed through. A journey that silently skipped would
# leave the rate in e2e/maestro/flows.json looking like a number nobody earned. See
# .gitea/workflows/mobile.yml.
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

# The pillar section of ARCHITECTURE.md is prose a contributor is meant to trust, and
# it carries line citations into other people's files, so it drifts the way code does:
# a file grows, a citation that named a symbol names a blank line, and nothing fails.
# Its pin is a Python file, and `go tool gotestsum --packages='./...'` cannot see a
# Python file, which would leave the one gate that reads those citations to be run by
# whoever remembered it. python3 is already a `check` prerequisite through
# check-apidiff, so this costs a second and no new dependency.
check-pillars: ## Fail when the pillar section drifts or one of its citations lands outside its file
	python3 -B scripts/architecture_pillars_test.py

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

# The tiers decision 0088 rules, as goals beside `check` rather than as a narrowing of it.
#
# `check` stays what a pull request must pass and stays the sum of the tiers, byte for byte where
# three scripts pin it; what changes is who runs it. A push runs `check-push`: the contracts and the
# behaviour of the packages the diff reaches, and nothing that opens a database — the selector
# refuses a selection that does, so the promise is structural rather than careful. A merge (a push
# to main, which is where a pull request lands) runs `check`, which is the whole suite plus the
# rehearsal, and the nightly runs the journeys and publishes the two reports the inventory's empty
# `duration` and flake columns are waiting for.
#
# The figures, on this host at this revision: `make check` finishes 5 574 tests in 294 s of suite
# plus the build, and the forge's median `check` job on main is 2 468 s (tests/inventory.json
# summary.check_job_seconds_median, 13 samples). `make check-push` for a diff that touches one
# package runs that package's cases and the same static lines: measured below in the commit that
# wires it.
check-test-inventory: ## Re-check tests/inventory.json against the tests the tree holds
	./scripts/check_test_inventory.sh

check-push: build vet fmt-check check-loc check-packages check-gucs check-ui check-versions check-run-owner check-test-inventory ## The push tier: contract and behaviour of the packages the diff reaches, no stack
	./scripts/check_push_tier.sh

check-merge: check-rehearse check ## The merge tier: the whole suite, the rehearsal against a populated database, the journeys in the suite

check-nightly: ## The nightly tier: the journeys, and the reports the inventory's duration and flake columns read
	$(MAKE) e2e
	$(MAKE) mobile-e2e
	$(MAKE) flakes

# One selection, one runner: the list comes out of tests/inventory.json, which is the same table a
# person ratified, so a package cannot be in the push tier by one rule and out of it by another.
FLAKE_RUNS ?= 5
flakes: ## Run the whole push tier FLAKE_RUNS times and print the per-test flake rate and the slowest tests
	./scripts/check_flake_report.sh $(FLAKE_RUNS)

# The instrument 0088's acceptance line needs a "before" for: "coverage not lower" is
# uncheckable in a tree that never measures it — `-coverprofile` appears nowhere in this
# repository, and the inventory's `unique_lines` column exists only for the 36 files the
# specify round measured one at a time. This goal runs the suite once with the mode the
# delta needs (`set`, not `count`: the prune asks whether a line is reached, not how often)
# and leaves the answer in reports/ as a named path — the figure a prune commit refuses to
# fall below, read with `go tool cover -func`. It is a goal and not a `check:` line because
# instrumenting every package costs real time, and a gate that is slow for a number nobody
# reads tomorrow gets switched off.
cover: ## Record this tree's statement coverage in reports/coverage.out and coverage.txt
	mkdir -p reports
	go test -count=1 -timeout=30m -covermode=set -coverprofile=reports/coverage.out $(TEST_PACKAGES) | tee reports/coverage.txt
	go tool cover -func=reports/coverage.out | tail -1

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
# The same stated per-package bound `check` carries, for the same reason and against the same
# measurement: go test's ten-minute default is not a decision this repository made, and
# apps/platformkit has grown up to it. The package's own wall clock under -race on CI's runner, read
# out of four logs where the step passed: 438.9s (run 57363), 448.5s (57557), 462.1s (57497),
# 484.3s (57383), 489.8s (57488). Read out of four more where it did not: `panic: test timed out
# after 10m0s`, at 57644 (parked in TestASetPasswordLinkOpensOneSessionInItsOwnTenant, 5s in), 57686
# (TestTheCompositionMountsTheCatalogItsContractIsGeneratedFrom, 1s in), 57718
# (TestTheCatalogNamesWhereTheWritesOfAResourceAre, 2s in) and 57727
# (TestAnApprovedProposalDoesNotAnswerASecondVerdictOverHTTP, 3s in). Four different tests, none of
# them hung, each of them simply the one that was open when the tenth minute arrived on a box doing
# other work. A bound that the suite passes on a quiet runner and the same suite misses on a busy
# one is not a bound; it is a coin toss reported as a defect — which is why `check` already says 30m
# beside its count rather than trusting the default. Thirty minutes is also the smaller of the two
# numbers this file can name: -race roughly doubles the suite, so the race run of the package that
# needs 30m without it cannot promise less with it, and the job that runs both steps gives itself 75
# minutes with this step measured at 314-947s inside it. The count comes first and the bound after it,
# as in `check`'s own line, because scripts/ci_go_cache_test.sh reads $(TEST_COUNT) as it sits
# directly after `-race`; scripts/make_check_count_default_test.sh and scripts/check_architecture_test.sh
# hold byte copies of the line that result, in CI's expansion and with the count emptied.
check-race: ## Run the concurrency kernel under -race
	go test -race $(TEST_COUNT) -timeout=30m $(RACE_PACKAGES)

# Gate 10 never drives an application it did not start. The two cases below answer
# that question of scripts/e2e.sh and scripts/mobile_e2e.sh without a database, a
# browser or node: a stranger answers /health on the port, the run's own application
# does not, and each script's own wait_healthy — extracted from the committed file
# rather than retyped — has to refuse. They are here, and not in
# check-e2e-guards, because nothing they ask needs a service: a run that would drive
# somebody else's listener is red before a merge rather than in the job that
# guards it.
check-run-owner: ## Refuse a browser run that would drive an application it did not start
	bash scripts/e2e_health_owner_test.sh
	bash scripts/e2e_health_requires_own_listener_test.sh

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
#
# scripts/mobile_journey_fetch_test.sh runs the journey job's own download step, as the workflow
# file states it, against a server on localhost that behaves like this forge: a 200 and a sign-in
# page to an anonymous request, the build to one carrying the job's token. It refuses each way the
# journey was wrong or is one edit away from being wrong — the job not on the label that has a
# device, a second job taking that label, a fetch step not given the job's token, a job-level
# PK_MOBILE_APK competing with the step's file:// export — and then asks the harness's own curl and
# sha256sum to open what the step exported. It is here rather than only in the mobile job because
# every one of those is a fact about a workflow file, which `check` already reads for its two other
# such facts above, and because the mobile job is the worst place to learn them: the journey has no
# device when they are wrong, and 40 minutes to find that out. It needs python3, curl and sha256sum
# — and PyYAML, which the job image does not ship: measured 2026-10-05 by running this file inside
# the digest ci.yml names (gitea/runner-images:ubuntu-24.04@sha256:e77e2b1e…), where it died at
# `ModuleNotFoundError: No module named 'yaml'` and answered `ok` once that package came in on the
# line that installs the socket probe. ci.yml carries it for that reason. Both files, 0.2s.
#
# scripts/ci_go_cache_test.sh asks the same kind of question about the Go build cache T-0277 gave the
# two kernel Go jobs: which key each one restores under, and what may never be true about it. Both of
# this change's promises can be undone from somewhere else — the key by anyone who moves the digest
# into a `hashFiles` expression, which act_runner answers "" for rather than failing, and the brief's
# "never cache test results across commits" by anyone who moves the count out of TEST_COUNT's default
# and into `GOFLAGS` — so both halves are pinned where they live: the workflow, and the one variable
# the two goals that run the suite read. It starts nothing and reads no database, which is what makes it
# a `check` line and not a job step.
#
# scripts/make_check_count_default_test.sh asks the count's question of the one shape no other case can
# take: CI's own, where nothing names the variable at all. The arch probe pins `TEST_COUNT=-count=1` on
# the nested make, and a command-line value overrides whatever the Makefile assigns; ci_go_cache_test.sh
# reads the `?=` default's line. A later assignment that empties the variable below that default passes
# both (measured by review 1: the guard answers ok, the probe still prints the fresh line) while CI's own
# expansion loses the flag and cached test results start travelling across commits. So this case starts a
# child make with TEST_COUNT, MAKEFLAGS and MAKEOVERRIDES removed from the environment — what a caller
# that sets nothing inherits — and refuses an expansion that is not today's fresh suite line, the same
# for the race goal, and any workflow that sets the variable itself. Two dry runs and one grep; nothing
# is compiled, started or tested.
#
# scripts/ci_go_cache_one_saver_per_key_test.sh asks the one question about those steps that no
# single step can see: whether two jobs that run side by side save under the same exact key. A save
# runs only when its restore was not an exact hit, so a key two jobs share ends up holding whichever
# archive the cache server kept — the v2 protocol refuses the second reservation of an existing
# (key, version), the v1 one keeps the newest — and the other job restores a tree built for someone
# else, hits it exactly, never saves, and compiles cold until go.sum moves while the log reports a
# warm restore. It reads the parsed workflow and starts nothing.
#
# scripts/ci_go_cache_job_archives_test.sh asks the third question about those four steps, and the one
# no static read can answer: it runs the two `run:` commands the workflow's naming steps actually carry,
# in a temporary copy of go.mod, go.sum and the key recipe, resolves every ${{ steps.gocache.outputs… }}
# the cache inputs read against what those commands emitted, and refuses a key both jobs emit, a key
# that survives a dependency change, a first restore prefix that matches the peer job instead of this
# job's own previous archive, and a save path list that differs from its restore in content or order.
# One `go env` and four sha256sums per call, 0.19s for the whole file, and one reason the CI job's
# tool step installs a YAML reader beside the database client and the socket probe.
check: build vet fmt-check check-loc check-packages check-gucs check-ui check-versions check-run-owner check-pillars check-rehearse check-apidiff check-test-inventory ## Everything a pull request must pass
	go mod tidy -diff
	# A stated per-package bound, because go test's ten-minute default is not a decision this
	# repository ever made and the suite grew past it. apps/platformkit's 122 cases each migrate an
	# installation into a schema of their own; the package measured 407s with three database packages
	# sharing one Postgres, and the run of 2026-10-06 killed it at 600s while it was still working —
	# six passing cases then reported as `(unknown)` over a goroutine dump of tests parked in
	# t.Parallel. A whole-suite run of 2026-10-06 on a host carrying six of this program's suites at
	# once took `modules/tenant/internal` to 18m37s and `kit/rest` — a package that opens no database
	# at all — to 20m, and the same five packages that failed there passed in 7m30s of wall clock
	# against 1m of CPU when nothing else was asking. Thirty minutes is the worst observation here plus
	# room for a machine doing other things as well. It is not a looser standard: the hang a bound
	# exists to catch still stops. Behind it the CI job that runs `make check` names 75 minutes for
	# itself, and the forge has three times cut that job off at about an hour over it — 94d387cc at
	# 3612s = 60m02s, b6f1e93 at 3612s, cc614f57 at 3609s, all three with 75 in their copy of
	# .gitea/workflows/ci.yml — so the bound that stops a hung package here is this line's, not the
	# job's, and the budget a step must fit is the runner's.
	#
	# The count beside it is TEST_COUNT's (PR #141), and the two are written in this order because
	# scripts/ci_go_cache_test.sh reads the count variable as it sits directly after the option separator,
	# so the count goes first and the bound after it. A caller that empties TEST_COUNT for its own cached
	# runs then loses the count, and the line that comes back carries the bound on its own — two spaces
	# and all, because make substitutes nothing for an empty variable and leaves the separator on both
	# sides. The shell folds them; the pin in the arch probe is of the bytes the recipe emits, in the same
	# shape as the double space that probe already pins in the local goal's line. One caution for whoever
	# edits this paragraph next: that guard greps this recipe's text, so setting the separator and the
	# variable side by side anywhere in here would answer it from a comment.
	#
	# Three files hold this command, and that is the price of a byte pin. The arch probe
	# scripts/check_architecture_test.sh dry-runs `make check` and `make test` under local selectors and
	# compares the fresh goal against a byte copy of it; scripts/make_check_count_default_test.sh asks
	# for the same bytes in CI's own shape, where nothing names TEST_COUNT; and scripts/ci_go_cache_test.sh
	# refuses a recipe that stops interpolating the count at all. All three were red on 2026-10-06, the day
	# -timeout=30m joined -count=1 here and nowhere else. Re-pinning is a decision somebody makes
	# rather than a fix: those cases refuse a re-pin that drops the count, the package pattern or any
	# stated bound, so what a re-pin may change is everything around the parts that make the run fresh.
	# The alternative is a fresh gate that narrows because nobody looked at it.
	go tool gotestsum --packages='./...' -- $(TEST_COUNT) -timeout=30m
	bash scripts/check_architecture_test.sh
	bash scripts/check_budget_ratchet_test.sh
	bash scripts/ci_checkout_history_test.sh
	bash scripts/mobile_journey_fetch_test.sh
	bash scripts/ci_go_cache_test.sh
	bash scripts/make_check_count_default_test.sh
	bash scripts/ci_go_cache_one_saver_per_key_test.sh
	bash scripts/ci_go_cache_job_archives_test.sh
	# The refusal a job reaches when its toolchain cannot answer where its caches live: the recipe
	# warns, writes no key and exits 0, so both cache steps skip and the job stays green. The three
	# cases above ask that of the recipe's text and of a tree with `go`; this one runs the recipe with
	# no `go` on PATH at all, which is the shape a failed setup-go step leaves.
	bash scripts/ci_go_cache_key_without_go_test.sh
	# `scripts/ci_go_cache_test.sh` reads two blocks out of the workflow and stops reading when a block
	# ends. An `exit` there closes the pipe under the writer, and under `set -euo pipefail` the SIGPIPE comes
	# back as 141: a red `make check` with no assertion in it. This case pads both block boundaries
	# with more comment than a pipe buffer holds, so the refusal cannot depend on whether the writer
	# happened to finish first, and asks for the guard's own verdicts on both sides of the padding.
	bash scripts/ci_go_cache_guard_comments_test.sh
	# scripts/ci_container_leak_test.sh asks what the program's CI does with the containers it starts by
	# hand: whether every `docker run -d` names itself after the run id and clears that name first,
	# whether every one carries `--label pkit.ci.run=<run id>`, whether the first step of a job that
	# starts containers removes the labelled ones older than twice that job's own ceiling, and whether
	# the `if: always()` step names a handle for every container its job can create. It is here because
	# the leak it refuses was invisible to every other gate in this file: on 2026-10-06 the three ci
	# runners held thirteen SeaweedFS containers, some nineteen hours old, all of them from the one
	# `docker run -d` that carried no `--name`, and a cancelled or timed-out job runs no `if: always()`
	# step of ours at all, because act_runner is SIGKILLed (`.gitea/workflows/ci.yml`'s own header).
	# The guard reads every workflow under `.gitea/workflows`, then runs ten mutations of its own text
	# back through the same pass — the store without its label, its name or its pre-removal; a name no
	# step's env defines; a sweep bound under twice the job's ceiling, moved off the first step, or
	# narrowed to running containers; a container the always() step names no handle for; the device
	# journey's NATS unnamed again — so the acceptance's "the test fails when a step reintroduces the
	# leak" is a line of `make check`. It starts no container and needs PyYAML, which ci.yml's own apt
	# step installs.
	bash scripts/ci_container_leak_test.sh
	# scripts/ci_container_sweep_age_test.sh runs the sweep instead of reading it. The guard above asks
	# three things of the sweep step's text — that it lists `docker ps -aq --filter label=pkit.ci.run`,
	# that it says `docker rm -f`, that its SWEEP_OLDER_THAN is at least twice this job's own ceiling —
	# and says nothing about the one line that compares a container's age to that bound, because all it
	# does is read. Turn `[ "$epoch" -le "$cutoff" ] || continue` round, or delete it, and every answer
	# above stays `ok` (measured at this commit: rc 0 on both mutants) while the step removes the
	# containers of the live jobs sharing the daemon — another repository's included, since `pkit.ci.run`
	# is a program-wide namespace and the sweep reaches whatever carries it. Raise the bound to 99999 and
	# that read is still `ok` while the sweep removes nothing at all. This file executes each job's
	# committed `run:` under `bash -e` against a stub `docker` that lists one container created four
	# hours ago and one created five minutes ago, and refuses `removed ['young']`, `removed ['old',
	# 'young']` and `removed nothing` alike, so the bound is pinned as behaviour and not as text. It
	# starts no container and needs PyYAML, as its neighbour above does.
	bash scripts/ci_container_sweep_age_test.sh
	# scripts/ci_container_sweep_age_guard_test.sh asks whether the file above would notice. It runs the
	# honest sweep and nothing else, so the one thing that can undo it — the sweep's own age comparison —
	# is covered by no assertion until somebody mutates it: turn that comparison's `-le` to `-ge`, delete
	# the line, or widen `SWEEP_OLDER_THAN` to 99999, and a guard that only ever sees correct workflows
	# stays green while the step removes the young containers of live jobs, or nothing at all. This file
	# writes those three workflows into a temporary directory and points the guard at the copy, so the pin
	# is the guard's refusals (`removed ['young']`, `removed ['old', 'young']`, `removed nothing`) and not
	# prose about them, with the committed text kept as the accepted shape. Substitution is on raw text
	# and the guard does the parsing, so this line needs no parser of its own.
	bash scripts/ci_container_sweep_age_guard_test.sh
	# The one `make check` line that reads an npm lock. It asks that the two packages the design job's
	# `npm audit --omit=dev --audit-level=high` refused `0bfae63` for (source-map-js, dompurify) are
	# locked above the ranges that report names, and that the gate that named them is still a step in
	# the job that reads this lock, at that level and in that directory. It reads no feed and installs
	# nothing: the feed moved under the runner, so the lock is the only half of this a tree can answer.
	bash scripts/openpencil_lock_above_advisory_test.sh
	# The Go half of that line. `govulncheck` is this repository's only Go security gate, it runs in the
	# check job *after* `make check` and it needs the network, so nothing in the local aggregate can see the
	# advisory feed move: forge run 55758 refused `check / govulncheck` on `golang.org/x/net@v0.59.0` and on
	# the go1.27.1 standard library over a tree whose `make check` had come back green a few minutes earlier,
	# and forge run 54902 reports the same step `success` on the same two versions two days before it. This
	# case asks the one question a tree can answer about a feed it does not hold — which side of a published
	# range the `toolchain` line and the x/net pin land on — and mutates both back inside them, so the
	# acceptance is a comparison and not a print. It reads no feed and reaches no network.
	bash scripts/go_mod_above_advisory_test.sh
	# That guard's own inputs, asked of a tree it did not write. Every mutation the shell makes starts
	# from the committed go.mod, so three branches of it live unless a synthetic tree reaches them: the
	# directory argument that names such a tree, the rule that a commented-out dependency is not a pin
	# (`\t// golang.org/x/net v0.60.0` answers `is absent, so nothing here resolves above the range`),
	# and a release *superseding* a fix — the committed pins sit exactly at v0.60.0 and go1.27.2, so
	# "strictly above" is never asked of them. This case writes five go.mod files into a temporary
	# directory and asks the guard about each through its subprocess, and asserts exit codes rather than
	# sentences, so a floor comparison that passes for the wrong reason — an absent pin read as a
	# satisfied range — cannot answer 0. It reaches no network and builds nothing.
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/go_mod_advisory_inputs_test.py
	bash scripts/check_pin_rehearsal_test.sh
	# Where a run finds the mail catcher. The journeys that open a mailed link are
	# the only proof the address the application dials is right, and they cannot say
	# which of the two addresses in this repository they were pointed at: a caller
	# that runs one spec directly used to dial compose.yaml's default port while the
	# catcher it had been given answered on another one, and the journey blamed
	# Playwright. This case asks the script itself, in under a second, with no stack.
	bash scripts/e2e_mail_address_test.sh
	bash scripts/free_port_test.sh
	# Which failures of the API gate's one network step are worth another try. The target
	# itself is the last prerequisite; this case is the bound on its retry, so it runs with
	# the rest of the script cases and needs no stack, no proxy and no tree but this one.
	bash scripts/check_public_api_fetch_test.sh
	# The other half of that bound: a fetch that fails twice and then succeeds must still be
	# judged on what it exported. These cases drive the gate's own main() with the tool's
	# subprocess calls answered from memory, so they need no network and no second tree.
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/check_public_api_retry_test.py
	bash scripts/e2e_guards_run_before_the_gate_test.sh
	# scripts/test_inventory.py feeds two gates — `check-test-inventory` above and the push tier's
	# selection in scripts/check_push_tier.sh — and until review 1 neither could fail, because the
	# tool returned its refusal and the script discarded it. The six cases below are what keeps that
	# shape now: scripts/test_inventory_test.sh pins the eight refusals and the exit status of each,
	# against a fixture tree in a temporary directory; scripts/a_refused_inventory_check_exits_nonzero_
	# test.sh asks the status of a removed row and of a push selection that opens a stack;
	# scripts/push_tier_reaches_the_consumers_of_a_changed_package_test.sh asks that a change to a
	# package selects the package that imports it — a selector that keyed `go list`'s absolute
	# directories against the diff's relative ones matched nothing, so no push ever ran a consumer;
	# scripts/push_tier_reaches_test_imports_test.sh asks the same of a consumer that exists only
	# in a `*_test.go` file, internal and external package alike — `.Deps` alone names no such edge,
	# so a package with no test of its own could change, break the test that imports it, and leave the
	# selector answering an empty list a push job read as "nothing to run";
	# scripts/push_tier_reaches_the_package_whose_testdata_changed_test.sh asks it of a change that is
	# not a Go file at all — a golden under `testdata/` and a catalogue under `go:embed` are read by
	# the package's own test binary, and a selector that resolved only the directory `git diff` named
	# ran no case for either, so the pull request went green and the push to main was the first red;
	# and scripts/the_inventory_lists_a_javascript_test_test.sh asks the table for the shape of test no
	# Go walk sees — a `node --test` case the editor job runs — whose round-named sibling otherwise
	# counts against no ceiling and whose absence from the table is the drift 0088 exists to refuse.
	# None of the six opens a stack; together they cost a few seconds.
	bash scripts/test_inventory_test.sh
	bash scripts/a_refused_inventory_check_exits_nonzero_test.sh
	bash scripts/push_tier_reaches_the_consumers_of_a_changed_package_test.sh
	bash scripts/push_tier_reaches_test_imports_test.sh
	bash scripts/push_tier_reaches_the_package_whose_testdata_changed_test.sh
	bash scripts/the_inventory_lists_a_javascript_test_test.sh
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

up: ## Start Postgres, NATS, Valkey, the object store and the mail catcher, and wait for all five to be healthy
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
down: ## Stop Postgres, NATS, Valkey, the object store, the mail catcher and the collector, and drop their volumes
	docker compose --profile telemetry down -v
