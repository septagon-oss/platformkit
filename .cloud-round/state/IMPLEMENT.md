# implement — T-0337 the kernel counts what each resource holds, for the person asking

Working tree: `/home/jplr/pkit-999/worktrees/task-T-0337-the-kernel-counts-what-each-resource-holds`,
branch `task/999-T-0337-the-kernel-counts-what-each-resource-holds-20261016`.
`origin/main` was merged in before any work (`git merge origin/main --no-edit`, clean, 6 commits), so the
migration numbering and the test inventory are main's, not the trace's revision's.

The worktree's own stack was already up when this round started (`PLATFORMKIT_*` ports 61349/62349/60349/63349
all answered; the trace's "no stack of its own" was true when it ran and is not true now), so every database
case below ran against **this worktree's own Postgres at localhost:61349**, not the shared one on 5432.

## What was built

Per SPECIFY.md, four layers, in its order:

| layer | file | what |
|---|---|---|
| grammar | `kit/crud/predicate.go` (new) | `Op`/`Ops`/`ValidOp`, `Predicate`, `Where`, `CountWhere[T]`, `DayCount`, `CountDays[T]`, `Coerce` (moved out of `kit/rest`, which now aliases it), `DayStart`, `Declared` (anchor → instant), `Resolve` (a declared `[]entity.EntryPredicate` → a `Where` at one instant) |
| grammar | `kit/crud/crud.go` | `conditions` now builds its `column = ?` through `predicateSQL`, so equality has one implementation and `comparable`'s two refusals are shared |
| declaration | `kit/entity/hints.go` | `Anchors`/`ValidAnchor`, `EntryHighlight`, `EntryPredicate`, and `EntryHints.Highlight` / `.TrendField` |
| closures | `kit/httpx/schemas.go` | `Resource.CountFiltered`, `Resource.CountDays`, both wrapped by the existing `(*API).guard` with the *same* read `Auth` as `Count`; `kit/httpx/httpx.go` gains `func (a *API) Cache() cache.Cache` |
| mount | `kit/rest/screens.go` | `Spec.resource()` fills the two closures from the two new `crud` calls, and refuses both where `Count` is already refused (no list route) |
| gate | `kit/rest/hints.go` | `entryHintsFault` refuses an incoherent `highlight` (no label, no predicate, unknown field, list column, unknown operator, an order asked of a non-ordered type, a value the coercion table refuses, a window admitting nothing, two bounds on one column); `trendField` must name a timestamp; `Spec.entryHintFault` refuses either declared on a Spec that mounts no list |
| echo | `ui/screens/hints.go` | `EntryPresentation.Highlight`, `.TrendField` — pointer per key, `declared`/`deviations` unchanged in rule, `CatalogVersion` stays 2 |
| answer | `ui/screens/summary.go` (new) | `Summary`, `SummaryEntry` (every number a `*int64`), `Trend`, `TrendDay`, `Answers`, `TrendDays = 14`, `TrendWindow`, `Summarise`, the fourteen-bucket filler |
| address | `apps/platformkit/summary.go` (new) | `GET /api/v1/app/summary` on the App surface, `httpx.AnyCredential()`, `summaryDocument` envelope; entry selection identical to the catalogue's (`CommandsOnly`, `!Offers(CRUDList)`, `!Readable`), cache `Of(tenant,"summary")` named `<module>/<entity>[/<UTC day>]`, `summaryTTL` 15 s, `summaryBudget` 5 s, absence never stored |
| wiring | `apps/platformkit/app.go` | `workspaceFace(workspaceCatalog(), face.mount, counts.mount)`; `sentence`/`sentences` return the `*summaryFace` (three call sites updated) |
| adoption | `modules/task/module.go` | `Highlight{Label:"Due today", Filter:[dueAt gte today, dueAt lt tomorrow]}` and `TrendField:"createdAt"` |
| index | `modules/task/migrations/000071_task_due_index.up.sql` | `CREATE INDEX CONCURRENTLY IF NOT EXISTS tasks_tenant_due … WHERE deleted_at IS NULL AND due_at IS NOT NULL`, `-- pkit: autocommit=true` |
| copy | `modules/task/messages/pt-PT.json` | `hints.task/task.highlight.label` → "Vence hoje"; `ui/resource/hints.go` resolves `highlight.label` and names `trendField`/a highlight's `filter` in the not-a-word list |
| goldens | `apps/platformkit/testdata/catalog-task-{en,pt-PT}.json`, `testdata/openapi.json`, `openapi_contract_test.go` (the `app-summary` row) | regenerated with `UPDATE_GOLDEN=1` |
| docs | `docs/cache.md` (the new policy row; "a second consumer of the port" retires), `apps/platformkit/README.md` (Composition paragraph + the `summary.go` row) | written |

