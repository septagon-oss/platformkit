# implement — T-0337 the kernel counts what each resource holds, round 2 (fix after a failed gate)

Worktree: this checkout (the loop's WORKTREE), at the task branch's head `d5a1508`, plus the round's
`.cloud-round/` commit `618ef2c`, which the loop drops. Pushed to `cloud/T-0337/implement-2`.

## Result

`IMPLEMENT: complete`, with one environmental caveat stated first and in full:

> `make check` exits 2 in this VM on `modules/webhook/internal TestDeliveryPreservesAnIPv6EndpointPort` alone
> (`listen tcp6 [::1]:0: socket: address family not supported by protocol`). The VM's kernel has no IPv6:
> `/proc/sys/net/ipv6` and `/proc/net/if_inet6` do not exist, and `socket(AF_INET6)` fails with errno 97.
> This branch does not touch `modules/webhook` (`git diff 8020ac5 --stat -- modules/webhook` is empty). The
> loop host has IPv6 and ran this case green in both of its gate runs. Every other recipe line of `make check`
> passed against the final candidate, including the 47 lines after the Go suite, which `make` stops before.

## What the gate refused, the cause, and the fix

| gate finding | cause | fix |
|---|---|---|
| `apps/platformkit TestTheInventoryQuotesItsDeclaredGoTestCeiling`: `inventory quotes 172119 against 172119; declared go_test ceiling is 172855` (both host runs) | `713d530` priced `go_test` at its measured 172855 in `loc-budget.json`, but the ratchet row in `tests/INVENTORY.md` still quoted the old figure | `4736de5 docs(tests): quote the go_test ceiling loc-budget.json now declares` updates the row to `172 855 against the 172 855 ceiling`. The budget is unchanged and the test is unchanged |
| not reported by the host (its gate stopped at the Go suite, so the 47 recipe lines after it never ran there): `scripts/inventory_area_totals_match_document_test.py` red, `kit 458 != 460, apps 234 != 235, ui 176 != 177` | round 1 added test files in kit (2), apps (1) and ui (1) and re-derived `inventory.json`, but the hand-quoted "Round-named verdicts by area" table kept the old `files` column | `87a8744 docs(tests): quote the area file counts inventory.json now holds` changes three numbers. The verdict columns did not move (still 117 keep-rewrite, 173 merge) |
| `kit/events TestAnotherTenantCannotReadOrRewriteAMovedLedger`: `a delivery is mid-claim on ledger-ledger-invoice_issued; no ledger row moved, run it again` (first host run only) | not this branch's: `kit/events` is untouched, the host's second run passed it, and it passes here 3 of 3 (`-count=3`) and in the full suite | none. Recorded rather than called a flake: its message describes a claim in flight, which depends on timing under a shared Postgres |
| `i18n_gate_test.go:130: publish = 409 …` in the host's "failure lines above the tail" | a logged line, not a failure: the second host run printed it and still reported 1 failure (the inventory case). It passes in every run here | none |

No review this round (`--- REVIEW --- (no review yet)`), and the worktree held no uncommitted reviewer test files
(`git status` was clean at the start). So there was nothing to commit unchanged.

## Commands and results (narrow first, then in full)

```
$ ~/.pkit/services.sh up                                   → up=0 (postgres, valkey :6379, s3 :8333, mailpit :8025)
$ timeout 300 go run ./tools/locbudget --check             → go_test 172855 / 172855; every bucket at or under its
                                                             ceiling except markdown 19537 / 17885 (+1652), and
                                                             1652 is exactly `wc -l` of .cloud-round/**/*.md
$ go test -count=1 -run '^TestTheInventoryQuotesItsDeclaredGoTestCeiling$' ./apps/platformkit
                                                           → red before the row edit, then ok 0.109s
$ timeout 200 ./scripts/check_test_inventory.sh            → INVENTORY: 3063 rows agree with the tree; 19 round-named
                                                             file(s) of 22 ceiling; 1028 test(s) of 3063 in the push tier
$ go test -count=3 -run '^TestAnotherTenantCannotReadOrRewriteAMovedLedger$' ./kit/events → ok 0.396s
$ (review-pinned) go test -run '^(TestABootstrapThatRefuses…|…|TestTwoTenantsOfOneInstallationAreServedInTwoLanguagesAtOnce)$' ./apps/platformkit
                                                           → ok 8.362s
$ (review-pinned) go test -run '^(TestARefusalNormalisesItsHostBeforeAskingWhoseTenantItIs)$' ./kit/httpx → ok 0.318s
$ PYTHONDONTWRITEBYTECODE=1 python3 scripts/inventory_area_totals_match_document_test.py
                                                           → FAILED (failures=1) before the table edit, OK after
```

The commits gate, in the host's shape (`8020ac5` plus all four of the task's commits, with no `.cloud-round` commit
in between): a scratch worktree at `d5a1508`, the two new commits cherry-picked onto it, then

