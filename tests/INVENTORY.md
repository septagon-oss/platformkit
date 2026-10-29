# Test inventory and verdicts — platformkit (decision 0088)

One row per test in [`inventory.json`](inventory.json) (`platformkit.test-inventory.v1`); the `Scale` table counts them. This file is the table a person ratifies; CI re-checks the JSON — every test in the tree has exactly one row, no row has no test, and the round-named file count never exceeds its ceiling. "Every test" means every file a runner of this repository executes, in whatever language: Go functions, Playwright specs, Maestro flows, `scripts/` pins, and the `node --test` cases under `tools/designexport/openpencil/` that the `editor` job runs (`kind: node` in the JSON). Every column names the measurement that filled it. An empty column is `unmeasured`, never zero, and a verdict beside an unmeasured column is a keep.

## Scale

<!-- BEGIN GENERATED: scripts/inventory_figures.py scale -->
| quantity | value | measured by |
|---|---|---|
| test rows | 5013 | one per Go test function, Playwright test, Maestro flow, pin script, `node --test` case |
| files | 2754 | `git ls-files` over `*_test.go`, `e2e/**`, `scripts/*_test.*`, `*.test.{js,mjs,ts}` |
| Go test functions | 4634 | `^func (Test\|Benchmark\|Fuzz\|Example)` |
| files named for a round (`review*`, `round*`, `probe*`) | 79 | filename prefix |
| rows inside them | 123 | same walk |
| Playwright specs / Maestro flows / `scripts/` pins / node tests | 122 / 1 / 157 / 99 | same walk |
<!-- END GENERATED -->

## Layers and tiers

<!-- BEGIN GENERATED: scripts/inventory_figures.py layers -->
| layer (0088) | rows | tier | predicate that put it there |
|---|---|---|---|
| contract | 24 | push, first | asserts a golden or the published API set |
| behaviour | 4284 | push or merge | one package's rules — Go, or one design-tooling case; push when the package opens no stack |
| composition | 582 | merge | package `main` of the reference app: it boots the composition (every `scripts/` pin is filed here too, at tier push) |
| journey | 123 | nightly | `e2e/`: browser or device |
<!-- END GENERATED -->

Tier need is a **package** fact, not a file fact: one test binary per package, so one file that opens Postgres puts every case beside it behind a database. Rows sit in packages that open nothing, in packages that do, and in the browser. The push tier's headline, which `scripts/check_test_inventory.sh` prints, also counts the contract-key rows (openapi, asyncapi, golden, wire_compatibility, mobile flows) that `tier_of` files under push by name while their package opens a stack — a package the selector files under its first row, which for those files is merge. They run at merge, and the headline counts them at push; `--tier push` refuses such a package outright, so a needs_db package whose alphabetically-first file matches a contract key would have no way to pass. That is a known overstatement, named under `Limits`.

The push headline holds Go rows, `scripts/` pins and `node --test` cases. All three shapes run on a pull request, but not from the same line: the Go rows come out of `--tier push`, the pins are recipe lines of `make check` (which `check-push` runs fewer of, named under `Limits`), and the node cases run in the `editor` job — `npm test` over the tool's own `*.test.mjs`, `npm run test:browser` over `browser/`, and two later steps that pass `editor/*.test.mjs` and `preview/<file>` to the runner directly (`.gitea/workflows/ci.yml:741,779,1044,1073`). The headline is what a pull request runs; the selector's list is what `scripts/check_push_tier.sh` runs, and it answers Go packages only.

Wall clock today, read from the forge: the `check` job of the last 13 completed runs on main (runs 408–578) — median 2468s, p90 2718s, slowest 3070s. Per-package timings exist for 17 packages only, because no CI step writes gotestsum's JSON report today (`Makefile:90-92` names the file in a comment and nothing writes it): the `duration` column is therefore empty per test, and wiring that report is implement-deliverable 5.

### Flakiness, over the runs that were read