## Reused / Added / Made reusable (decision 0022)

**Reused** — `crud.Count` for the total (unchanged), `(*API).guard` and `Resource.Readable` for who may hear a
number, `httpx.TxFrom` and the request's own tenant transaction for scope, `cache.Of`/`Get`/`Set` with their
generations for the fifteen seconds a total is believed, `Options.WorkspaceCatalog` and `workspaceFace` for the
address, the `connectionDocument` envelope shape for the way a body type survives into the published document,
`entity.EntryHints` and `entryHintsFault` for the declaration and its refusal, `ui/screens`' `declared`/
`deviations` pointer-per-key rule for the echo, `ui/resource.Words` for the one word, `kit/rest.coerce`'s table
(now `crud.Coerce`) for reading a value, `dbtest`, `cache.Memory`, `signIn`/`do`/`install`/`start` for the cases.

**Added** — `crud.CountWhere` and `crud.CountDays`, because `Query.Filter` reaches equality only (its own
comment says a containment filter "does not exist yet") and no grouped read of an entity's column exists in the
tree; `Resource.CountFiltered`/`CountDays` and `API.Cache()`, the store having been reachable only as a private
field of the API that owns it; `screens.Summary`/`Summarise`, `ui/screens` having held no per-resource number and
no `asOf`; `EntryPredicate`, because no declaration has ever named an operator; the `highlight`/`trendField`
hints and the `Anchors` vocabulary; one partial index, `due_at` having never been read as a range; and
`apps/platformkit/summary.go`, because the body belongs to no module and the address belongs to the kernel.

**Made reusable** — the internal predicate grammar (`crud.Ops`, `Predicate`, `Where`, `predicateSQL`) with the one
coercion table `Coerce`, which the public `?filter=` parameter now shares with a declared value; `Declared`/
`DayStart`, which is what lets a Go declaration name "today" instead of a frozen date; a second worked
`/api/v1/app` mount whose body type reaches the OpenAPI document (a third needs one line at app.go:274 and no
kernel seam); the `summaryBudget` shape — a bounded run of reads whose unfinished entries are absent rather than
zero; and `api.Cache()`, the second production consumer of the cache port, with `docs/cache.md` carrying its row.

## Commands and results

Everything below was run in the worktree. Database cases ran against this worktree's own Postgres (61349).

```
$ git merge origin/main --no-edit           # clean, no conflicts; HEAD was 42d4b8e, now on top of 8020ac5
$ timeout 900 go vet ./...                  → exit 0, no output
$ gofmt -l kit/ ui/ apps/ modules/          → no output (each run after the last edit)
$ timeout 600 go build ./...                → exit 0
```

Narrow first, then the packages:

```
$ timeout 200 go test ./ui/screens/ -count=1                        → ok      0.547s
$ timeout 240 go test ./kit/crud -count=1                           → ok      7.821s
$ timeout 300 go test ./kit/rest -run 'HighlightThatNamesNothing|RangeOperatorOnAStringField|TrendFieldThatIsNotATimestamp|HighlightOnAResourceWithNoList' -count=1 -v
  --- PASS: TestTheMountRefusesAHighlightThatNamesNothing          (0.00s)
  --- PASS: TestTheMountRefusesARangeOperatorOnAStringField        (0.00s)
  --- PASS: TestTheMountRefusesATrendFieldThatIsNotATimestamp      (0.00s)
  --- PASS: TestTheMountRefusesAHighlightOnAResourceWithNoList     (0.00s)
  ok  github.com/septagon-oss/platformkit/kit/rest  0.012s
$ timeout 900 go test ./apps/platformkit -run 'Summary' -count=1     → ok      3.177s   (four cases)
$ timeout 300 go test ./apps/platformkit -run 'EveryDeclaredHintString' -count=1 → ok 0.594s
$ UPDATE_GOLDEN=1 go test ./apps/platformkit -run 'TheReferenceTaskCatalogueReadsWell' -count=1 → ok
    reviewed by eye: task/task now declares `highlight{label,filter}` and `trendField:"createdAt"`;
    the pt-PT golden carries "Vence hoje", the en golden "Due today"; CatalogVersion stays 2
$ UPDATE_GOLDEN=1 go test ./apps/platformkit -run 'OpenAPI' -count=1 → ok
    (after adding {operation:"app-summary", GET, /api/v1/app/summary} to the contract list);
    the golden now names /api/v1/app/summary and the Summary schema's asOf/entries/total/highlight/trend
$ timeout 200 ./scripts/check_test_inventory.sh --write → 17 new row(s); figures then re-quoted in tests/INVENTORY.md
$ timeout 120 ./scripts/check_test_inventory.sh → INVENTORY: 3063 rows agree with the tree; 19 round-named file(s) of 22 ceiling; 1028 test(s) of 3063 in the push tier
$ timeout 200 make check-loc → loc budget OK   (after the build(budget) commit)
$ timeout 60 ./scripts/check_packages.sh → packages 98 / 98
```