```
$ python3 .cloud-round/tools/commits_gate.py . 8020ac5     → commit gate: ok (4 commits, 4 authored)
$ python3 .cloud-round/tools/commits_gate.py . 618ef2c     → commit gate: ok (2 commits, 2 authored)   (in this checkout)
```

Against `d5a1508` (the commit before `.cloud-round`), the gate in this checkout refuses `618ef2c` itself, the
round's own input commit, which has no `Verified:` paragraph. The loop drops that commit.

**`make check` in full**, run in a scratch worktree of the final candidate `87a8744` with `.cloud-round/` removed
(so the markdown budget measures the tree the loop will measure) and Chromium on `PATH`:

```
$ timeout 3300 make check
  … every prerequisite green: build vet fmt-check check-loc check-packages check-gucs check-ui
    check-richtext-bundle check-versions check-run-owner check-pillars check-step-library check-i18n
    check-rehearse ("rehearse: ok: 52 file(s) within 30s each …") check-apidiff check-test-inventory,
    then `go mod tidy -diff`, the pseudo-locale gate, then the suite:
  ✓  apps/platformkit (6m5.47s)
  === SKIP: apps/platformkit TestDeclaredMobileFlowsRan          (as on the host: MAESTRO_JUNIT unset)
  === SKIP: modules/auth TestAPlannedFactorNamesTheTaskThatFinishesIt   (as on the host)
  === FAIL: modules/webhook/internal TestDeliveryPreservesAnIPv6EndpointPort
      ipv6_endpoint_test.go:16: listen tcp6 [::1]:0: socket: address family not supported by protocol
  DONE 6639 tests, 2 skipped, 1 failure in 365.470s
  make: *** [Makefile:543: check] Error 1                         → exit 2
```

and then each of the 47 recipe lines after the `gotestsum` line (from `make -n check`), one at a time, in the
same tree: **47 / 47 exit 0** (`check_architecture_test.sh` … `check_imports.sh`).

Two earlier runs, for the record. The first stopped at `check-rehearse` (`v1.1.0 does not name a revision of this
repository`) because the clone was shallow and had no tags. `git fetch --tags` and `git fetch --unshallow` fixed
the clone, not the tree. The second ran without Chromium on `PATH`: 9 skipped, the same IPv6 failure, and
`inventory_area_totals_match_document_test.py` red in the post-suite lines, which led to `87a8744`. Its seven
Chromium cases (`TestARefusedProposalKeepsTwoTextAlignments` and its phone-width twin, and the `modules/file`
library design cases) were then run with Chromium on `PATH` and all passed. The final run above carries Chromium,
so its skips match the host's two.

`make check` ran once on the final candidate, not twice as the loop runs it. Its only red is unchanged from the
run before it and environmental, and PROMPT.md says not to repeat an unchanged full-suite failure.

## Commits this round

* `4736de5 docs(tests): quote the go_test ceiling loc-budget.json now declares` (`tests/INVENTORY.md`, 1 line)
* `87a8744 docs(tests): quote the area file counts inventory.json now holds` (`tests/INVENTORY.md`, 3 lines)
* `cloud-round: notes` (this file only)

Neither touches `loc-budget.json` or `packages-budget.json`. The markdown line count does not move, because both
edits replace a line with a line.

## What was built (round 1, unchanged this round)