0088 asks for flakiness over the last 30 runs of main. The forge answers at job granularity for those runs (30 read, 408–578; the last 30 main runs are further back than one API page of `branch=main`) and at package granularity for the 17 packages whose summary lines reach a log. It is a *failure* count, not a flake count: a run that failed because its own change was broken sits in the same column. The nightly tier separates the two — a job that fails while main did not change is a flake, and the report the nightly publishes is what makes that sentence checkable.

| job | runs | failed |
|---|---|---|
| `check` | 14 | 2 |
| `design` | 14 | 1 |
| `editor` | 8 | 1 |
| `journey` | 15 | 15 |
| `report` | 1 | 1 |

The `check` job is not where the flakiness is; `journey` — `.gitea/workflows/mobile.yml:36`, the phone flow — failed in nearly every main run read. The journey tier this task wires is being wired onto a failing floor, and that is stated here rather than discovered at the prune.

## Verdicts

<!-- BEGIN GENERATED: scripts/inventory_figures.py verdicts -->
| verdict | files | rows | meaning |
|---|---|---|---|
| `keep` | 2509 | 4627 | not named for a round: 0088 rule 2 does not reach it |
| `keep-rewrite` | 91 | 170 | named for a round, and the only file in its package reaching something — rewritten and renamed for the rule in the same commit (72) |
| `merge` | 154 | 216 | a rule a kept sibling in the same package already reaches — `merge_into` names it |
| `delete` | 0 | 0 | nothing in the file judges anything, and the file is not a TestMain harness, a benchmark or an `// Output:` example |
<!-- END GENERATED -->

A `merge` or `delete` executes only after its file's coverage delta is measured at the head of the prune and reads zero unique production lines. `unique_reach` is a static proxy — the first-party call sites, or the HTTP routes for a file that drives the app over HTTP — and it is complete for every Go row; `unique_lines` is the dynamic measurement and exists only where it was run.

### Coverage, measured on a sample of the candidate set

`coverdelta.py` ran over the 36 round-named files in the 10 packages that need no stack — at the revision `inventory.json` records as `revision_measured` (`5f3ae05`), which is also the revision the `unique_lines` column belongs to; the files below and in the table beside it now carry those figures under the names this tree answers, and no row's measurement moved when its name did. Run the package with everything, run it again with that file's cases deselected by `-run`, diff the profiles. **33 of 36 cover zero production lines that another test in the package does not cover**. The 3 that do are exactly the three with a number beside them in `inventory.json` — `kit/telemetry/exemplar_pin_test.go` (7), `kit/locale/providers/xtext/review_r6_merged_index_test.go` (5) and `kit/config/sample_ratio_test.go` (4) — and they are `keep-rewrite` rows: where the static reach column and the coverage measurement both speak, they agree. Package coverage across the ten measured packages runs from 19.4% to 96.5% of statements.

The reading that matters: statement coverage cannot tell these files apart, in a 96.5%-covered package and a 19.4%-covered one alike. A file can be the only thing that checks *what* a refusal says while touching no line nothing else touches. So coverage is the floor the prune must not break (acceptance: coverage not lower), and the rule column is what decides a verdict — coverage never issues a `delete` here. The instrument was checked against itself: with every case deselected, all all 69 covered lines of `ui/screens` are attributed as unique, so a zero is a measurement and not a broken diff.

### Deletes

| file | rows | evidence |
|---|---|---|
| *(none)* | 0 | mechanical evidence supports no delete at this revision |

