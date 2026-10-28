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
.PHONY: help build test vet run seed e2e mobile-e2e rehearse backup restore-drill load-test check check-race check-loc check-complexity generate check-generated check-packages check-gucs check-comments check-fixtures check-richtext-bundle richtext-bundle check-e2e-guards check-pillars check-versions check-applied-migrations check-support-record check-rehearse-supported fmt-check check fmt image up trace down check-test-inventory check-push check-merge check-nightly flakes cover inference-conformance

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

# The secret service kit/secrets' OpenBao adapter speaks, and the token its dev mode
# answers to. Like PLATFORMKIT_TEST_VALKEY_URL it is exported only when the service is
# there: kit/secrets boots and runs every case without a server, so a checkout with no
# OpenBao running takes the named skip rather than a failure, and whoever exports this
# variable at a store that is not answering gets failures instead — an address you set
# yourself is a promise.
#
# The question asked of the port is OpenBao's own `sys/seal-status`, and not whether
# anything answers: this stack's ports come from whoever starts it, two services can
# be handed the same one, and a suite dialled at whatever else happens to be published
# there reads a mail catcher's 404 as a broken adapter. The space after the colon is
# what a pretty-printing client writes and nothing writes the other way, so the
# pattern admits both.
PLATFORMKIT_OPENBAO_PORT ?= 8200
PLATFORMKIT_TEST_OPENBAO_URL ?= $(shell timeout 3 curl -fs http://localhost:$(PLATFORMKIT_OPENBAO_PORT)/v1/sys/seal-status 2>/dev/null | grep -q '"initialized": *true' && echo http://localhost:$(PLATFORMKIT_OPENBAO_PORT))
export PLATFORMKIT_TEST_OPENBAO_URL
export PLATFORMKIT_TEST_OPENBAO_TOKEN ?= dev-only-unseal-token

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

# The same stated per-package bound `check` and `check-race` carry, for the same reason against the
# same measurement: go test's ten-minute default is a clock nobody in this repository chose, and
# `apps/platformkit` needs most of it even uncached and unraced. Measured on this program's shared
# Postgres: 9m51s for the package on a host doing nothing else — nine seconds of headroom on a
# default the suite does not beat so much as tie — and `panic: test timed out after 10m0s` on one
# doing something, with the cases that were merely running when the clock rang reported as failures.
# A goal whose verdict is a race against a clock it never names answers to the load on the box, and
# every caller of this one is a person or a gate waiting for an answer rather than a coin toss. `check`'s bound is the
# number, not a new one; it sits before $(TEST_FLAGS) so a caller that passes its own -timeout wins,
# and scripts/check_architecture_test.sh pins this line byte for byte beside `check`'s own.
# Four package processes share the test server; dbtest bounds each owner pool.
test: ## Test selected packages, reusing successful results when inputs match
	go tool gotestsum $(TEST_OPTIONS) --packages='$(TEST_PACKAGES)' -- -timeout=30m -p=4 $(TEST_FLAGS)

vet: ## Run go vet
	go vet ./...

run: config.yaml ## Run the reference app; a missing config.yaml is created from the example
	cd apps/platformkit && PLATFORMKIT_MAIL_HOST=127.0.0.1 PLATFORMKIT_MAIL_PORT=$(PLATFORMKIT_MAILPIT_SMTP_PORT) \
	  PLATFORMKIT_MAIL_FROM=platformkit@localhost go run . --config ../../config.yaml

# A first run has no config.yaml, and the example is the development
# configuration `make up` matches, so the first run gets a copy of it. The rule
# has no prerequisite on purpose: a file that exists is up to date, so an edited
# config.yaml is never overwritten, whatever the example's timestamp says.
config.yaml:
	cp config.example.yaml config.yaml

# The reference application's own seed, applied through the write path each
# resource already has: a page through content's, a person through the user
# module's invitation. Two names are enough to say which run this is — the tenant
# by its slug and the person by their address, whose roles answer for every write.
# Whether the demo records are in scope is the tenant's own row's answer, not
# DEMO's: seed refuses for a tenant whose row says false. DRY=1 prints the plan and
# writes nothing; the plan it prints is the one a real run would have decided.
SEED_TENANT ?=
SEED_AS ?=
seed: config.yaml ## Seed one tenant from this tree's seed files; SEED_TENANT=<slug> SEED_AS=<email> [DEMO=1] [DRY=1]
	@test -n '$(SEED_TENANT)' -a -n '$(SEED_AS)' || { echo 'usage: make seed SEED_TENANT=<slug> SEED_AS=<email> [DEMO=1] [DRY=1]' >&2; exit 2; }
	cd apps/platformkit && go run . seed --config ../../config.yaml --tenant '$(SEED_TENANT)' --as '$(SEED_AS)' $(if $(DEMO),--demo,) $(if $(DRY),--dry-run,)

.PHONY: check-screen-render
check-screen-render: ## Validate screen anchor geometry and permission projection
	./scripts/e2e.sh screen-anchors.spec.ts

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

# A bucket may grow since the merge base with origin/main by what this branch's own build(budget)
# commits declare, and no more; a ceiling main's own growth made stale is not this branch's to
# restate (CONTRIBUTING.md, "Keep budgets honest"). Fetch origin/main first: a stale one is an
# older merge base.
check-loc: ## Fail when a bucket grew beyond what this branch's build(budget) commits declare
	bash scripts/check_budget_ratchet.sh --merge-base origin/main

COMPLEXITY_BASE ?= origin/main
check-complexity: ## Require exact complexity ceilings and reviewed budget transitions
	go run ./tools/complexity --check --merge-base "$(COMPLEXITY_BASE)"

# Every writer whose output is GENERATED, in a fixed order; each runs offline and reads no clock.
# A golden (OpenAPI, AsyncAPI, catalogue and token fixtures) keeps its own -update path and is
# never written here (CONTRIBUTING.md, "Generated and golden files").
# The baseline the published screen artifact is held to, so a view or anchor the
# line already published cannot be retargeted by the next `make generate`. A
# repository that carries no origin/main — a copy of this tree committed into a
# throwaway repository, which is where `make check`'s own generated-file cases run —
# is read against this commit's artifact: that file is what a product already
# reaches, and a write that retargets a name held in it is refused there too.
SCREENS_BASE ?= $(shell git rev-parse --verify --quiet origin/main >/dev/null && echo origin/main || echo HEAD)
generate: ## Rewrite every generated file and section from its inputs, offline
	go run ./apps/platformkit composition -config apps/platformkit/testdata/composition.yaml -dir apps/platformkit
	go run ./apps/platformkit screens -config apps/platformkit/testdata/composition.yaml -base $(SCREENS_BASE)
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/inventory_figures.py

# The generated files are the ones .gitattributes marks linguist-generated, and tests/INVENTORY.md
# holds generated sections. The diff is against the index, so a regenerated file the author has
# staged passes and unrelated work is not judged. A writer whose output carries neither marker sits
# outside this diff: marking what a writer writes is part of writing it, and the whole-tree form,
# `make generate && git diff --exit-code`, is what catches a marker that was never added.
check-generated: generate ## Fail when a generated file or section differs from what `make generate` writes
	git diff --exit-code -- $$(git ls-files | git check-attr --stdin linguist-generated | awk -F': ' '$$3 == "true" { print $$1 }') tests/INVENTORY.md

check-packages: ## Fail when the app links too many first-party packages
	./scripts/check_packages.sh

check-gucs: ## Fail when anything outside kit/db writes a tenancy setting
	./scripts/check_gucs.sh

check-comments: ## Fail when a production comment cites a review, a round, a finding, a task id or a test file
	./scripts/check_comments.sh

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

# The rich text editor's vendor bundle: Tiptap and its Markdown extension, built
# to one minified ES module by the node project under tools/richtext-bundle. The
# ceiling is 150 KiB of gzip — the bytes every page that carries a rich text
# field adds to the download — counted in bytes against a round number, the way
# the other ceilings here are counted, and asserted whether or not this machine
# has a node toolchain. The rebuild comparison needs one, so it is the second
# arm of the same step rather than a step of its own.
RICHTEXT_BUNDLE_CEILING ?= 153600

check-richtext-bundle: ## Fail when the vendored editor bundle is over its gzip ceiling or differs from a fresh build
	bash scripts/richtext_bundle_check.sh $(RICHTEXT_BUNDLE_CEILING)

# Rebuild the bundle and its licence file from what package-lock.json pins.
# Run it after changing the extension list or a pinned version, and commit the
# two files together with the manifest and the lock.
richtext-bundle: ## Rebuild ui/assets/richtext/editor.js from its lockfile
	bash scripts/richtext_bundle_build.sh

# The number decision 0075's i18n row reads: the share of the words a person reads on
# a served page that a catalogue reached. It renders every document the reference
# composition serves without a row or a query in front of it, in the pseudo-locale,
# prints the ratio, the list of what went around a catalogue and the documents it could
# not reach with the reason for each, and refuses a fall below
# testdata/i18n-coverage.json — which is both the floor and the set of pages measured. -v is what prints: `check`'s suite runs
# through gotestsum, whose format drops the output of a passing test, so a number that
# is only a log line on a green run is a number nobody reads. The gate therefore runs
# twice — once here, printed, and once inside the suite, silent — which is what keeps
# `check`'s fresh test boundary the whole suite with nothing taken off it, the boundary
# scripts/check_architecture_test.sh pins.
check-i18n: ## Print the share of words a person reads that a translator can reach, and fail below the floor
	go test ./apps/platformkit -run '^TestThePseudoLocaleGate$$' -count=1 -v

check-versions: ## Fail when go.mod replaces a dependency or a go.work file is present
	./scripts/check_versions.sh

# An applied migration is checksummed by every installation that ran it, so an edit to one is a boot
# failure there. CI passes the commit it compares with, judged from its merge base with HEAD; empty
# means the merge base with origin/main. Uncommitted and staged edits are judged too, as raw bytes.
MIGRATIONS_BASE ?=
check-applied-migrations: ## Fail when a migration file the base carries was edited, renamed or removed
	./scripts/check_applied_migrations.sh $(MIGRATIONS_BASE)

# The pillar section of ARCHITECTURE.md is prose a contributor is meant to trust, and
# it carries line citations into other people's files, so it drifts the way code does:
# a file grows, a citation that named a symbol names a blank line, and nothing fails.
# Its pin is a Python file, and `go tool gotestsum --packages='./...'` cannot see a
# Python file, which would leave the one gate that reads those citations to be run by
# whoever remembered it. python3 is already a `check` prerequisite through
# check-apidiff, so this costs a second and no new dependency.
# The implementation map's own goal. The pillar test reads the pillar section's citations and
# asks only that a cited number sit inside the file it names; the composition sentences are held
# to the call they name by a case in kit/fault, which meets each of its claims at the first place
# the map says it. So a claim said twice — a sentence copied into a summary while the section that
# wrote it keeps its own — aims that case at the summary and leaves the citation behind it
# unread, and the pin below refuses the document in that shape.
# That pin lifts its claims out of the case's own row table with a pattern anchored on how a row
# is typeset today, so it carries a second copy of that table's shape: reflow one row onto two
# lines, or lift a claim into a named constant, and the case still asserts thirteen claims while
# the guard reads twelve and still answers that each of them is said once. The pin after it counts
# the rows a second way, by tokenising the table's strings and braces, and refuses the two readings
# disagreeing. Neither opens anything; together they cost a fifth of a second.
check-pillars: ## Fail when the implementation map drifts: a pillar citation outside its file, a composition claim said twice, or a claim the guard no longer reads
	python3 -B scripts/architecture_pillars_test.py
	bash scripts/a_claim_the_composition_case_reads_is_said_once_in_the_implementation_map_test.sh
	bash scripts/the_composition_citation_guard_reads_one_claim_for_every_row_of_its_table_test.sh

# The rehearsal and the public-API comparison, both of which existed as steps
# nobody was forced to run: `make rehearse` needed an operator who remembered it,
# and the apidiff workflow said "deliberately not a required check" and ran on a
# Sunday. Both answer a question no other gate here can — what this release's
# migrations cost on a table the size the installation actually has, and which
# exported name a pinned consumer compiled against last time — and a question
# answered only after a version is published is a review of the damage.
#
# The rehearsal's base is v1.1.0, because that is the release an installed database
# replays from. The exported API freezes at its own revision, named by the line record
# below: one variable drove both lines while the API kept moving, and where one name
# chooses the migration base and the API base, freezing either moves the other in
# silence. The seed is the fixture the rehearsal script documents, because a migration
# measured against an empty table is a migration measured against nothing.
#
# The cost is measured, not assumed: against a copy of the v1.1.0 ledger seeded
# with 10,000 rows per table, the rehearsal of this tree's nine pending files ran
# in 19 s of wall clock (commit b7a0354's Verified: line names the run), and
# check-apidiff takes about a minute. Both sit inside the 75-minute ceiling
# 8a3297a set for the check job. When that ceiling gets tight, T-0219 splits the
# job; nothing is removed from `check` to fit it.
REHEARSE_BASE ?= v1.1.0
REHEARSE_ARGS ?= --base-ref $(REHEARSE_BASE) --seed scripts/testdata/rehearse/seed.sql
APIDIFF_RECORD ?= scripts/baselines/public-api-v2.0.0-candidate.json

check-rehearse: ## Apply this tree's pending migrations to a copy of the previous release's database
	REHEARSE_RECEIPT="$${REHEARSE_RECEIPT:-$$(mktemp)}" ./scripts/rehearse_migrations.sh $(REHEARSE_ARGS)

# The release form of the same step: one case per installation this release promises to
# upgrade from, which is the support record's list and not this file's, plus the record's
# refusals asked for their reason and the exported-surface comparison beside the data.
# `check-rehearse` stays the single-source diagnostic a contributor runs in a minute;
# nothing is published from one case, and scripts/check_pin_rehearsal.sh --strict refuses a
# receipt that measured one. It costs a second base build and a second copied database, so
# it is the merge tier's and not every pull request's.
check-rehearse-supported: ## Certify this tree against every installation the release promises to upgrade from
	REHEARSE_RECEIPT="$${REHEARSE_RECEIPT:-$$(mktemp)}" ./scripts/rehearse_migrations.sh --supported

# The support record decides which installations a release undertakes to upgrade from, and
# the release is certified from that same file, so the cases that read it are the promise's
# own coverage check rather than extra tests of a helper: which revisions the record may
# name, which it may not narrow away, which applied bytes it owes, and what a failed
# migration leaves behind. `go tool gotestsum --packages='./...'` sees no shell script, so a
# case nobody names in a recipe line is a case nothing runs, and a gate nobody runs is prose.
#
# Two of the five want the stack, and refuse without it rather than skipping: a gate that
# passes because the machine had no database would certify the retry and the byte refusal
# on a checkout where neither ran.
check-support-record: ## Fail when the support record, its promise anchor, the applied bytes or the migration retry stop holding
	bash scripts/supported_sources_test.sh
	bash scripts/a_narrowed_support_record_is_not_a_new_baseline_test.sh
	bash scripts/migration_retry_test.sh
	bash scripts/applied_bytes_refusal_test.sh
	bash scripts/a_case_whose_evidence_denies_its_own_receipt_is_refused_test.sh

# The line record names the revision the exported surface is frozen at: the published
# tag once the owner cuts it, the frozen commit until then. No list of accepted breaks
# is passed and none is read, so within a line the surface only grows and any reported
# incompatibility is a refusal — including one somebody had written down. Moving the
# freeze is the owner's, in a commit of its own: the diff of that one line is the
# review, and a red gate is not a reason. docs/COMPATIBILITY.md owns the rule.
check-apidiff: ## Fail on any exported API change from the release line's frozen surface
	python3 scripts/check_public_api.py --line-record $(APIDIFF_RECORD) HEAD

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

# The provider legs of kit/inference's one conformance suite. `make check` runs that
# suite over both far ends this repository holds — the script, and the OpenAI-shaped
# adapter over a loopback stub — which is the whole of the portability claim a
# checkout with no network can make. What it cannot claim is that a second vendor
# speaks the same wire, so that claim is made per run and per named endpoint, and the
# suite refuses a leg it cannot reach rather than skipping it.
#
# PROVIDERS=ollama,hosted reads PLATFORMKIT_TEST_INFERENCE_OLLAMA_URL, _MODEL and
# _KEY, and the same three for `hosted`. A key is never printed: the leg names the
# variable it read and not the value, as the adapter's error sentences name a status
# code and a host for the same reason. Each real endpoint Honours the port's own
# semantics and declines the far-end deviations — a hosted vendor will not answer a
# truncated reply to order — and prints the cases it declined.
PLATFORMKIT_TEST_INFERENCE_PROVIDERS ?= $(PROVIDERS)
export PLATFORMKIT_TEST_INFERENCE_PROVIDERS
inference-conformance: ## Run the inference port's suite against operator-named providers (PROVIDERS=ollama,hosted)
	@test -n '$(PLATFORMKIT_TEST_INFERENCE_PROVIDERS)' || { echo 'usage: make inference-conformance PROVIDERS=<name>,<name>, each configured by PLATFORMKIT_TEST_INFERENCE_<NAME>_URL, _MODEL and _KEY' >&2; exit 2; }
	go test -v -count=1 -timeout=10m -run 'TestTheConfiguredProvidersConform' ./kit/inference

check-push: build check-generated vet fmt-check check-loc check-complexity check-packages check-gucs check-comments check-ui check-versions check-run-owner check-test-inventory ## The push tier: contract and behaviour of the packages the diff reaches, no stack
	./scripts/check_push_tier.sh

check-merge: check-rehearse check-rehearse-supported check ## The merge tier: the whole suite, the rehearsal against a populated database, the whole supported matrix, the journeys in the suite

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
	./modules/change/... ./kit/locale/... ./apps/platformkit
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
# hold byte copies of the line that result, in CI's expansion and with the count emptied,
# scripts/race_binary_timeout_test.py asks the test binary what it was given, and
# scripts/race_bound_not_below_check_bound_test.sh compares this bound with `check`'s own.
check-race: ## Run the concurrency kernel under -race
	go test -race $(TEST_COUNT) -timeout=30m -p=4 $(RACE_PACKAGES)

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

# The journey steps this module publishes are a surface a pinned consumer compiles against, and
# nothing else here asks them a question: `make check` builds Go, and gate 10 transpiles the two
# files without asking whether they should exist. So the published names, the specifiers a module
# zip can carry, the addresses the reference application serves and the hosts a harness step may
# fall back to are decided here, beside check-apidiff, which makes the same promise about Go. It
# reads text and builds no fixture tree, so nothing it asks needs node, a browser or a database —
# the placement argument check-run-owner makes above.
check-step-library: ## Refuse a published journey step that vanished, or one that cannot load from the pin
	bash scripts/e2e_step_library_test.sh

# Gate 10's own port choice runs inside `check` rather than only inside gate 10, because the three
# promises it makes — the port it refuses, the listener it is willing to serve through, and the port
# it hands Playwright — otherwise fail as a wrong number in a browser run rather than as a red gate
# here. See scripts/free_port.sh for why the answer is no longer the literal 8099 and why a 200 on
# /health is not enough to answer it, and scripts/free_port_test.sh for the cases behind each of them.
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
# The mobile fetch case runs the workflow step against a local forge and checks the digest
# and file:// handoff. The precedence case keeps the repository secret ahead of the job token:
# putting the job token first prevents the fetch from using the repository credential.
# Both need PyYAML, which CI installs before this gate.
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
check: build check-generated check-screen-render vet fmt-check check-loc check-complexity check-packages check-gucs check-comments check-ui check-richtext-bundle check-versions check-applied-migrations check-run-owner check-pillars check-step-library check-i18n check-rehearse check-support-record check-apidiff check-test-inventory ## Everything a pull request must pass
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
	go tool gotestsum --packages='./...' -- $(TEST_COUNT) -timeout=30m -p=4
	bash scripts/check_architecture_test.sh
	# scripts/provenance_case_paths_test.sh asks whether the provenance cases the architecture gate runs
	# are still the files it invokes. The gate keeps that list in one `for provenance_case in …; do`
	# line, and a rename that reaches only one side of it goes wrong twice over: a name with no file
	# behind it fails the gate at `FAIL: … refuses the line the rehearsal report is headed with`, which
	# blames the rehearsal step for a missing file, and a list emptied out reaches no case at all while
	# the gate prints its `rehearsal step:` line and moves on. Reading the list out of the gate's own
	# text, rather than repeating it, is what keeps the two halves from parting — so this file refuses a
	# loop it cannot find, a loop that names nothing, a named case with no file behind it, and a case
	# named for the round that wrote it rather than for what it protects. It starts no service, reads one
	# file and two paths, and answers in under a second.
	bash scripts/provenance_case_paths_test.sh
	# scripts/test_comments_explain_what_they_protect_test.sh reads every whole-line comment of every
	# test file — with each comment group's line breaks collapsed — and refuses one that cites the round,
	# finding number or attempt number that filed the case instead of the behaviour it protects. The
	# collapse is the point: a phrase split across two `//` lines is invisible to `rg` and to the
	# per-package comment pins, and one file's own opener was written that way. It excludes the files the
	# naming census already counts as debt, derived from each file's name rather than listed, so a rename
	# brings its prose under the rule in the same commit. It starts nothing and reads tracked text; the
	# planted cases it proves itself on are written into a directory of its own, outside the tree.
	bash scripts/test_comments_explain_what_they_protect_test.sh
	# scripts/comment_group_boundaries_test.py asks whether the guard above can still see the shape it
	# exists for. It runs that guard's own scanner definitions, read out of the file above rather than
	# copied, on inputs no test file in the tree happens to contain: a citation wrapped over the last two
	# lines of a file, behind a blank line and a tab, at each of the two comment markers and each of the
	# two line endings, where a group reader that stops at end of file or numbers a group from its last
	# line loses the phrase; a blank line and a line of code between two comment groups, which must stay
	# two groups so two unrelated sentences never collapse into one citation; and the domain prose —
	# "Review the proposal", "rounded 5 values" — that has to stay allowed. Three mutants of those
	# definitions die on it: a run that carries on across code, a group reported at its last line, and a
	# matcher that refuses the word "review". It starts nothing, reads two files and answers in 5 ms.
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/comment_group_boundaries_test.py
	# scripts/plural_comment_citations_test.py holds the plural half of that matcher, which no other layer
	# names out loud: narrow the matcher to the singular and this file is the one that says which half went
	# missing, on six subcases over both comment markers and three casings of the word, where the guard's
	# own planted case fails for a reason that reads like a broken arithmetic boundary. It holds the other
	# side too — four sentences of ordinary numeric prose, "5.2 to 5, with half rounded up" and "A round
	# trip reads 8 rows" among them, that the matcher must keep accepting — so a matcher widened past the
	# digits fails here as well. It reads the matcher through the case above, which execs the guard's own
	# definitions, so no copied pattern can drift from the real one. It starts nothing, reads two files and
	# answers in milliseconds.
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/plural_comment_citations_test.py
	bash scripts/check_budget_ratchet_test.sh
	# scripts/a_branch_that_carries_mains_numbers_keeps_what_it_priced_test.sh runs the ratchet over a
	# merge of two branches that each moved the same budget file, the shape a real merge of main takes when
	# the merge itself carries main's numbers. The suite above builds each case from one base commit and
	# never merges, so git's history simplification — which follows one parent of a merge whose budget files
	# match that parent, and so can hide the branch's own build(budget) commits — is unobservable there.
	# One throwaway repository, six commits across its two branches, no network, no database, one second.
	bash scripts/a_branch_that_carries_mains_numbers_keeps_what_it_priced_test.sh
	# scripts/budget_commits_count_once_test.sh asks the ratchet the question neither case above can: does
	# one commit that prices both budget files declare its allowance once? Naming a commit is what the walk
	# above does per filename it edited, so a commit that moves loc-budget.json and packages-budget.json
	# together can enter the declared growth twice — and neither neighbour sees it: the merge case changes one
	# budget file per commit and the suite above builds each case from its own base. Doubled, two priced
	# lines admit four new ones, and a 600-line raise reads as 1,200 and is refused for an owner trailer
	# nobody owed. Two throwaway repositories, three commits each, no network, no database, one second.
	bash scripts/budget_commits_count_once_test.sh
	# scripts/a_raise_restated_after_a_merge_is_priced_once_test.sh asks the ratchet what a branch declares
	# when it merges main twice and, after each merge, writes main's loc-budget.json numbers back up to the
	# ceiling it had priced: one declaration, restated. Measured against the commit before it, each
	# restatement is a fresh raise, so a 400-line raise buys 1,200 lines and is then refused for three
	# `Owner-approved:` trailers on a raise that never passed 1,000 lines — and the lax half of the same
	# history passes 800 lines against 300 priced. Neither neighbour sees the shape: the merge case above
	# merges once and never re-prices, the count-once case never merges, and the suite at the top of this
	# list builds every case from one base commit. Two throwaway repositories, eleven commits in each, one
	# `go build` of the counter and no network, database, browser or listener: two seconds.
	bash scripts/a_raise_restated_after_a_merge_is_priced_once_test.sh
	# scripts/a_lowering_cannot_price_growth_from_a_merge_resolution_test.sh asks whether bringing a ceiling
	# down can mint growth credit. The branch merges main with a higher ceiling written only in the
	# resolution, then lowers that ceiling in a build(budget) commit of its own: a merge declares nothing,
	# so the lowering has to leave the branch with nothing priced. Neither neighbour above reaches the
	# shape — the merge case never re-prices and the restated case never lowers. One throwaway repository,
	# nine commits, no network, no database, one second.
	bash scripts/a_lowering_cannot_price_growth_from_a_merge_resolution_test.sh
	# scripts/a_lowering_cannot_expand_a_previously_priced_allocation_test.sh holds the other half of the
	# same rule: a branch that priced two lines, merged main with a higher ceiling in the resolution and
	# then lowered it still holds two lines, not eight. Kept apart from the case above on purpose — an
	# allowance read as "never more than the branch's own raise" passes here while a branch with no raise
	# at all is granted credit from nothing, and the reverse mistake passes there. Two throwaway
	# repositories' worth of one history, eleven commits, one `go build` of the counter, no network,
	# database, browser or listener.
	bash scripts/a_lowering_cannot_expand_a_previously_priced_allocation_test.sh
	# scripts/a_lowered_allocation_survives_main_without_restoring_returned_lines_test.sh asks what an
	# allocation the branch spent most of survives: fifteen hundred lines priced, fourteen hundred and fifty
	# of them consumed, fifty returned, then a merge of main carrying main's numbers. The remainder has to
	# stay priced across that merge, because the merge base moves to main's side and the branch's own raise
	# is still the only thing that priced it; and the one line of the returned fifty the branch then adds has
	# to be refused against the fourteen hundred and fifty still standing. Neither neighbour above reaches
	# both halves: the merge cases never consume what they price, and the approval case never merges. One
	# throwaway repository, eight commits, four verdicts over it, no network, no database.
	bash scripts/a_lowered_allocation_survives_main_without_restoring_returned_lines_test.sh
	# scripts/a_lowering_after_a_merge_cannot_restore_returned_allocation_test.sh asks the question the two
	# lowering cases above leave open between them: a branch priced two lines, took one of them back, merged
	# main with a higher ceiling written only in the resolution, then lowered that ceiling in a commit of its
	# own. One line still stands priced — the second lowering tidies a number the branch never raised, and
	# what an earlier lowering returned stays returned until a raise reprices it. Read the newest declaration
	# alone and capped by the largest raise on the branch, both lowerings pass while the returned line is
	# granted back. One throwaway repository, nine commits, three verdicts over it, no network, database,
	# browser or listener.
	bash scripts/a_lowering_after_a_merge_cannot_restore_returned_allocation_test.sh
	# scripts/a_returned_allocation_can_be_partly_repriced_test.sh asks what an allowance is worth once
	# part of it has been handed back: the branch priced six lines, spent three, returned three, and a later
	# build(budget) raise buys one of those three. "Newest declaration wins" grants that branch the whole
	# original six, because a merge of main left a higher number for the lowering to write over; "largest
	# raise ever written" refuses the one line, because it holds a returned allocation open forever. The
	# running total both readings were written to replace is what this case can tell apart, and the merge and
	# the lowering that follow the reprice have to leave the other two lines unpriced. Each of its four
	# verdicts also asks that the ratchet leave the repository it was asked about at the HEAD it found and
	# with a clean status: the merge-base form clones and checks out that tree, and a checker that wrote to
	# it would make every verdict after it worthless. One throwaway repository, fourteen commits across its
	# two branches, four verdicts over it, one `go build` of the counter, no network, database, browser or
	# listener; 2.8 s measured.
	bash scripts/a_returned_allocation_can_be_partly_repriced_test.sh
	# scripts/a_lowering_needs_no_new_owner_approval_test.sh asks who owes the `Owner-approved:` trailer of
	# a bucket standing over 1,000 lines: the commit that raised it, never the one that brings part of it
	# back. CONTRIBUTING.md permits lowering a ceiling and asks for the trailer on a raise, so returning
	# 500 unused lines of an approved 2,000 is the tidying that document prescribes — and the ratchet
	# refuses it in both of its forms, CI's explicit base and `make check-loc`'s merge base, the moment it
	# reads the lowering as a raise. Two budget commits over one base, no network, no database.
	bash scripts/a_lowering_needs_no_new_owner_approval_test.sh
	# scripts/ratchet_fixtures_name_their_committer_test.sh asks the one question the eight fixtures above
	# cannot ask of each other: whether every script test that builds a throwaway repository and runs `git
	# merge` names its committer inside that repository. A merge commits with the identity the repository
	# holds rather than the one an invocation passes, so a fixture added without a repository-local identity
	# writes its history in a checkout that happens to have one and aborts at its first merge in a container
	# that has none, taking the cases after it with it. Each fixture reads only its own file, so none of them
	# notices a new sibling missing that identity. One grep over `scripts/*_test.sh`, no repository built, no
	# network, no database, milliseconds.
	bash scripts/ratchet_fixtures_name_their_committer_test.sh
	# scripts/a_test_file_no_commit_holds_is_counted_before_it_lands_test.sh asks the ratchet whether what it
	# counts is the tree on disk or the tree the index holds. Every case above commits the lines it measures,
	# so none can see the order a priced case actually arrives in: the file sits on disk while the raise that
	# pays for it stands alone in a commit of its own. Three throwaway repositories — four lines no commit
	# holds, the same four lines priced before the file lands, then landed — one loc-budget.json each, no
	# network, no database, about a second.
	bash scripts/a_test_file_no_commit_holds_is_counted_before_it_lands_test.sh
	bash scripts/check_applied_migrations_test.sh
	bash scripts/applied_migration_merge_base_test.sh
	bash scripts/ci_checkout_history_test.sh
	bash scripts/mobile_journey_fetch_test.sh
	bash scripts/mobile_journey_fetch_token_precedence_test.sh
	bash scripts/mobile_journey_token_environment_test.sh
	bash scripts/ci_go_cache_test.sh
	# scripts/ci_browser_host_share_test.sh asks the three browser suites of the workflow the question
	# their refusals cannot answer from inside a runner: does a step that starts a Chromium ask the host
	# for a core before its first page, and does the suite's own verdict survive the sampler wrapped
	# around it? The guard it runs is scripts/ci_browser_host_share.sh, adopted here because `go tool
	# gotestsum --packages='./...'` sees no shell script and a step that quietly stopped guarding its
	# browsers would leave nothing else red — which is also why the case asks the dry-run recipe of this
	# goal for its own line, and refuses a copied Makefile that lost it. Gate 10 joined those two at the
	# step that lost `shared families: en / dark` to `frame.evaluate: Target crashed` at job 70995, the
	# second death of that one case in four runs read beside it. Cases over the guard's three
	# subcommands, its three steps and the three shapes a host can leave memory accounting in, six of
	# them planted refusals over copied files; 3.4 s, no node, no browser, no database and no listener.
	bash scripts/ci_browser_host_share_test.sh
	# scripts/ci_browser_guard_readable_cgroup_test.py asks that same guard the one thing this machine
	# cannot ask it: the guard reads fixed paths under /sys, so a host that exposes none reports the
	# ceiling as unreadable and every case above passes on the refusal alone. This case copies the guard
	# and its case into a tree of its own with those paths redirected to readable cgroup v1 and v2
	# fixtures, and requires the case to pass where the counters are there to be read — a guard that
	# answered the ceiling question only when it had no numbers to give would leave this red and that
	# host's log silent. Two fixture trees, one copied recipe, nothing started.
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/ci_browser_guard_readable_cgroup_test.py
	# scripts/ci_browser_recent_load_test.py holds the admission boundary of that same `wait` at its
	# load file: five shapes read in turn — a quiet host, a host at exactly one task per core, and the
	# three where the one-minute term, the five-minute term, or both sit above the core count — with a
	# free share required exactly where both recent windows fit and `starting anyway` everywhere else,
	# printing the bytes it read. A host whose recent windows fit is admitted whatever its fifteen-
	# minute term says, which is the difference between asking what a host is doing and asking what it
	# did. Five reads of a fixture load file, nothing started.
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/ci_browser_recent_load_test.py
	# scripts/ci_sampler_output_lifetime_test.py asks the sampler wrapped around a suite the one question
	# the suite's own verdict cannot answer: when the watched suite finishes, does the sampler let go of
	# its output? The guard sleeps between samples, and a sleep holds the write end of the suite's output
	# pipe open, so a sampler still asleep when its suite died keeps a reader waiting for the sleep rather
	# than for the suite — and the reader that waits for end-of-output never reaches the status the suite
	# exited with. The case runs the guard's `watch` over a suite that prints one line and exits, once at
	# the passing status and once at a refusal, at a sampling interval of thirty seconds, and requires the
	# output to close within ten, carrying that line and that status, and the sample taken before the
	# suite started to be in it: a first sample only ever taken behind the suite leaves the baseline the
	# guard compares against unwritten. Two shells and no interval actually waited, nothing listening.
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/ci_sampler_output_lifetime_test.py
	# scripts/ci_sampler_pin_is_a_check_recipe_line_test.sh asks whether the line above is still in the
	# recipe at all. tests/inventory.json files a scripts/ pin at tier push from its path alone and
	# `go tool gotestsum --packages='./...'` sees no Python, so a recipe that dropped that line would
	# leave an inventory row pointing at a case nothing runs — the same gap this goal's own wiring cases
	# refuse for the lines they name. The case reads this goal's dry-run recipe for that exact command and
	# refuses a recipe it was handed without it, so the file and the line that runs it cannot part. One dry
	# run of this goal and one mutation of the recipe in memory; nothing is compiled, started or tested.
	bash scripts/ci_sampler_pin_is_a_check_recipe_line_test.sh
	bash scripts/make_check_count_default_test.sh
	# scripts/make_test_timeout_override_test.sh asks the clock's question of that same goal. It dry-runs
	# `make test` twice — once as everyone runs it, once with a caller's own `-timeout` inside TEST_FLAGS —
	# and refuses a stated default that vanished or a caller's bound that no longer arrives last. Go honours
	# the last `-timeout` it is handed, so the order of the two on that line is the whole promise: put the
	# default after the caller's flags and a shorter ask is answered with the longer bound, which is a suite
	# ignoring the clock its caller named. Two dry runs in a directory holding only go.mod's module, go and
	# toolchain lines; nothing is compiled, started or tested.
	bash scripts/make_test_timeout_override_test.sh
	# scripts/race_boundary_guard_test.py asks whether the case above could still refuse. It copies the
	# committed Makefile and that guard into a directory of its own, takes one part out of the race goal
	# at a time — the detector, the fresh count, the stated bound — and asks four things: that the pair as
	# committed answers, and that each of the three stripped of one part refuses at the race line and
	# nowhere else. No file in the tree is touched: the mutation lives in the copy, and the Makefile and
	# the workflows stay as committed. What it buys is the difference between a guard that is read and a
	# guard that can be broken, for the four dry runs it names and no database.
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/race_boundary_guard_test.py
	# scripts/race_guard_check_recipe_test.py asks that the line above stays a line the goal runs. It
	# expands this goal and asks for exactly one executable mention of that guard — not a comment, not a
	# recipe line ending in `|| true` — beside the inventory row that still files it as a `scripts/` pin
	# at the push tier. A line commented out keeps the file, the comment above it and the log's silence,
	# and loses the refusal, which is the difference this case draws.
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/race_guard_check_recipe_test.py
	# The race guards above hold the recipe's bytes, break the guard that reads them, and ask that the
	# breaking stays wired; this one runs the recipe and asks the binary. It dry-runs `make check-race`
	# for one package, hands the command it prints back to go with `-n`, and asserts on the arguments the
	# test binary receives after flag parsing: the stated clock there, go's ten-minute default when the
	# same recipe loses it. A bound that stops at the recipe would satisfy every guard above and still
	# leave the suite answering to a clock nobody chose, which is the failure this repository already
	# paid for once. Half a second, nothing compiled, nothing started.
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/race_binary_timeout_test.py
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
	# The one step of the check job that asks a mirror for anything. Its `apt-get update` ended the job
	# red on 2026-10-09 with exit 100 and no assertion — `File has unexpected size … Mirror sync in
	# progress?`, a CDN node whose index had rolled forward under the Release it was serving — and every
	# guard that asks for psql, ss or PyYAML was red behind a fact about Canonical's mirrors. This file
	# runs the retry it replaced: which failures are worth another ask, how long they wait, that a fact
	# about the index is refused at once, that every step of the workflows which asks apt for a package
	# goes through that bound, and that the step which buys three tools asks all three whether they
	# arrived. It breaks no mirror of its own: the stub apt-get answers from memory, and the one case that
	# pays the real waits pays the first of them.
	bash scripts/ci_apt_index_test.sh
	# scripts/ci_npm_audit_test.sh asks the same question of the one npm command in this repository that
	# answers against a live feed. Forge run 8105 refused the design job's audit step with `npm error
	# audit endpoint returned an error` over lock bytes `main` had passed twenty minutes earlier; the
	# retry it replaced is bounded, retries only a registry that would not answer, and refuses an audit
	# that printed a finding on its first try — that last ordering is the whole of the difference between
	# a retry and a way to lose an advisory, so it is asked first. It reads no registry: the stub npm
	# answers from memory, and the workflows' own audit line is run as the job runs it.
	bash scripts/ci_npm_audit_test.sh
	# scripts/ci_package_install_refusal_test.py runs the half of that step the guard above cannot see: the
	# index answered, the ask went out, and the package did not arrive. apt-get install exits 100, and the
	# `-e` act_runner runs every step body under is what stops it there. A body that lost it, or gained a
	# `|| true`, would carry on and look green doing it: ci.yml's three questions are answered by whatever
	# is on PATH, so `command -v ss` and `python3 -c 'import yaml'` would pass off a stub and report an
	# install that installed nothing, and mobile.yml's next line dials `psql -h postgres`. This case runs
	# both jobs' committed step bodies with PATH the stub directory alone, refuses the install, and asks
	# two things of each: that the step exits 100 after exactly two apt calls, and that the file its psql,
	# ss and python3 stubs each append to the moment they run was never created. It reuses the apt double
	# scripts/ci_apt_index_test.sh defines rather than carrying a second one, installs nothing, starts no
	# container, pays no real wait (the retry bound is handed `0 0`) and writes only inside a
	# TemporaryDirectory it removes.
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/ci_package_install_refusal_test.py
	# scripts/package_install_refusal_guard_test.py asks whether the case above would still refuse if the
	# refusal it exists to catch came back as success. It runs that case twice over the committed workflow
	# bodies — once as written, once with `|| true` appended to every `apt-get install` line in the step
	# body the runner passes through subprocess — and requires the second run to fail both workflow
	# subcases with no error and no skip. Nothing on disk changes: the mutation lives in the argument list
	# of the one call, and the workflows stay as committed. It is here because a pin nobody can break is
	# a pin nobody can trust: the two apt doubles and the three stub tools are shared with the case it
	# mutates, so this file carries no fixture of its own and costs the two runs it names.
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/package_install_refusal_guard_test.py
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
	# The one `make check` line that reads an npm lock. It asks that the four packages the design job's
	# `npm audit --omit=dev --audit-level=high` refused — source-map-js and dompurify at `0bfae63`, zod at
	# `badcf51f`, and undici at the range npm reports for three ids — are locked above the ranges those
	# reports name, and that the gate that named them is still a step in the job that reads this lock, at
	# that level and in that directory. It reads no feed and installs nothing: the feed moved under the
	# runner every time, so the lock is the only half of this a tree can answer.
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
	# (`\t// golang.org/x/net v0.62.0` answers `is absent, so nothing here resolves above the range`),
	# and a release *superseding* every fix — the committed pins sit exactly at v0.62.0 and at go1.27.3,
	# the newest toolchain floor, so "above all of them" is never asked. This case writes six go.mod files
	# into a temporary directory and asks the guard about each through its subprocess, and asserts exit
	# codes rather than sentences, so a floor comparison that passes for the wrong reason — an absent pin
	# read as a satisfied range — cannot answer 0. It reaches no network and builds nothing.
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/go_mod_advisory_inputs_test.py
	# The x509 floor alone: go.mod at go1.27.2, go1.27.3 and go1.27.4 with x/net already fixed, so the
	# first is refused by GO-2026-6809's line and no other, and the two later releases pass.
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/go_mod_x509_floor_test.py
	bash scripts/check_pin_rehearsal_test.sh
	# Where a run finds the mail catcher. The journeys that open a mailed link are
	# the only proof the address the application dials is right, and they cannot say
	# which of the two addresses in this repository they were pointed at: a caller
	# that runs one spec directly used to dial compose.yaml's default port while the
	# catcher it had been given answered on another one, and the journey blamed
	# Playwright. This case asks the script itself, in under a second, with no stack.
	bash scripts/e2e_mail_address_test.sh
	bash scripts/free_port_test.sh
	# scripts/mobile_listener_attribution_test.py asks the journey's receipt step the question the
	# planted-silence cases cannot: whether the socket table of the machine running this answers with
	# the exact pid of the listener the step itself started. It runs the committed step body with one
	# reader on PATH at a time, ss, then lsof where the host carries it, refuses a reader that answers
	# with some other pid sharing the true one's prefix, and asks two steps to name their own listeners
	# at once. No node, no container and no real wait: it starts one loopback server per case and kills
	# its process group whether the case passed or refused.
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/mobile_listener_attribution_test.py
	# Whether the job would run that step at all. scripts/free_port_test.sh reads the receipt step's own
	# policy keys — no `if:`, and no `continue-on-error:` other than the literal `false` — and this case is
	# the check on that reading: it plants each weakening in a copy of the workflow and runs the whole
	# guard over the copy, which must lose the verdict while keeping the commands that just passed, so a
	# refusal belongs to the declaration and not to a step whose body went missing with it. The accepted
	# forms — no condition, and the explicit `continue-on-error: false` — are run beside them, so a guard
	# that hardened into a refusal of any line under a step's name fails here. Four runs of the guard, so
	# a few seconds more than its own line, and still no stack, no container and no device.
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/mobile_listener_step_policy_test.py
	# Whether both cases above are still lines of this recipe. tests/inventory.json files every
	# scripts/ pin at tier push from its path alone, so the table promises a run no wiring keeps: an
	# edit that moved either line into another goal, or dropped it, leaves the push headline counting
	# a case no pull request runs. This case asks `make -n check` for both lines and plants each
	# deletion, so a refusal names the line that went missing; its own make run is a dry one, which
	# compiles nothing, starts no container and needs no stack.
	bash scripts/mobile_listener_pins_are_check_recipe_lines_test.sh
	# Which failures of the API gate's one network step are worth another try. The target
	# itself is the last prerequisite; this case is the bound on its retry, so it runs with
	# the rest of the script cases and needs no stack, no proxy and no tree but this one.
	bash scripts/check_public_api_fetch_test.sh
	# The other half of that bound: a fetch that fails twice and then succeeds must still be
	# judged on what it exported. These cases drive the gate's own main() with the tool's
	# subprocess calls answered from memory, so they need no network and no second tree.
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/check_public_api_retry_test.py
	# What the frozen line refuses, measured rather than argued: a throwaway removal, a
	# throwaway addition, a method added to an exported interface and a reshape, each run
	# through the shipped gate over a scratch module under mktemp -d. The fake above cannot
	# say what the comparison tool reports, and what it reports is what this gate rests on,
	# so these cases pay the same module proxy the target pays — ten seconds of it.
	bash scripts/check_public_api_line_test.sh
	bash scripts/the_release_line_record_and_the_design_pin_agree_test.sh
	bash scripts/e2e_guards_run_before_the_gate_test.sh
	# The other half of that ordering, which nobody had asked. Gate 10 runs `make e2e` inside
	# scripts/ci_browser_host_share.sh's sampler, and the sampler's whole contract is that it
	# hands the suite's status back; a step line that swallowed it — `|| true`, a pipe into
	# something that succeeds, continue-on-error — would leave the job green over a browser
	# suite that died, and the case above reads only the step's name. This one runs the step's
	# own run block the way a runner runs it, from a copy of the tree's shape, with `make`
	# answering 3 and the host wait bounded at zero, so no node, browser or database starts, and
	# shows the same reading telling a swallowed status from the real step in three planted ways.
	bash scripts/e2e_gate_answers_with_its_suite_status_test.sh
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
	# A generated file or section edited by hand, or a marker the writer cannot pair, is refused by
	# check-generated; this case proves it in a copy of the tree, never in this checkout.
	bash scripts/a_hand_edited_generated_file_fails_the_regeneration_check_test.sh
	# Whether either goal still asks at all. `check` and `check-push` name `check-generated`,
	# which runs both writers and then diffs with --exit-code, and both CI workflows run `make
	# check`; the case above calls the goal itself, so it cannot notice a goal that lost the
	# prerequisite or a diff that lost its exit status, and `go tool gotestsum` sees neither
	# goal. This one dry-runs both goals for the two writers and then the diff, in order, and
	# refuses each of those four losses — every edit made to a copy of this Makefile in a
	# temporary directory, never to this checkout's.
	bash scripts/the_check_goals_regenerate_before_they_diff_test.sh
	# scripts/e2e_guards_run_before_the_gate_test.sh above asks one goal whether it still runs the two
	# subjects it exists for. These two ask that question of `check` itself, whose two source gates are
	# prerequisites, and a prerequisite that leaves the list leaves nothing else red:
	# `go tool gotestsum --packages='./...'` sees no Python file and no `.spec.ts`, so a `check-pillars`
	# that dropped out would let ARCHITECTURE.md's citations land on blank lines, and a
	# `check-step-library` that dropped out would let a published journey step vanish while gate 10 went
	# on transpiling a file nobody owns. scripts/journey_and_pillar_check_wiring_test.sh dry-runs this
	# goal and refuses a recipe that has lost either command — a nested `make -n`, the shape
	# scripts/check_architecture_test.sh already runs from inside this same recipe, measured at 5 ms for
	# the whole dry run here. scripts/journey_and_pillar_check_refusal_test.sh asks whether that pin
	# would notice: it deletes one prerequisite at a time from a temporary copy of this Makefile and
	# refuses each acceptance. Both read text and start nothing, which is the argument
	# check-step-library makes for itself above, and here it holds twice over — unlike its two subjects
	# these need no node and no browser either, so nothing argues for a job later than this one. Review 5
	# found the first wired to nothing: correct at `0012ae0`, run by nobody, and a gate nobody runs is
	# prose. Wiring it is these two lines.
	bash scripts/journey_and_pillar_check_wiring_test.sh
	bash scripts/journey_and_pillar_check_refusal_test.sh
	# The third and fourth lines here are that argument run once more over itself. Review 6 wrote
	# scripts/journey_check_recipe_test.sh — the one assertion that both source-gate guards still execute
	# as children of a parallel make, which is the question `make -n` cannot answer — and the sixth
	# round adopted its bytes at `f676a6e` unwired, on scope rather than cost: the file measured 0.52 s
	# standalone and the same inside a recipe at -j2, with make printing `-j2 forced in submake:
	# resetting jobserver mode`, which is the nested recipe taking its own token pool. Review 6 named
	# that deferral as the thing left to adopt, and a gate nobody runs is prose, so it is wired here.
	# scripts/journey_check_recipe_wiring_test.sh is the case this line needs and `f676a6e` deliberately
	# did not write: `go tool gotestsum --packages='./...'` sees no shell script, so a recipe line that
	# leaves this list leaves nothing else red. It asks the dry-run recipe for this command, copies this
	# Makefile to a temporary tree, deletes the one line, and requires the same assertion to fail there —
	# three dry runs of a goal one of these two prerequisites already dry-runs, 0.2 s for the file.
	# What pins the last line is nothing: the case has no peer to pin it, and it says so rather than
	# pretending otherwise. Both files start nothing — no node, no browser, no database, no listener.
	bash scripts/journey_check_recipe_test.sh
	bash scripts/journey_check_recipe_wiring_test.sh
	# scripts/both_roads_keep_an_unset_operation_set_apart_from_an_empty_one_test.sh asks the one
	# question two packages answer from the same pair of facts: a resource that declared no operation
	# set mounts all five verbs, because nothing was said, and one whose list named nothing mounts
	# none. kit/entity/present turns that pair into views and ui/resource into controls, and no Go case
	# reaches both roads, so this pin runs each and refuses the length reading on both — on a copy, by
	# Go's build overlay, never this checkout. Four `go test` invocations of two small packages, 2 s.
	bash scripts/both_roads_keep_an_unset_operation_set_apart_from_an_empty_one_test.sh
	# scripts/a_module_that_declares_a_view_another_module_owns_is_refused_by_the_manifest_test.sh
	# asks the manifest the same question from the other side: module.Validate accepts a module's own
	# custom-screen descriptor, refuses one whose owner is another module, refuses one named twice.
	# screen.Publish refuses a foreign mount too, but at the routing door, with the manifest's own
	# diagnostics gone and the sentence naming an operation rather than the module that wrote the
	# declaration — and the composition gate that catches a missing route-level screen answer stays
	# green with the manifest's check deleted, so nothing else in the tree asks it. The pin proves it
	# answers to that call site by deleting the three lines from a copy of validate.go over Go's build
	# overlay and refusing when the copy passes the case. Two `go test` runs of one small package, 1 s,
	# no stack and no browser.
	bash scripts/a_module_that_declares_a_view_another_module_owns_is_refused_by_the_manifest_test.sh
	# scripts/the_view_manifest_pin_is_a_check_recipe_line_and_refuses_by_assertion_test.sh asks the
	# two questions the pin above cannot ask about itself: that this recipe still runs it, and that the
	# mutated copy of validate.go is refused by the injected case's own assertion rather than by a
	# build that stopped compiling — a mutation branch that reads any non-zero `go test` as that
	# refusal prints its second ok over a copy that no longer builds. It reads the dry-run recipe of
	# this goal and extracts the Go case from the pin it judges, so the two cannot drift apart. One
	# dry run and two `go test` runs of one small package, 2 s, no stack and no browser.
	bash scripts/the_view_manifest_pin_is_a_check_recipe_line_and_refuses_by_assertion_test.sh
	# scripts/a_served_page_names_no_write_door_behind_a_route_it_does_not_mount_test.sh asks the
	# question those two leave: ui/screens decides which write routes a mounted resource answers,
	# ui/resource draws the doors, and the two read different files — so a served page that named a
	# New button, an Edit link or a Delete form behind a route nobody mounted would publish an anchor
	# to a view the composition published nowhere. The pin injects one Go case into ui/screens over
	# Go's build overlay, asks it of this tree, and refuses a copy of the renderer that answers from
	# what the resource declares instead of what the adapter mounts. Two `go test` runs of one small
	# package, 2 s, and the one test schema the package's own server helper opens; no browser.
	bash scripts/a_served_page_names_no_write_door_behind_a_route_it_does_not_mount_test.sh
	# scripts/the_served_page_door_pin_is_a_check_recipe_line_and_refuses_by_its_own_verdict_test.sh
	# asks the two questions the pin above cannot ask about itself: that this recipe still runs it,
	# and that both of its runs report a verdict for the case they inject. The pin reads the exit
	# status of each `go test` as its answer, so an overlay that stopped applying, or a mutated
	# renderer that simply stopped compiling, would print its second `ok` anyway; this guard
	# requires the injected case's own PASS and FAIL lines. It reads the dry-run recipe of this goal
	# and extracts that case from the pin it judges, so the two cannot drift apart. One dry run and
	# two `go test` runs of one small package, 2 s measured here, beside its subject; no browser.
	bash scripts/the_served_page_door_pin_is_a_check_recipe_line_and_refuses_by_its_own_verdict_test.sh
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

up: ## Start Postgres, NATS, Valkey, the secret store, the object store and the mail catcher, and wait for all six to be healthy
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

# Every profile is named here as well, because `down` is the file's teardown: a goal
# that stopped four containers and left a fifth holding a port would half finish the
# one job it has. That is measured here and not taken from a comment: a `down` naming
# one profile and not the other does not stop the other's container, prints
# `Network … Resource is still in use`, and exits 0 — so a stack that had ever started
# the candidate store would leak one container every time it tore itself down, which is
# the cost this repository is trying to shrink rather than add. It stays destructive of
# volumes, as before.
down: ## Stop Postgres, NATS, Valkey, the object stores, the mail catcher and the collector, and drop their volumes
	docker compose --profile telemetry --profile floci down -v
