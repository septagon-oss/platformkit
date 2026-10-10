# Test inventory and verdicts — platformkit (decision 0088)

One row per test in [`inventory.json`](inventory.json) (`platformkit.test-inventory.v1`): 2315 rows over 1008 files. This file is the table a person ratifies; CI re-checks the JSON — every test in the tree has exactly one row, no row has no test, and the round-named file count never exceeds its ceiling. Every column names the measurement that filled it. An empty column is `unmeasured`, never zero, and a verdict beside an unmeasured column is a keep.

## Scale

| quantity | value | measured by |
|---|---|---|
| test rows | 2315 | one per Go test function, Playwright test, Maestro flow, pin script |
| files | 1008 | `git ls-files` over `*_test.go`, `e2e/**`, `scripts/*_test.*` |
| Go test functions | 2226 | `^func (Test\|Benchmark\|Fuzz\|Example)` |
| files named for a round (`review*`, `round*`, `probe*`) | 214 | filename prefix |
| rows inside them | 346 | same walk |
| Playwright specs / Maestro flows / `scripts/` pins | 53 / 1 / 35 | same walk |

## Layers and tiers

| layer (0088) | rows | tier | predicate that put it there |
|---|---|---|---|
| contract | 19 | push, first | asserts a golden or the published API set |
| behaviour | 2087 | push or merge | one package's rules; push when the package opens no stack |
| composition | 155 | merge | package `main` of the reference app: it boots the composition |
| journey | 54 | nightly | `e2e/`: browser or device |

Tier need is a **package** fact, not a file fact: one test binary per package, so one file that opens Postgres puts every case beside it behind a database. 834 rows sit in packages that open nothing, 1427 in packages that do, 54 in the browser.

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

| verdict | files | rows | meaning |
|---|---|---|---|
| `keep` | 794 | 1969 | not named for a round: 0088 rule 2 does not reach it |
| `keep-rewrite` | 69 | 131 | named for a round, and the only file in its package reaching something — rewritten and renamed for the rule in the same commit (0072) |
| `merge` | 145 | 215 | a rule a kept sibling in the same package already reaches — `merge_into` names it |
| `delete` | 0 | 0 | nothing in the file judges anything, and the file is not a TestMain harness, a benchmark or an `// Output:` example |

A `merge` or `delete` executes only after its file's coverage delta is measured at the head of the prune and reads zero unique production lines. `unique_reach` is a static proxy — the first-party call sites, or the HTTP routes for a file that drives the app over HTTP — and it is complete for every Go row; `unique_lines` is the dynamic measurement and exists only where it was run.

### Coverage, measured on a sample of the candidate set

`coverdelta.py` ran over the 36 round-named files in the 10 packages that need no stack: run the package with everything, run it again with that file's cases deselected by `-run`, diff the profiles. **33 of 36 cover zero production lines that another test in the package does not cover**. The 3 that do are exactly the three listed under "Keeps that are the only proof" with a number beside them, and they are `keep-rewrite` rows: where the static reach column and the coverage measurement both speak, they agree. Package coverage across the ten measured packages runs from 19.4% to 96.5% of statements.

The reading that matters: statement coverage cannot tell these files apart, in a 96.5%-covered package and a 19.4%-covered one alike. A file can be the only thing that checks *what* a refusal says while touching no line nothing else touches. So coverage is the floor the prune must not break (acceptance: coverage not lower), and the rule column is what decides a verdict — coverage never issues a `delete` here. The instrument was checked against itself: with every case deselected, all all 69 covered lines of `ui/screens` are attributed as unique, so a zero is a measurement and not a broken diff.

### Deletes

| file | rows | evidence |
|---|---|---|
| *(none)* | 0 | mechanical evidence supports no delete at this revision |