An empty column is the honest first answer, and it came from a near miss. Counting assertions as only `t.Errorf`/`t.Fatalf` called nine `delete`s: `apps/platformkit/main_test.go` (the `TestMain` that gives the 122-case package its database), `kit/jobs/capacity_test.go` (`BenchmarkPerTenantCapacity`, which `make load-test` runs), `kit/events/providers/nats/stream_fixture_test.go` (the fixture), four `*/contracts/*test/fake_test.go` (each one line: the call into the conformance suite that is the fake's whole proof) and two `pkit` files (one `// Output:` example, one whose assertion is a local `says(t, …)` helper). A rule that deletes those things is worse than no rule, so `delete` requires the file to hold nothing that judges — assertion helper, conformance run, `// Output:`, benchmark or harness — and a named reason. Everything the prune can defend on mechanical evidence is a `merge` or a rename.

### Merges, by target — the fifteen (package, target) groups with most

<!-- BEGIN GENERATED: scripts/inventory_figures.py merges -->
| package | files | into |
|---|---|---|
| `kit/db` | 29: autocommit_function_body_test.go, bare_name_letters_test.go, bodys_own_cte_reads_the_window_that_leads_the_merged_list_test.go … | `alter_actions_test.go` |
| `kit/porttest` | 14: review10_alias_write_test.go, review10_names_are_the_run_test.go, review12_shared_refusal_name_test.go … | (none in package) |
| `apps/platformkit` | 13: asyncapi_payload_shape_test.go, flow_rate_denominator_test.go, review10_subcommand_pin_test.go … | (none in package) |
| `apps/platformkit` | 5: composition_mounts_its_own_catalog_test.go, golden_regeneration_test.go, review_r3_two_tenants_two_languages_test.go … | `app_test.go` |
| `kit/db` | 5: catalog_count_by_name_alone_sees_another_tests_schema_test.go, every_unexceptable_refusal_is_named_in_the_division_below_the_table_test.go, refusal_id_is_declared_wherever_the_runner_keeps_it_test.go … | (none in package) |
| `kit/db` | 5: connection_failure_on_the_migrate_door_carries_no_credential_test.go, data_body_semicolon_test.go, data_body_that_moves_its_own_key_is_refused_rather_than_drained_test.go … | `data_file_shape_test.go` |
| `kit/app` | 4: budget_from_configuration_test.go, drain_composed_test.go, drain_in_flight_boots_test.go … | `app_test.go` |
| `migrations` | 4: deployment_schema_namespace_test.go, module_schema_gaps_test.go, namespace_grants_test.go … | `module_schema_test.go` |
| `modules/auth/internal` | 4: one_shared_issuer_still_separates_two_clients_test.go, pinned_route_count_names_the_whole_surface_test.go, second_half_asks_about_the_account_test.go … | `http_test.go` |
| `modules/tenant/internal` | 4: refused_add_host_writes_no_host_row_test.go, refused_remove_host_writes_no_host_row_test.go, retired_tenants_host_test.go … | `lifecycle_test.go` |
| `apps/platformkit` | 3: review_r7_denied_page_language_test.go, review_round1_declared_language_test.go, signed_person_without_grant_shown_page_reference_test.go | `access_refusal_test.go` |
| `kit/db` | 3: contention_err_contended_under_budget_deployment_named_test.go, source_cannot_declare_floor_past_head_test.go, the_run_puts_the_budgets_back_the_way_it_found_them_test.go | `migrate_autocommit_budget_test.go` |
| `kit/events` | 3: a_relay_pass_drains_whatever_its_ledger_holds_test.go, outbox_lag_test.go, purge_exemption_is_the_rows_own_test.go | `events_test.go` |
| `kit/telemetry` | 3: only_the_composition_links_the_measurement_sdk_test.go, the_guide_prints_no_figure_a_reader_cannot_recheck_test.go, the_two_documents_that_quote_the_tally_agree_about_it_test.go | (none in package) |
| `modules/auth/internal` | 3: another_tenants_factor_is_not_spendable_test.go, scoped_key_is_held_to_its_scope_test.go, two_tabs_answering_one_code_test.go | `first_half_test.go` |
<!-- END GENERATED -->

The merge target is named by *reach*, not by reading the two files as prose. Where the target does not assert the rule — reach overlap is not rule identity — the verdict becomes `keep-rewrite` and the file is renamed instead. That reading is the prune commit's job, and the row records it either way.

### Keeps that are the only proof — the reach nothing else has

| file | unique reach | unique lines (where measured) |
|---|---|---|
| `apps/platformkit/alias_row_reference_application_leads_somewhere_answers_test.go` | (+1 more), /admin, /admin/assets … | unmeasured |
| `apps/platformkit/ask_door_tells_trail_grants_nothing_test.go` | /api/v1/app/access-requests, /app/widget/widgets | unmeasured |
| `apps/platformkit/both_sides_of_the_trail_test.go` | /hosts/watched.localhost | unmeasured |
| `apps/platformkit/door_the_kernel_opened_test.go` | /api/v1/auth/factors, /api/v1/auth/factors/00000000-0000-0000-0000-000000000000, /api/v1/auth/factors/totp/begin … | unmeasured |
| `apps/platformkit/key_that_opens_no_door_is_not_a_credential_test.go` | /api/v1/auth/factors/recovery/rotate, /api/v1/auth/roles/admin, /api/v1/auth/sessions … | unmeasured |
| `apps/platformkit/public_page_links_only_addresses_installation_serves_test.go` | /admin, /admin/_gallery, /admin/tenants/new … | unmeasured |
| `apps/platformkit/released_host_answers_for_its_successor_test.go` | /hosts/initech-secondary.localhost | unmeasured |
| `apps/platformkit/sessions_screen_revocation_on_the_trail_test.go` | /app/auth/sessions, /app/auth/sessions/revoke-rest | unmeasured |
| `apps/platformkit/way_on_and_notified_test.go` | /api/v1/auth/roles/gatekeeper | unmeasured |
| `kit/app/a_collector_that_never_answered_test.go` | kit/telemetry.Tracer | unmeasured |
| `kit/app/an_unexporting_process_still_propagates_test.go` | kit/telemetry.Propagators, kit/telemetry.WithRequestID | unmeasured |
| `kit/app/the_tally_says_which_registered_jobs_it_counts_test.go` | app.Name | unmeasured |
| `kit/app/two_spellings_render_one_document_test.go` | app.AsyncAPI | unmeasured |
| `kit/config/sample_ratio_test.go` | config.Ratio | 4 |
| … | the rest, each with its reach and delta in `inventory.json` | |

### Round-named verdicts by area

<!-- BEGIN GENERATED: scripts/inventory_figures.py area-verdicts -->
| area | files | keep-rewrite | merge | delete |
|---|---|---|---|---|
| kit | 804 | 69 | 139 | 0 |
| modules | 713 | 35 | 36 | 0 |
| ui | 341 | 22 | 5 | 0 |
| apps | 329 | 24 | 28 | 0 |
| tools | 160 | 1 | 0 | 0 |
| e2e | 123 | 5 | 0 | 0 |
| design | 15 | 9 | 0 | 0 |
| migrations | 15 | 5 | 8 | 0 |

The verdict columns sum to 170 keep-rewrite, 216 merge and 0 delete. Areas that hold no verdict, with their file populations: `scripts` (157 files), `pkit` (97 files).
<!-- END GENERATED -->

`tools` appears with the design tooling's JavaScript cases: one of them, `browser/review_round18_authored_role_read.test.mjs`, is named for the round that wrote it, and is the only one the walk has to judge. `files` counts every file in the area — the population the verdicts are drawn from — and the three verdict columns count the rows `inventory.json` carries, so they sum to the `Verdicts` table above. The table lists the areas that hold a verdict; an area that holds none is every `keep`, so a row for it would carry three zeroes beside a population and tell a reader nothing the `Verdicts` table above does not, and the sentence under the table names it with its population instead. `ui`'s merge column is small because the round-named sheets that carried those verdicts folded into the named pins beside them. The table and that sentence are written by `scripts/inventory_figures.py` from the same rows (`make generate`), and `make check` refuses this file when they no longer answer them.

## The four composition journeys (0088 layer 3)

| rule | journey | status at this revision |
|---|---|---|
| a door the plan does not open is refused at that door, for two tenants that share it | `apps/platformkit/tenancy_a_write_for_a_tenant_the_plan_does_not_open_is_refused_test.go` | **written at this revision** — `cross_tenant_proposal_test.go` proved row isolation inside a served tenant; this walks the refusal and the working side of one door with two tenants in one installation. Both tenants are served here: the door is closed to one of them, the host is not closed to either — that refusal is the row below |
| a host this composition does not serve is answered a 404 | `apps/platformkit/app_test.go` (`TestAnEmptyDatabaseBecomesAWorkingInstallation`: `GET /api/v1/tasks` at `nowhere.localhost` = 404) and `apps/platformkit/released_host_answers_for_its_successor_test.go` (`TestAReleasedHostStopsAnsweringForTheTenantThatReleasedIt`) | **already in the suite before this revision** — this task wrote no second journey for an unserved host; the row names where the refusal is proved, and the file has since been renamed for its rule (0072) |
| a duplicate delivery is idempotent | none | **not written at this revision** — `Idempotency` exists only in `modules/billing` (contracts + provider); the mounted app never presents the key twice, and no seam swaps `billing.Manual()` or triggers `billing-renew`. The seam is a brief of its own; the row says what is missing rather than naming a file not in the tree |
| a populated database upgrades | `make check-rehearse` (`scripts/rehearse_migrations.sh` + `scripts/testdata/rehearse/seed.sql`), still a `check:` prerequisite and now one of `check-merge:` too | moved to the merge tier at this revision: the step in `.gitea/workflows/ci.yml` is gated `github.event_name != 'pull_request'`, so a push job runs no rehearsal and the push to main that merges runs it |
| a plan-gated door closes | the last third of `apps/platformkit/tenancy_a_write_for_a_tenant_the_plan_does_not_open_is_refused_test.go` | one file, not two: the tenant that buys nothing is still refused at the door *after* its neighbour buys the plan that opens it, in the same boot — a second file would boot an installation to re-ask what this one answers |

## Ratchets this prune leaves behind

| ratchet | today | measured by |
|---|---|---|
| round-named test files | the round-named count against `summary.round_named_ceiling` | `summary.round_named_ceiling` in `inventory.json`, re-checked by `scripts/check_test_inventory.sh`: `--write --ceiling N` lowers it, and a rise is refused by the same line that prints the count. 22 is the ceiling the naming guard already holds (`CENSUS_CEILING` in `scripts/test_comments_explain_what_they_protect_test.sh`), so the table and the guard refuse a rise at the same number. They do not count the same population: that census, and the loop's gate beside it, match `review` anywhere in a file name, so they hold four files the prefix walk does not — `apps/platformkit/change_review_queue_test.go`, `kit/httpx/refusal_host_form_review7_test.go` and the translation module's two `mark_reviewed_*` pins — and the walk holds one the census never reads, `tools/designexport/openpencil/browser/review_round18_authored_role_read.test.mjs`, whose suffix is not a shape the census takes. Hence the 22 answers a wider walk than the prefix count the `Scale` table gives; 22 is what a rise is refused against, and three of those four names say what their files check in the product's own words — a change queue, a copy already marked reviewed — so no rename of one of them is the answer this table holds. The one name that did move is `kit/wire`'s case that a widened kind covers no plan feature the door gained, which `main` filed as `reviewed_widening_gains_no_feature_test.go` after this branch's count was full: the table holds every row that predates `scripts/test_rename_carries_assertions_test.py` at the path the merge answered it under, so the file that arrived beside that pin is the one this branch could still name for its rule, and it is named `an_allowance_pair_over_the_kind_covers_no_feature_the_door_gained_test.go` — the package's own words for the same rule, since the mechanical name the generator proposes (`areviewed_widening_covers_no_feature_the_door_gained_test.go`) keeps the letters both censuses match. The rename is the path alone: the function, its assertions and its message read as `main` wrote them. |
| inventory rows | the `Scale` table's `test rows` | `scripts/check_test_inventory.sh` re-derives them from the tree |
| coverage | unmeasured tree-wide; measured per package for 10 packages here | `make cover` (new): the prune refuses a figure below the recorded one |

<!-- BEGIN GENERATED: scripts/inventory_figures.py go-test-ceiling -->
The `go_test` bucket declares a ceiling of 242154 lines in `loc-budget.json`; `make check-loc` refuses a branch's growth in it beyond what the branch prices in its own `build(budget):` commits since its merge base with `origin/main`.
<!-- END GENERATED -->

## What CI re-checks, after the tiers

* **push** — `make check-push`: the budgets and the static checks, then `scripts/check_test_inventory.sh`, then `scripts/check_push_tier.sh`, which asks `scripts/test_inventory.py --tier push` for the packages the diff reaches — the directories `git diff` names, the package that owns a changed file under a directory of its own (a golden under `testdata/`, an entry under a `go:embed` directory: the nearest ancestor directory `go list` reports as a package, because the test binary reads those bytes), the packages that import them through `go list`'s dependency graph (review 1), and the packages whose test binary compiles them through `TestImports` or `XTestImports` (review 2) — and refuses with exit 3 a selection reaching a package that opens a stack. Contract rows are not run ahead of the rest, and the selection runs no package the diff does not reach; `Limits` names both.
* **merge** — every behaviour row, every composition row, the rehearse, the apidiff, the byte pins: what `make check` runs, as `check-merge:`'s prerequisites. gotestsum writes no JSON report beside it (`Makefile:90-92` names the file in a comment and nothing writes it), which is why the `duration` column is still `unmeasured`.
* **nightly** — `.gitea/workflows/nightly.yml`: `make flakes FLAKE_RUNS=5` and `make check-test-inventory`, both printing into the job log. It publishes no artifact yet, so the flake and duration columns are read by a person from that log; the job header says so.

Nothing in the tiers is a new selector language: the tiers are goals, and `make check` stays the sum of them.

## Limits

What this table overstates, and what its selector does not yet do. Each is a known gap named here rather than a claim the gate holds.

* **The push tier's headline counts rows it cannot run.** `tier_of` labels a contract-key path (`openapi`, `asyncapi`, `golden`, `wire_compatibility`, `mobile_flows`) push wherever it appears, so files in `apps/platformkit` carry `tier: push, needs_db: true`. Their package is filed under its first row, which is merge, so they run at merge; the headline counts them at push, above the true stack-free count. The same rule is a loaded gun: a package whose alphabetically-first file matches a contract key and whose other files open Postgres would be filed under push and refused on every pull request touching it, with no flag that passes. The fix is `tier_of` honouring `needs_db`, and `--check` refusing a push row in a stack-opening package.
* **The selection runs no contract row the diff does not reach**, so 0088 layer 1's "contract rows first" is unimplemented: `kit/wire` and `ui/forms/testdata` open no stack and could run on every push.
* **The push headline counts rows the Go selector never schedules.** The `scripts/` pins and `node --test` rows carry `tier: push` because a pull request runs them — from `make check`'s recipe and the `editor` job — while `--tier push` answers Go packages and prints neither.
* **A push tier defined by "opens no stack" runs no behaviour test for a stack-opening package.** A pull request touching `modules/*`, `kit/db`, `kit/limit` or `apps/*` reaches zero Go cases before merge; `make check-push` also runs fewer of the `scripts/` pins than `make check` does. Whether that is 0088 item 5 as ratified is root's ruling, not this table's.
* **`duration`, `flake` and `unique_lines` are `unmeasured` except where a row says otherwise**, because no CI step writes a per-case report and `make cover` is not a gate.