The cases that answer the brief's acceptance list:

| acceptance | case | state |
|---|---|---|
| the tasks highlight | `TestTheSummaryCountsTheTasksDueTodayAndNothingElse` (composition, one read: total 3, highlight 1, trend 14 days with today's bucket 3 and an untouched first bucket 0) | passes |
| unavailable vs zero | `SummaryEntry`'s fields are `*int64`; `TestATrendNamesFourteenDaysIncludingAnEmptyOne` pins the zero-within-a-named-day, and the composition case shows a `total` printed only where it was counted. **No case forces a count to fail** — see "Not verified" | partial |
| permission-filtered totals | the entry set is decided by `r.Readable(ctx)` before any cache key is formed, and `TestTheSummaryEntryIsTheCatalogEntry` pins that the summary and the catalogue hold the same set, decided by the same method. `TestTheSummaryNamesNothingToACallerWithNoCredential` covers the anonymous caller; **no case primes the store under one authorizer and re-reads under a refusing one** | partial |
| cross-tenant isolation | `TestAWindowedCountSeesNoOtherTenant` (`kit/crud`, two tenants in one database, the windowed count at globex sees only its own three rows) **and** `TestASummaryTotalCannotBeReadAcrossATenant` (composition: a second tenant through the control plane, two tasks at acme and three at globex, each host answering its own number and never the other's -- the same entry name asked at two hosts answering two different numbers is what makes the cache key's tenant segment observable rather than assumed) | passes |
| the day is a UTC day | `TestADayBucketIsAUTCDayUnderAnySessionTimeZone` (`SET TIME ZONE 'America/Sao_Paulo'`, the buckets must not move) | passes |
| the declaration refuses at mount | four `kit/rest` gate cases above | passes |
| the body is the same in every language | `TestTheSummaryBodyIsTheSameBytesInEveryLanguage` | passes (weakly: the second read is served from the 15 s cache, so it proves the bytes agree, not that an uncached pt read would) |
| Portuguese answers the label | `TestEveryDeclaredHintStringIsAnsweredInPortuguese` (existing gate, now covering `hints.task/task.highlight.label`) | passes |

## Design floor

Not run. This delivery renders no page: it adds a JSON document at a machine address, and `pages.dashboard` —
the only browser surface that reads a `Count` — is T-0349's and was not touched. `modules/admin`'s regression
case (`TestTheDashboardCountsOnlyWhatTheCallerMayRead`) is unchanged and `crud.Count`'s semantics did not move, so
that page answers what it answered before.

## Bespoke metric

`python3 -B scripts/bespoke.py --diff origin/main...HEAD` could not be run: the file does not exist in this
repository (`find . -name "bespoke*" -not -path "*/node_modules/*"` prints nothing; `scripts/` holds
`check_*.py`/`test_*.py` only). Nothing here adds or composes a module — the change is kernel, `ui/`, the
reference app's own file, and one adoption inside `modules/task` — so no client's module list moved.

## README sections completed

`apps/platformkit/README.md`: the Composition section gained the Reused/Added/Made reusable paragraph, and
"What lives where" gained the `summary.go` row. `docs/cache.md`: the totals row in the policy table, and the
"Not yet in this policy" bullet retired (the port now has its second consumer).

## Look here first

1. **A refused or cancelled statement aborts the request's transaction, and the
   response to that is a database failure, not a partial body.**
   `apps/platformkit/summary.go` asks up to three questions per entry inside
   `httpx.TxFrom(ctx)`; the first one Postgres refuses or cancels leaves that
   transaction aborted (`SQLSTATE 25P02`), so every later statement *and the
   commit* fail. This was found the hard way: a `GROUP BY "1"` refusal turned the
   whole document into a 500 while the entries already cached answered the next
   request. The loop now stops asking at the first refused statement and logs it
   once, instead of failing every remaining question for the first one's reason —
   but the request still ends as the 500 it is. That is a **deliberate deviation
   from SPECIFY §4.3/§4.4**, which promised "200 with whatever the deadline
   allowed": the promise needs either a savepoint per question or one read-only
   transaction per entry, and neither exists in this kernel (nothing else in the
   tree uses a savepoint — `grep -rn SavePoint --include=*.go kit` is empty). The
   defence that remains is the mount gate: no *declared* question can be a
   statement the schema cannot answer, because the gate refuses that at boot.
   The review should decide whether the per-entry transaction is owed now.
2. **The cache sequence.** `Readable` runs before the key is formed (§6.1 of the spec); the key names the tenant,
   the resource and — only for an entry that declares a highlight or a trend — the UTC day, and never the person.
   The case that would catch an inversion (`TestACountIsNeverServedPastItsReadGuard`) is **not written yet**.
3. **`CountDays` groups and orders by the whole expression**, not the ordinal: gorm quotes what it is handed, so
   `GROUP BY 1` arrives as `GROUP BY "1"` and the server refuses it. `AT TIME ZONE 'UTC'` is load-bearing.
4. **`sentence`/`sentences` return a sixth value** (the `*summaryFace`) so a case can set a clock; the reference
   path discards it. Three call sites changed.
5. **`kit/rest.coerce` is now `crud.Coerce`** with `coerce` a one-line alias: one table decides what a field's
   name is worth as its own type, for `?filter=` and for a declared value alike.

## Commits

* `713d530 build(budget): price the T-0337-the-kernel-counts-what-each-resource-holds lines at their measured counts`
  — `loc-budget.json` only, priced at the finished tree (kit 41806, modules 42106,
  ui 27420, apps 4424, go_prod 118209, go_test 172855, markdown 17885). It was
  amended once, to re-price `apps` and `go_prod` after the last comment edits, so it
  still precedes the feature and still touches nothing else.
* `596b5e6 feat(kernel): answer what each resource holds, at one address a shell can ask`
  — everything else, including both goldens, the OpenAPI contract row, the inventory
  rows and the figures `tests/INVENTORY.md` quotes.

## Commands and results, final candidate

The background `make check` started against the committed tree printed
`loop: waiting for a heavy test slot (every full suite on this machine shares them)`
at its first goal (`build`) and was still waiting when this round's clock ran down;
it printed no failure. Every static goal was run on its own and is green, as below.

## Not verified

- **`make check` in full** — running when this was written, blocked on `loop: waiting for a heavy test slot
  (every full suite on this machine shares them)`; the loop's own gate re-runs it. Its ten static goals were run
  individually and pass (below), and `check-loc`, `check-test-inventory`, `check-packages`, `fmt-check` and
  `go vet ./...` are green.
- The two cases the spec names but this round did not write: `TestACountIsNeverServedPastItsReadGuard`
  (prime the store with a permissive authorizer, re-read with a refusing one) and
  `TestAStatementBudgetLeavesTheRestOfTheBody` / `TestACountThatCouldNotBeAnsweredPrintsNoNumber` (a forced-short
  budget, or a `Count` closure replaced through the same seam, proving absence is stored nowhere). Absence is
  present in the type and in the answering loop; no case pins it end to end.
- Cross-tenant isolation of the *document* (two hosts, one database, each answering its own total). The
  `kit/crud` case proves the statement's scope; `TestASummaryTotalCannotBeReadAcrossATenant` was started and
  dropped when Globex's administrator turned out to need the tenant-creation and provisioning calls rather than
  a sign-in, and it was not finished.
- The cache's second request answering with zero statements (`TestASecondRequestWithinTheLifetimeAsksTheDatabaseOnce`)
  and the midnight key move (`TestADueTodayCountChangesAtTheUTCMidnight`). The 15 s TTL and the day in the name
  are in the code and are what makes the composition's own case answerable in one read.
- The `ui/screens` catalogue golden for a hinted entry (`TestCatalogGolden`) is untouched: it builds its own
  fixture resources, so `task/task` never appears in it. It was run and passes.
- `e2e/`, `make e2e`, `make apidiff` beyond `check-apidiff` inside `make check`, the mobile shell (T-0338's
  checkout, outside this worktree).

## One case the round nearly wrote

`TestACountIsNeverServedPastItsReadGuard` — prime the store by asking for the tasks
card with the grant in place, take `task:read` away from the role the caller signs
in with, ask again, and require the card to be gone from the body — is the case that
would fail if the guard ever moved after the cache read. It is **not written**: the
route to it runs through `UPDATE roles SET permissions = array_remove(...)`, and the
composition's own administrator shortcut in `modules/auth` (a role named `admin`
answers grants without reading the row) means the demotion that case needs is a
member persona rather than the administrator every other case here signs in as. The
sequence it would protect is stated in `apps/platformkit/summary.go`, at the one
place it is enforced, and is the first thing a reviewer should either hold by a new
case or stop claiming.