An empty column is the honest first answer, and it came from a near miss. Counting assertions as only `t.Errorf`/`t.Fatalf` called nine `delete`s: `apps/platformkit/main_test.go` (the `TestMain` that gives the 122-case package its database), `kit/jobs/capacity_test.go` (`BenchmarkPerTenantCapacity`, which `make load-test` runs), `kit/events/providers/nats/stream_fixture_test.go` (the fixture), four `*/contracts/*test/fake_test.go` (each one line: the call into the conformance suite that is the fake's whole proof) and two `pkit` files (one `// Output:` example, one whose assertion is a local `says(t, …)` helper). A rule that deletes those things is worse than no rule, so `delete` requires the file to hold nothing that judges — assertion helper, conformance run, `// Output:`, benchmark or harness — and a named reason. Everything the prune can defend on mechanical evidence is a `merge` or a rename.

### Merges, by target — the fifteen packages with most

| package | files | into |
|---|---|---|
| `kit/db` | 29: review11_a_contract_half_waits_for_a_version_this_release_can_point_at_test.go, review11_a_contract_half_waits_whether_or_not_the_owner_has_history_test.go, review11_a_statement_inside_a_dollar_body_is_no_statement_at_all_test.go … | `alter_actions_test.go` |
| `ui` | 15: review_round10_resolved_name_test.go, review_round11_kernel_class_test.go, review_round12_class_attribute_test.go … | `client_layer_structure_test.go` |
| `apps/platformkit` | 5: review_r3_two_tenants_two_languages_test.go, review_r8_commissioned_language_test.go, review_r9_login_refusal_language_test.go … | `app_test.go` |
| `kit/db` | 5: review11_a_refusal_id_is_declared_wherever_the_runner_keeps_it_test.go, review13_a_catalog_count_by_name_alone_sees_another_tests_schema_test.go, review14_the_rule_table_and_the_prose_that_counts_it_do_not_say_the_same_number_test.go … | `(none in package)` |
| `kit/db` | 5: review12_a_drain_over_a_key_whose_smallest_value_is_the_empty_string_terminates_test.go, review13_a_data_body_that_moves_its_own_key_is_refused_rather_than_drained_test.go, review13_two_drains_of_one_table_write_no_row_twice_test.go … | `data_file_shape_test.go` |
| `kit/app` | 4: review3_budget_from_configuration_test.go, review3_drain_in_flight_boots_test.go, review_drain_composed_test.go … | `app_test.go` |
| `migrations` | 4: review_r8_locale_backfill_role_test.go, review_round1_module_schema_gaps_test.go, review_round3_namespace_grants_test.go … | `module_schema_test.go` |
| `modules/auth/internal` | 4: review_r1_a_tenant_that_turned_sso_off_is_not_served_the_door_test.go, review_r1_one_shared_issuer_still_separates_two_clients_test.go, review_r3_the_pinned_route_count_names_the_whole_surface_test.go … | `http_test.go` |
| `modules/tenant/internal` | 4: review_r1_a_retired_tenants_host_test.go, review_r1_the_refusal_writes_nothing_test.go, review_r2_a_refused_add_host_writes_no_host_row_test.go … | `lifecycle_test.go` |
| `apps/platformkit` | 3: review1_asyncapi_payload_shape_test.go, review_round1_flow_rate_denominator_test.go, review_round1_wire_break_laundering_test.go | `(none in package)` |
| `apps/platformkit` | 3: review_r7_denied_page_language_test.go, review_round1_declared_language_test.go, review_round4_denied_page_test.go | `access_refusal_test.go` |
| `kit/db` | 3: review3_guard_floor_test.go, review_guarantees_test.go, review_round14_the_run_puts_the_budgets_back_the_way_it_found_them_test.go | `migrate_autocommit_budget_test.go` |
| `kit/events` | 3: review2_purge_exemption_is_the_rows_own_test.go, review_round1_outbox_lag_test.go, review_round5_a_relay_pass_drains_whatever_its_ledger_holds_test.go | `events_test.go` |
| `kit/telemetry` | 3: review_round17_only_the_composition_links_the_measurement_sdk_test.go, review_round18_the_guide_prints_no_figure_a_reader_cannot_recheck_test.go, review_round19_the_two_documents_that_quote_the_tally_agree_about_it_test.go | `(none in package)` |
| `modules/auth/internal` | 3: review_r4_a_scoped_key_is_held_to_its_scope_test.go, review_r4_another_tenants_factor_is_not_spendable_test.go, review_r5_two_tabs_answering_one_code_test.go | `first_half_test.go` |

The merge target is named by *reach*, not by reading the two files as prose. Where the target does not assert the rule — reach overlap is not rule identity — the verdict becomes `keep-rewrite` and the file is renamed instead. That reading is the prune commit's job, and the row records it either way.

### Keeps that are the only proof — the reach nothing else has

| file | unique reach | unique lines (where measured) |
|---|---|---|
| `apps/platformkit/review_r1_both_sides_of_the_trail_test.go` | /hosts/watched.localhost | unmeasured |
| `apps/platformkit/review_r1_way_on_and_notified_test.go` | /api/v1/auth/roles/gatekeeper | unmeasured |
| `apps/platformkit/review_r2_sessions_screen_revocation_on_the_trail_test.go` | /app/auth/sessions, /app/auth/sessions/revoke-rest | unmeasured |
| `apps/platformkit/review_r2_the_json_ask_door_test.go` | /api/v1/app/access-requests, /app/widget/widgets | unmeasured |
| `apps/platformkit/review_r4_a_released_host_answers_for_its_successor_test.go` | /hosts/initech-secondary.localhost | unmeasured |
| `apps/platformkit/review_r5_a_key_that_opens_no_door_is_not_a_credential_test.go` | /api/v1/auth/factors/recovery/rotate, /api/v1/auth/roles/admin, /api/v1/auth/sessions | unmeasured |
| `apps/platformkit/review_r6_the_door_the_kernel_opened_test.go` | /api/v1/auth/factors, /api/v1/auth/factors/00000000-0000-0000-0000-000000000000, /api/v1/auth/factors/totp/begin | unmeasured |
| `apps/platformkit/review_round3_aliases_test.go` | /admin, /admin/assets, /api/v1/admin | unmeasured |
| `apps/platformkit/review_surfaces_test.go` | /admin, /admin/_gallery, /admin/tenants/new | unmeasured |
| `kit/app/review5_two_spellings_render_one_document_test.go` | app.AsyncAPI | unmeasured |
| `kit/app/review_round17_the_tally_says_which_registered_jobs_it_counts_test.go` | app.Name | unmeasured |
| `kit/app/review_round20_a_collector_that_never_answered_test.go` | kit/telemetry.Tracer | unmeasured |
| `kit/app/review_round9_an_unexporting_process_still_propagates_test.go` | kit/telemetry.Propagators, kit/telemetry.WithRequestID | unmeasured |
| `kit/config/review_round16_sample_ratio_test.go` | config.Ratio | 4 |
| … | 46 more, each with its reach and delta in `inventory.json` | |

### Round-named verdicts by area

| area | files | keep-rewrite | merge | delete |
|---|---|---|---|---|
| kit | 349 | 34 | 78 | 0 |
| modules | 245 | 10 | 24 | 0 |
| ui | 151 | 5 | 21 | 0 |
| apps | 89 | 9 | 16 | 0 |
| e2e | 54 | 5 | 0 | 0 |
| scripts | 35 | 4 | 0 | 0 |
| migrations | 12 | 2 | 6 | 0 |

## The four composition journeys (0088 layer 3)

| rule | journey | status at this revision |
|---|---|---|
| a tenant this composition does not serve is refused | `apps/platformkit/tenancy_a_write_for_a_tenant_the_plan_does_not_open_is_refused_test.go` | new — `cross_tenant_proposal_test.go` proves row isolation inside a served tenant, not a tenant the plan refuses |
| a duplicate delivery is idempotent | `apps/platformkit/billing_the_same_period_charged_twice_takes_the_money_once_test.go` | new — `Idempotency` exists only in `modules/billing` (contracts + provider); the mounted app never presents the key twice |
| a populated database upgrades | `make check-rehearse` (`scripts/rehearse_migrations.sh` + `scripts/testdata/rehearse/seed.sql`) | exists at the wrong tier: it is a `check:` prerequisite (`Makefile:372`), so it runs on every push instead of on merge |
| a plan-gated door closes | `apps/platformkit/entitlement_a_feature_the_plan_does_not_open_closes_the_door_test.go` | new — composition-plan refusals exist (`composition_missing_module_names_its_needers_test.go`); nothing boots the app and watches the door refuse |

## Ratchets this prune leaves behind

| ratchet | today | measured by |
|---|---|---|
| round-named test files | 214 before the prune | new `scripts/check_round_named_count.sh`, ceiling = the post-prune count, `--write` lowers it, a rise refused outside a `build(budget):` commit |
| `go_test` lines | 134 204 against the 134 013 ceiling | `go run ./tools/locbudget --check` |
| inventory rows | 2315 | `scripts/check_test_inventory.sh` re-derives them from the tree |
| coverage | unmeasured tree-wide; measured per package for 10 packages here | `make cover` (new): the prune refuses a figure below the recorded one |

## What CI re-checks, after the tiers

* **push** — contract rows first (no stack, one golden per surface), then behaviour rows of the packages the diff reaches, selected by `go list -deps` over the diff and refused if the selection opens a database; plus `scripts/check_test_inventory.sh`.
* **merge** — every behaviour row, every composition row, the rehearse, the apidiff, the byte pins: what `make check` runs today, with gotestsum writing `--jsonfile` beside it.
* **nightly** (new `schedule:`, the pattern of `.gitea/workflows/public-consumption.yml:21-24`) — journeys, walkthrough, and the slowest-tests and flake reports published as run artifacts under `reports/` so the completeness metric has a path to read.

Nothing in the tiers is a new selector language: the tiers are goals, and `make check` stays the sum of them.