| layer | file | what |
|---|---|---|
| grammar | `kit/crud/predicate.go` | `Op`/`Ops`/`ValidOp`, `Predicate`, `Where`, `CountWhere[T]`, `DayCount`, `CountDays[T]`, `Coerce` (moved from `kit/rest`), `DayStart`, `Declared`, `Resolve` |
| grammar | `kit/crud/crud.go` | `conditions` builds `column = ?` through `predicateSQL` |
| declaration | `kit/entity/hints.go` | `Anchors`/`ValidAnchor`, `EntryHighlight`, `EntryPredicate`, `EntryHints.Highlight`/`.TrendField` |
| closures | `kit/httpx/schemas.go`, `httpx.go` | `Resource.CountFiltered`, `Resource.CountDays` (same read guard as `Count`); `(*API).Cache()` |
| mount | `kit/rest/screens.go` | the two closures, refused where `Count` is |
| gate | `kit/rest/hints.go` | `entryHintsFault` refuses an incoherent `highlight`/`trendField` at mount |
| echo | `ui/screens/hints.go` | `EntryPresentation.Highlight`, `.TrendField`; `CatalogVersion` stays 2 |
| answer | `ui/screens/summary.go` | `Summary`, `SummaryEntry` (`*int64`), `Trend`, `TrendDays = 14`, `Summarise` |
| address | `apps/platformkit/summary.go` | `GET /api/v1/app/summary`, cache `Of(tenant,"summary")`, 15 s TTL, 5 s budget, absence never stored |
| adoption | `modules/task/module.go`, `migrations/000071_task_due_index.up.sql`, `messages/pt-PT.json` | "Due today" highlight, `createdAt` trend, partial `due_at` index, "Vence hoje" |
| goldens/docs | catalogue + OpenAPI goldens, `docs/cache.md`, `apps/platformkit/README.md` | regenerated / written |

## Acceptance (as round 1 left it, corrected where it was stale)

| acceptance | case | state |
|---|---|---|
| the tasks highlight | `TestTheSummaryCountsTheTasksDueTodayAndNothingElse` | passes |
| unavailable vs zero | `*int64` fields, `TestATrendNamesFourteenDaysIncludingAnEmptyOne`; no case forces a count to fail | partial |
| permission-filtered totals | `TestTheSummaryEntryIsTheCatalogEntry`, `TestTheSummaryNamesNothingToACallerWithNoCredential`; no case primes the cache under one grant and re-reads under a refusing one | partial |
| cross-tenant isolation | `TestAWindowedCountSeesNoOtherTenant` (`kit/crud`) and `TestASummaryTotalCannotBeReadAcrossATenant` (`apps/platformkit/summary_test.go:200`, present and green in the full suite; round 1's "Not verified" line saying it was dropped is stale) | passes |
| the day is a UTC day | `TestADayBucketIsAUTCDayUnderAnySessionTimeZone` | passes |
| the declaration refuses at mount | four `kit/rest` gate cases | passes |
| same body in every language | `TestTheSummaryBodyIsTheSameBytesInEveryLanguage` (the second read is cached) | passes, weakly |
| Portuguese answers the label | `TestEveryDeclaredHintStringIsAnsweredInPortuguese` | passes |

## Look here first (carried forward; this round changed none of it)

1. **A refused or cancelled statement aborts the request's transaction**, so `/api/v1/app/summary` answers 500
   rather than a partial body. This deviates from SPECIFY §4.3/§4.4 on purpose: a partial body needs a
   savepoint or a transaction per entry, and the kernel has neither. The defence is the mount gate. The review
   should decide whether a per-entry transaction is owed now.
2. **The cache sequence**: `Readable` runs before the key is formed. `TestACountIsNeverServedPastItsReadGuard`,
   the case that would catch an inversion, is **not written**. It needs a member persona whose grant can be
   removed, because the administrator role answers grants without reading the row.
3. `CountDays` groups by the whole expression (gorm quotes `GROUP BY 1` into `"1"`).
4. `sentence`/`sentences` return the `*summaryFace` as a sixth value.
5. `kit/rest.coerce` is now an alias of `crud.Coerce`.

## Not verified

- `TestDeliveryPreservesAnIPv6EndpointPort` in this VM, which has no IPv6. See Result.
- A second full `make check` on the final candidate (see above). `make e2e`, the mobile flows (`MAESTRO_JUNIT`
  unset, skipped here as on the host) and the design floor (this delivery renders no page).
- Cases the spec names that are still unwritten: `TestACountIsNeverServedPastItsReadGuard`, a forced-short
  budget proving absence is stored nowhere, `TestASecondRequestWithinTheLifetimeAsksTheDatabaseOnce`, and
  `TestADueTodayCountChangesAtTheUTCMidnight`.
- Not reachable from this VM: the architecture record and decision 0087's table under `/home/jplr/…/platformkit-record`,
  `state/shelf`, and the old-platform repositories on 192.168.1.200. No shelf claim was re-checked this round,
  and this round added no code, so none was owed.
- `origin/main` on GitHub has moved to `93d3052` since `8020ac5`. Nothing was merged in this round. Whether the
  loop's own `origin/main` has also moved is the loop's to see.