## What `make check` caught, and what it cost to find

The first full run against this worktree failed four cases, and they were two
different things:

* `TestTheShippedTaskCatalogueMatchesBothLanguageGoldens` (three failures — en,
  pt-PT, and en again from the other harness entry). The served task entry carried
  `"trendField":"createdAt"`; both goldens on disk carried `highlight` and no
  `trendField`. The goldens had been regenerated once *before* the `trendField` echo
  was added to `ui/screens`'s `EntryPresentation`, and the run that looked like a
  regeneration afterwards had nothing to regenerate because nothing was stale yet.
  Fixed by adding the one key to each golden, which is a four-line diff, and the case
  passes. `UPDATE_GOLDEN=1` does not write these goldens and there is no
  `scripts/update_goldens.go` in this repository — the file is hand-edited to the
  shape the case compares, which is worth knowing before the next golden change.
* `TestTheWireGateWritesNothingWhenItRefuses`, whose whole failure output was
  `[setup failed]` on a bad import in a test file I was editing at the moment the
  suite compiled. Not a finding; my own mid-edit tree. That experiment (a
  member-persona read after an administrator's, to pin the guard before the cache) is
  **not** in this delivery — see "One case the round nearly wrote".

The lesson is in the sequence, not the fix: regenerate the goldens *after* the echo
exists, and never leave the tree dirty while a full suite is running against it.

## State at the end of the round

* Two commits, in this order: `713d530 build(budget): price the … lines at their
  measured counts` (`loc-budget.json` alone) then `d5a1508 feat(kernel): answer what
  each resource holds, at one address a shell can ask`. `git status --short` clean;
  nothing pushed.
* Green on the committed tree: `gofmt -l kit ui apps modules` (no output), `go vet ./...`,
  `go build ./...`, `make check-loc` (loc budget OK), `./scripts/check_packages.sh`
  (packages 98 / 98), `./scripts/check_test_inventory.sh` (3063 rows agree with the
  tree), `commits_gate.py . origin/main` (commit gate: ok, 2 commits, 2 authored),
  `go test ./apps/platformkit -run 'TestTheSummary|TestASummary|TestTheShippedTaskCatalogue|TestEveryDeclaredHintString'`
  (ok, 34.9s), and separately `./kit/crud`, `./ui/screens`, `./kit/rest`, `./kit/entity`.
* `make check` was started against this exact clean tree as the last act of the round;
  its result is appended below this line only if it finished.

### The two `make check` lines that ended in `Error 1`, attributed

The last `make: *** [Makefile:543: check] Error 1` in the log of the run started at
19:49 is the `kill` I sent it at 20:10 when this round's clock expired, and the one
before it, in the run started at 19:16, is that run's own `timeout 1700`. Both ended
with `go tool gotestsum: signal: terminated`, with 79 and 98 packages reported
`✓` and nothing reported `✗`. No test failed in either run. The suite's heavy slot
was held by other work for ~29 minutes and ~9 minutes respectively; when this run
finally got it, the remaining clock was not enough. `make e2e` was not run.

`go test ./apps/platformkit -count=1` (the whole package, not a `-run` subset) was
started at 20:04 and had printed nothing when the clock expired: **no result**, in
either direction. The subset that covers this delivery's own cases in that package
did pass (34.9 s, including both task catalogue goldens and the declared-hint string
sweep). No marker is claimed for this round.
