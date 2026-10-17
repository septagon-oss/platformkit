# TRACE — T-0337 the kernel counts what each resource holds, for the person asking

Read-only trace of `/home/jplr/pkit-999/worktrees/task-T-0337-the-kernel-counts-what-each-resource-holds`
at `42d4b8e8c21d4b7a512d2437c20ee5c729dbecc0` ("Merge pull request 'feat(catalogue): declare picture and
row-action hints' (#213) …", committed 2026-10-16 14:21:36 +0100). Every path and line range below was read
in that checkout in this round.

## Resolved pins

```
$ go list -m -json github.com/septagon-oss/platformkit
{
	"Path": "github.com/septagon-oss/platformkit",
	"Main": true,
	"Dir": "/home/jplr/pkit-999/worktrees/task-T-0337-the-kernel-counts-what-each-resource-holds",
	"GoMod": "/home/jplr/pkit-999/worktrees/task-T-0337-the-kernel-counts-what-each-resource-holds/go.mod",
	"GoVersion": "1.26.6"
}

$ go list -m -json github.com/septagon-dev/platformkit-catalog
go: module github.com/septagon-dev/platformkit-catalog: not a known dependency
```

- **platformkit is this checkout.** `go.mod:1` is `module github.com/septagon-oss/platformkit`; `go 1.26.6`,
  `toolchain go1.27.2`. There is no `replace` line (`grep -n "replace" go.mod` prints nothing) and no
  `go.work` (`ls go.work*` → "No such file or directory"), which is what `make check-versions`
  (`Makefile:260`) enforces. So the revision this task's consumer uses is the commit above, not a sibling.
- **platformkit-catalog is not a dependency of this module at all** — it appears in no `go.mod` line
  (`grep -n septagon go.mod` prints only line 1, the module path). Nothing in this brief reads it.
- The brief's `requires: [task-T-0331]` is satisfied inside this checkout's history: HEAD *is* the merge of
  T-0331 (`git log --oneline -1` → `42d4b8e feat(catalogue): declare picture and row-action hints`), and its
  result is readable: `kit/entity/hints.go:98-105` (`CoverField`, `ThumbnailField` at 104-105),
  `kit/entity/hints.go:107-110` (`PrimaryCommand`), `kit/entity/hints.go:169-176` (`DoneValues`),
  `ui/screens/hints.go:47-49` (the same three on the wire).
- **There is no `apps/flagship/registry.go` in this repository**, and no `apps/platformkit/modules.go` either
  (AGENTS.md names the latter; it does not exist here). `find . -name registry.go -not -path "*/node_modules/*"`
  prints nothing, and `grep -rn flagship --include=*.go .` prints nothing. The reference application is
  `apps/platformkit`, whose module list is written inline in `apps/platformkit/app.go` (the `.Use(...)`
  list ending `change.Module, admin.Module` at `apps/platformkit/app.go:263-265`, with every module imported
  at `apps/platformkit/app.go:44` and neighbours). Record the shape, not the template's name.
- The phone that will consume this document is a **sibling checkout, not a pin of this one**:
  `/home/jplr/gitrepos/pkit/platformkit-mobile` at `75ab6f2`. It reads the catalogue in
  `src/core/catalog.ts` (249 lines) and renders Home in `src/screens/Home.tsx`
  (`app/index.tsx:19` passes `state.catalog?.resources ?? []` to `<Home entries=…>`). It pins no OpenAPI
  file: `find . -name "openapi*.json" -not -path "*/node_modules/*"` in that checkout prints nothing.

## Existing owner and consumer

**The owner of "what a resource holds" is `kit/httpx`'s `Resource.Count`, filled by `kit/rest` from
`kit/crud.Count`, and it already has one consumer and one guard.**

| Layer | Path and lines | What it owns |
|---|---|---|
| Storage | `kit/crud/crud.go:131-135` | `Count[T Entity](tx db.Tx[db.Tenant]) (int64, error)` — "the total live rows visible to this tenant without loading a page": `Model(blank[T]()).Where("deleted_at IS NULL").Count(&total)`. No filter parameter, no grouping, no operator. |
| Query grammar | `kit/crud/crud.go:92-97` (`Query{Limit, Offset, Sort, Filter map[string]any}`), `:343-355` (`conditions` emits only `column = ?` per filter key), `:374-388` (`comparable` refuses a name outside the schema and any `TypeList`) | Equality filtering only. `kit/crud/crud.go:374-379` says so itself: "a containment filter is a different operator and it does not exist yet". |
| HTTP grammar | `kit/rest/rest.go:1492-1529` (`listInput`, `?filter=field:value` repeated, `query:"filter"`) | The public filter vocabulary — `field:value`, coerced to the column's type, equality only. |
| Closure | `kit/rest/screens.go:68-75` | `Spec.resource()` fills `httpx.Resource.Count` as `crud.Count[T](tx)` in the request's own transaction. `kit/rest/screens.go:150-156`: a Spec that mounts no list route replaces `Count` with a refusal — "Count goes with the list, not with the read. Its one caller outside this package is the dashboard card". |
| Guard | `kit/httpx/schemas.go:129-131` (the field), `:304-327` (`(*API).guard`) | Every `Count` is wrapped in `a.mayDeclare(ctx, read)` before it runs: *the same* method the list, the get and the catalogue entry ask, so "totals respect tenancy and permissions exactly as the list does" is already the rule at this seam. A `Count` that is nil falls back to `list(ctx, crud.Query{Limit: 1})`'s total (`kit/httpx/schemas.go:313-319`). |
| Consumer (the only one) | `modules/admin/internal/pages.go:520-546` | `pages.dashboard` loops `p.resources`, skips `r.Count == nil`, calls `total, err := r.Count(ctx)` (`:536`), and draws nothing at all — not even the resource name — when the call errors: "A refused or unavailable count produces no card". Its output is the card 0085 complains about: `count + " " + rest.Humanize(r.Entity) + "s"`, `Description: "In " + r.Module` (`:540-542`). |

**The owner of "the address a shell is written against" is the composition, through one kernel seam.**

- `kit/app/app.go:117-131` — `Options.WorkspaceCatalog func(api *httpx.API)`: "the kernel composes the
  address (no module composes `/api/v1/app/…`, and the address is a native shell's contract, not a
  capability's), and only ui can render the answer — and kit may not import ui".
- `kit/app/app.go:1052-1064` + `:1089-1113` — `WorkspaceCatalogRoute[T]` / `mountWorkspaceCatalog`: registers
  `OperationID: "app-resources"`, `GET /resources` on `api.Surfaces("").App` behind `httpx.AnyCredential()`,
  and carries the body's **type** into huma so the published OpenAPI names the fields a shell parses.
- `apps/platformkit/fault.go:272-286` — `workspaceFace(routes ...func(api *httpx.API))`: "app.Options.WorkspaceCatalog
  is the only seam that hands a composition the `*httpx.API` of `/api/v1/app/…` … it takes an arbitrary mount
  function, so a second such route needs no new kernel seam, only this line."
- `apps/platformkit/app.go:273` — that one line today: `WorkspaceCatalog(workspaceFace(workspaceCatalog(), face.mount))`.
- `apps/platformkit/connection.go:66-116` + `:104-116` — `connectionFace.mount` is the **worked example of a
  second module-less `/api/v1/app` document**: its own `httpx.Register(api.Surfaces("").App, …)` with a
  declared guard, its envelope struct `connectionDocument{Body *screens.Connection}` (`:104-110`, "a body
  handed to huma as `any` leaves its fields out of the published document"), and its read taken from
  `httpx.TxFrom(ctx)` so "row-level security scopes every query with no help from here".
- The gate that keeps such a door honest: `apps/platformkit/openapi_contract_test.go:66-88` lists every
  operation a shell may be generated against (`app-resources`, `app-connection` …), and
  `TestTheOpenAPIDocumentIsTheCompositionServed` (`:91-105`) fails the golden document otherwise.

**The owner of the hint contract is `kit/entity` + `kit/rest` + `ui/screens`, in that order.**

- `kit/entity/hints.go:76-115` — `EntryHints` (the declaration a Spec writes): `Singular`, `Plural`,
  `Description`, `Icon`, `Group`, `Order`, `PrimaryField`, `PreviewField`, `SummaryFields`, `StatusField`,
  `CoverField`, `ThumbnailField`, `PrimaryCommand`, `Sections`, `EmptyDescription`, `Sortable`. No
  `Highlight`, no `TrendField`.
- `kit/rest/hints.go:31-135` — `entryHintsFault` is the mount gate that refuses an incoherent entry declaration
  against the entity's own fields (`kit/rest/hints.go:383` `CheckReferences` and `:421` `CheckRowHints` are
  the whole-list questions, called at boot from `apps/platformkit/fault.go:259-269`).
- `ui/screens/hints.go:33-50` — `EntryPresentation`, one pointer per key; `ui/screens/hints.go:79-100`
  `deviations` — "a key is on the wire because its author put it there, and never because the server had an
  opinion about what it would have been".
- `ui/screens/catalog.go:34` `CatalogVersion = 2`; `:40-45` `Catalog{Version, Resources, Pictures}`;
  `:92` `Entry.Presentation *EntryPresentation \`json:"presentation,omitempty"\``; `:172` `Describe`,
  `:250` `Describe1`.
- Copy resolution: `ui/resource/hints.go:10-27` — a declared reading *string* is keyed
  `hints.<module>/<entity>.<aspect>` and resolved through `Words.Entry` (`ui/resource/hints.go:128`);
  the vocabulary list at `:29-34` names what is deliberately **not** resolved because a screen draws it.
  A module's second language sits beside it (`modules/task/catalogue.go:10-17`,
  `modules/task/messages/pt-PT.json`).

**The product's adoption point** is `modules/task/module.go:96-111` — `Spec.Present: entity.EntryHints{…}`
(`Singular: "task"`, `Icon: "task"`, `Group: {work, "Work"}`, `PrimaryField: "title"`,
`SummaryFields: {priority, dueAt}`, `Sortable: {title, dueAt, updatedAt}`). The task's `dueAt` and
`createdAt` are the fields the brief wants to highlight and trend; the module's migrations are
`modules/task/migrations/000004_task.up.sql` and `000050_task_revision.up.sql`, adopted through
`modules/task/module.go:197-198` (`Migrations: Migrations.Files`, `Adopts`).

**Existing tests that already pin the shapes the acceptance list asks for** (reuse these, do not invent):

- `kit/rest/count_test.go:29` `TestResourceCountChecksPermissionOnceAndLoadsNoRows` — one authorization
  decision, one `COUNT(*)`, no entity rows, and 403/503 for a denial/an unavailable authorizer.
- `modules/admin/module_test.go:581` `TestTheDashboardCountsOnlyWhatTheCallerMayRead` — permission filtering.
- `kit/rest/withheld_list_test.go:37` `TestACountOfAResourceWithNoListRouteRefusesWhereItsCardWouldAnswer` —
  unavailable is a refusal, not a zero.
- `kit/rest/signed_in_closure_tenant_test.go:21` `TestASignedInResourceClosureCannotReadAnotherTenantsRow` —
  cross-tenant isolation of a closure reached as a signed-in caller.

## Boundary check

The boundary this task must move is the presentation contract as the catalogue writes it, so the check ran there:

```
$ cd /home/jplr/pkit-999/worktrees/task-T-0337-the-kernel-counts-what-each-resource-holds
$ go test ./ui/screens/ -run 'Presentation|Hints|Catalog' -count=1 -v
…
--- PASS: TestTheCatalogNamesOnlyTheWriteDoorItCannotDerive (0.00s)
--- PASS: TestCatalogGolden (0.00s)
--- PASS: TestTagsDeclareTheSameHintsAsHandWrittenFields (0.00s)
--- PASS: TestTheRouteStampsTheCatalogVersion (0.00s)
PASS
ok  	github.com/septagon-oss/platformkit/ui/screens	0.011s
```

Everything the hint half of this brief touches mounts, prints and golden-matches today.

The count half needs the test database, and **it does not answer in this worktree**: the loop allocated
`PLATFORMKIT_PG_PORT=61349`, `PLATFORMKIT_NATS_PORT=62349`, `PLATFORMKIT_S3_PORT=63349`,
`PLATFORMKIT_VALKEY_PORT=60349` for this task and started none of them
(`timeout 30 docker ps --format '{{.Names}} {{.Ports}}' | grep -E "61349|62349|63349|0337"` prints nothing,
and `docker ps -a | grep -i 0337` prints nothing). The first run, left at the ports the environment gave this
worktree, therefore failed on the connection rather than on the code:

```
$ go test ./kit/rest/ -run TestResourceCountChecksPermissionOnceAndLoadsNoRows -count=1 -v
=== RUN   TestResourceCountChecksPermissionOnceAndLoadsNoRows/allowed
    count_test.go:42: dbtest: DROP SCHEMA IF EXISTS "t_1axm1vtbs1abv_testresourcecount…":
        failed to connect to `user=postgres database=platformkit`:
        [::1]:61349 (localhost): dial error: dial tcp [::1]:61349: connect: connection refused
…
--- FAIL: TestResourceCountChecksPermissionOnceAndLoadsNoRows (0.00s)
FAIL	github.com/septagon-oss/platformkit/kit/rest	0.015s
FAIL
```

That failure is the missing stack, not the code: `kit/db/dbtest/dbtest.go:50-51` reads
`PLATFORMKIT_TEST_ADMIN_URL` / `PLATFORMKIT_TEST_DATABASE_URL` and refuses without them. An attempt through
`make test … PLATFORMKIT_PG_PORT=5432` never left the host's shared queue
(`loop: waiting for a heavy test slot (every full suite on this machine shares them)`), so the count boundary
was run directly against the host's own test database on the default port instead, and it passes:

```
$ PLATFORMKIT_PG_PORT=5432 \
  PLATFORMKIT_TEST_ADMIN_URL='postgres://postgres:platformkit@localhost:5432/platformkit?sslmode=disable' \
  PLATFORMKIT_TEST_DATABASE_URL='postgres://platformkit_app:platformkit@localhost:5432/platformkit?sslmode=disable' \
  go test ./kit/rest/ -run TestResourceCountChecksPermissionOnceAndLoadsNoRows -count=1 -v
--- PASS: TestResourceCountChecksPermissionOnceAndLoadsNoRows (0.67s)
    --- PASS: TestResourceCountChecksPermissionOnceAndLoadsNoRows/allowed (0.30s)
    --- PASS: TestResourceCountChecksPermissionOnceAndLoadsNoRows/denied (0.24s)
    --- PASS: TestResourceCountChecksPermissionOnceAndLoadsNoRows/unavailable (0.12s)
PASS
ok  	github.com/septagon-oss/platformkit/kit/rest	0.681s
```

So both halves of the boundary are executed and green at this revision: the existing count asks one
authorization question, issues one `COUNT(*)` and loads no rows, and separates allowed / denied / unavailable;
the presentation contract mounts, prints and golden-matches. What remains unverified is everything this task
would *add* — no filtered count, no grouped count and no summary door exists to run. The implementation round
must bring up this worktree's own stack (the ports the loop allocated — 61349/62349/63349/60349 — have nothing
behind them) or keep pointing the suite at a database it names explicitly, as above.

## Gap

The kernel can count a whole resource for a caller and can say nothing else about it. `httpx.Resource.Count`
(`kit/httpx/schemas.go:131`, guarded at `:320-327`, filled at `kit/rest/screens.go:68-75`) takes only a
context and answers `crud.Count[T]` (`kit/crud/crud.go:131-135`), whose one predicate is
`deleted_at IS NULL` for the tenant's rows; `crud.Query`'s `Filter` is a `map[string]any` that
`conditions` (`kit/crud/crud.go:343-355`) turns into `column = ?` and nothing else — so a "Due today" total
(a range on `dueAt`) is not a question the existing query layer can be asked, and `crud.Count` has no
parameter to ask it with. A 14-day daily breakdown needs a `GROUP BY` over a truncated timestamp, and
`grep -rn "date_trunc\|GROUP BY" --include=*.go kit modules apps` returns one production hit
(`kit/events/ledger.go:541`, the tenant-migration tally) and nothing that reads an entity's column by day;
`kit/crud` has no grouping form at all. There is no address to answer at: `grep -rn "api/v1/app" --include=*.go kit pkit apps modules ui`
shows only `/resources`, `/connection`, `/access-requests`, and no `/summary` exists anywhere. And the
document has nowhere to declare the question — `entity.EntryHints` (`kit/entity/hints.go:84-115`) carries
seventeen entry hints and neither a highlight nor a `trendField`, so `ui/screens/hints.go:33-50` prints
neither and the mount gate (`kit/rest/hints.go:31-135`) refuses nothing about them. `asOf` exists in no Go
file (`grep -rn "asOf\|AsOf" --include=*.go kit apps ui` prints nothing). The caching half is *not* missing:
`kit/cache` is a port with generations, scopes, TTLs and a compose-on-miss `Group`
(`kit/cache/cache.go:182-238`, `kit/cache/group.go:76-92`) — but its only production consumer is host
resolution (`kit/httpx/tenant.go:164` and `:193`), and `cache.GroupOf` has none outside its conformance
harness (`grep -rn GroupOf --include=*.go .` → `kit/cache/group.go` and `kit/cache/cachetest/conformance.go`
only), so the brief's "cached briefly per tenant and person" is a *first use*, not a new mechanism.

**Reuse vs extension vs a new module.** This brief is not a new module, and nothing in it should be one:
there is no new capability, no new tenant-scoped state, no new permission, and no new contract — the person
asks a question the list already answers, about rows the list already reads. Decision 0038's test ("would
this add or grow a module only one client composes?") is not even reached. What is needed is (a) an
**extension of the existing `Resource.Count` path** — a filtered and a grouped count beside the unfiltered
one, in `kit/crud`, guarded by the same `mayDeclare(ctx, read)` that already wraps `Count` so tenancy and
grants cannot diverge from the list; (b) **two optional entry hints** (`highlight`, `trendField`) on
`entity.EntryHints`, refused at mount by `entryHintsFault` for the reason the seventeen existing hints are
refused, echoed by `ui/screens/hints.go`, resolved for language by `ui/resource/hints.go`; and (c) **one
more module-less route on the seam that already exists** — a `connectionFace`-shaped mount added to
`workspaceFace(...)` at `apps/platformkit/app.go:273`, mounted exactly as
`mountWorkspaceCatalog` mounts `/resources` so the body type reaches the OpenAPI document, plus its entry
in `apps/platformkit/openapi_contract_test.go:66-88`. The only genuinely new file-shaped thing is the
response body type itself, and it belongs beside `screens.Catalog` and `screens.Connection` in `ui/screens`
because that is where every other `/api/v1/app` body lives. A summary in a module would have to compose a
prefix no module may compose (`kit/module/module.go:287`, `kit/httpx/surfaces.go:171`) and would see the
other modules' resources only through ports — the same argument `apps/platformkit/connection.go:1-19` gives
for the connection document, one level simpler here.

## Reuse inventory

Decision 0022. For every kind this task touches, the unit that is composed rather than written. The
specify phase may not add what is listed here.

| Kind | Existing unit (path · symbol) | State |
|---|---|---|
| Kernel mechanism: the count | `kit/crud/crud.go` · `Count[T]` (`:131-135`); `kit/rest/screens.go` · `Spec.resource()`'s `Count` closure (`:68-75`) | exists, unfiltered; extend with the predicate form, keep the closure shape |
| Kernel mechanism: the guard | `kit/httpx/schemas.go` · `(*API).guard` (`:309-327`), `Resource.Count` field (`:131`) | exists; every registered `Count` is already behind `mayDeclare(ctx, r.ReadAuth())` — reuse it, add no second authorization |
| Kernel mechanism: request transaction | `kit/httpx` · `TxFrom(ctx)`; the pattern at `apps/platformkit/connection.go:118-122` (`composition.describe`) | exists |
| Kernel mechanism: the filter grammar | `kit/crud/crud.go` · `Query.Filter` (`:95-97`), `conditions` (`:343-355`), `comparable` (`:380-388`); `kit/rest/rest.go` · `listInput.query` (`:1503-1529`) | exists for equality only; the range operator is the extension, and `comparable`'s field-name check is what a declared `highlight.filter` must pass |
| Kernel mechanism: the cache | `kit/cache` · `Cache` (`cache.go:182-238`), `Of(tenant, ns) Scope` + `Scope.Entry` (`cache.go:112-150`), `GroupOf[V]` (`group.go:76-92`), `CacheKey(app, tenant, name)` (`appname.go:40-47`); `kit/httpx/httpx.go` · `Options.Cache` (`:285`) and `API.Connect(_, store)` (`:572`) | exists; the one production user is `kit/httpx/tenant.go:164,193`; `GroupOf` is unused in production — use it rather than hand-rolling a TTL |
| Capability (`contracts/`) | none needed. `modules/task/contracts/permissions.go` already names `task:read`, the one grant a total may use | state it: this task adds no contract, no permission and no event |
| Entry-hint vocabulary | `kit/entity/hints.go` · `EntryHints` (`:84-115`), `Icons`/`Tones`/`Formats` (`:21-45`) | exists; add two optional fields to `EntryHints`, no new vocabulary list |
| Mount gate | `kit/rest/hints.go` · `entryHintsFault` (`:31`), `CheckReferences` (`:383`), `CheckRowHints` (`:421`); booted from `apps/platformkit/fault.go:259-269` (`documentGate.check`) | exists |
| Interface block / shell | `ui/screens/hints.go` · `EntryPresentation` (`:33-50`) + `deviations` (`:79-100`) + `declared` (`:66-73`); `ui/screens/catalog.go` · `Catalog` (`:40-45`), `Entry.Presentation` (`:92`), `Describe`/`Describe1` (`:172`, `:250`), `CatalogVersion` (`:34`) | exists; a new optional key is additive, so `CatalogVersion` stays at 2 (its own rule, `ui/screens/catalog.go:22-33`) |
| Address seam | `kit/app/app.go` · `Options.WorkspaceCatalog` (`:117-131`), `WorkspaceCatalogRoute[T]` (`:1064`), `mountWorkspaceCatalog` (`:1089-1113`), `workspaceDocument` (`:1078-1086`) | exists; `apps/platformkit/fault.go` · `workspaceFace` (`:280-286`) takes the second mount with no kernel change |
| Composition wiring | `apps/platformkit/app.go:273` (`WorkspaceCatalog(workspaceFace(workspaceCatalog(), face.mount))`); the worked example `apps/platformkit/connection.go:66-116` (`connectionFace.mount`, `connectionDocument`) | exists |
| Surface | `kit/httpx/surfaces.go` · the `App` surface of `api.Surfaces("")` (`:171`, `:466`; the refusal of a module taking the prefix at `kit/module/module.go:287`) | exists; `no-store` on every session response is already the rule (`apps/platformkit/connection_test.go:97` quotes it) — a summary cache is therefore in-process/by-tenant, never an HTTP cache |
| Web twin (same data, browser shell) | `modules/admin/internal/pages.go` · `pages.dashboard` (`:530-571`) and `pages.contents` (`:585-607`), drawn with `components.Card` / `components.Grid` | exists; T-0337 does **not** rewrite it — T-0349 is the dashboard's task — but any changed `Count` semantics land on this page first, so its test (`modules/admin/module_test.go:581`) is the regression to keep green |
| Mobile | `/home/jplr/gitrepos/pkit/platformkit-mobile` · `src/core/catalog.ts`, `src/screens/Home.tsx`, `app/index.tsx:19` | sibling checkout, out of this worktree; T-0338 is its task. The kernel side ships the bytes and the OpenAPI entry; it may not edit that file |
| Journey step / e2e | `e2e/` specs (`e2e/ui-components.spec.ts`, `e2e/one-notice-per-holder.spec.ts`) and `tests/inventory.json` (`make check-test-inventory`, `Makefile:322`) | exists; a new Go test file is a new inventory row, not an edit to `loc-budget.json` |
| Test double and fixture | `kit/rest/count_test.go` · `countAuthorizer` (`:19-27`); `kit/db/dbtest` (`dbtest.go:50-51`); `kit/rest`'s `mountAs`/`spec`/`acme`; `apps/platformkit/openapi_contract_test.go` · `deviceComposition`/`install`/`start` (`:91-98`); `kit/cache/cachetest` (the port's conformance) | all exist; compose them |
| Copy table | `ui/resource/hints.go` · `hints.<module>/<entity>.<aspect>` key grammar (`:10-27`) and `Words.Entry` (`:128`); `modules/task/catalogue.go:10-17` + `modules/task/messages/pt-PT.json`; the parity gate named at `ui/resource/hints.go:36-45`; `make check-i18n` (`Makefile:257`) | exists; a highlight `label` is a declared reading string through this seam — no new table, and no new `en` file |
| Migration pattern | `modules/task/migrations.go` + `modules/task/migrations/*.up.sql`, adopted at `modules/task/module.go:197-198` (`Migrations: Migrations.Files`, `Adopts`); `docs/adr/0011-migration-ownership.md` | exists — and this task needs **no migration**: a hint is a Go declaration and a total is a read, so no column and no row changes |
| Requirements / evidence shape | `CONTRIBUTING.md:7-25` (this trace), `AGENTS.md` (`Verified:`/report duty), `Makefile:506` (`check`) and `Makefile:325`/`:328` (push/merge tiers), the acceptance list in `/home/jplr/.local/share/pkit-999/tasks/T-0337-the-kernel-counts-what-each-resource-holds.md`, and the record at `/home/jplr/gitrepos/pkit/ops/platformkit-record/architecture/999-apps/decisions/0085-the-phone-feels-finished-from-the-first-minute.md` | exists |
| Documentation shape | `docs/adr/NNNN-*.md` (latest is `docs/adr/0020-a-credential-may-retire-itself.md`), `ARCHITECTURE.md`'s entity-and-presentation section (pinned by `make check-pillars`, `Makefile:270`), per-package `README.md`/`doc.go` (`kit/rest/README.md`, `kit/entity/README.md`, `kit/cache/README.md`, `docs/cache.md`) | exists; 0085 lives in the record repository, not here — update the record there, the package docs here |

## Search scope

For every claim of absence, the command run in
`/home/jplr/pkit-999/worktrees/task-T-0337-the-kernel-counts-what-each-resource-holds` at `42d4b8e`, and its
result. Nothing was excluded except `node_modules` where noted.

- `/api/v1/app/summary`, `asOf`, a highlight, a trend, a `trendField` —
  `grep -rn "api/v1/app" --include=*.go kit pkit apps modules ui` (only `/resources`, `/connection`,
  `/access-requests`); `grep -rn "asOf\|AsOf" --include=*.go kit apps ui` → nothing;
  `grep -rn "highlight\|Highlight\|trendField\|TrendField\|trend" --include=*.go --include=*.ts
  --include=*.tsx --include=*.json . | grep -v node_modules` → only CSS/colour tokens
  (`ui/style/color.go:64`, `ui/ui.go:765-768`), a rich-text paragraph comment and translation-diff copy;
  no declaration, no wire key, no route.
- A grouped or ranged count — `grep -rn "date_trunc\|GROUP BY\|Group(" --include=*.go kit modules apps` →
  `kit/events/ledger.go:541` (a migration tally) plus unrelated `g.Group` HTML nodes;
  `grep -rn '"/count"' --include=*.go . | grep -v node_modules` → only the in-test probe at
  `kit/rest/count_test.go:50`.
- Another owner already doing this — `grep -rn "Count" --include=*.go kit/rest/rest.go kit/rest/operation.go
  kit/rest/screens.go kit/entity/schema.go kit/entity/hints.go` → `kit/rest/screens.go:68,73,151,156` only;
  `grep -rn "func .*Count\|Count " --include=*.go kit | grep -v _test` → `kit/crud/crud.go:131-132`,
  `kit/limit`, `kit/telemetry`, `kit/httpx/ratelimit.go`, `kit/httpx/schemas.go:129-131,321`,
  `kit/rest/screens.go`.
- `apps/flagship`, `apps/platformkit/modules.go`, a module registry —
  `ls apps` → `platformkit`; `find . -path ./node_modules -prune -o -name registry.go -print` → nothing;
  `grep -rn "flagship" --include=*.go .` → nothing; `ls apps/platformkit/modules.go` → "No such file or
  directory".
- `tools/clientsource` (named in the phase's example) — `ls tools/` → `designexport locbudget
  richtext-bundle`; the tool does not exist in this repository, so the example command could not be run and
  the presentation boundary was checked at `ui/screens` instead.
- A production user of the generic cache — `grep -rn "GroupOf" --include=*.go .` (only `kit/cache/group.go`
  and `kit/cache/cachetest/conformance.go`); `grep -rn "cache\.Cache" --include=*.go kit/httpx kit/app pkit |
  grep -v _test` → `kit/httpx/httpx.go:285,572,582` and `kit/app/app.go`; `grep -rn "opts.Cache\." --include=*.go
  kit/httpx` → `kit/httpx/tenant.go:96,164,193` only.
- `react-native-reanimated` (wave two's premise, for context) —
  `grep -rn "reanimated" --include=package.json --include=*.ts --include=*.tsx --include=*.md . | grep -v
  node_modules` → nothing in this repository; this kernel task declares data, not motion.

## Risks for the specify phase

1. **Absence must survive as absence.** `httpx.Resource.Count` returns `(int64, error)` and its callers
   already treat an error as "no card" (`modules/admin/internal/pages.go:536-539`) — but 0085's own symptom
   is `"0 Contents"` and `"0 Settings"` on the web dashboard. A `nil`-vs-`0` distinction has to be in the
   *response type* (a pointer or an omitted key per entry, exactly as `ui/screens/hints.go:33-50` uses a
   pointer per key), not in an int64 whose zero is indistinguishable from "empty". The three states to
   specify are the three `count_test.go:33-40` already separates: allowed, denied, unavailable.
2. **"One query per entry" fights the 14-day trend.** Fifteen entries × one total is bounded; fifteen
   entries × a 14-bucket `GROUP BY` is fifteen more. Specify the bound (trend only when `trendField` is
   declared, one statement per entry that declares one, and what happens when a tenant holds a million
   rows — an index on `task(dueAt)`/`(created_at)` exists or the query is refused by budget, not allowed to
   run), and state which statement the cache stores.
3. **The cache key is a security surface.** `cache.Of(tenant, ns)` + `Scope.Entry(name)`
   (`kit/cache/cache.go:112-150`) names a tenant and a namespace, not a *person*, and a total is
   permission-filtered. `CacheKey(app, tenant, name)` (`kit/cache/appname.go:40-47`) is `<app>:<tenant>:<name>`,
   so the person's resolved grants have to be inside `name` — or the entry must be a per-permission-set key —
   or one person's cached total is served to another with fewer grants. `Generation`/`Move`
   (`kit/cache/cache.go:89-97,375-387`) invalidates a scope, not a person: a revoked grant must not survive
   the TTL, which bounds how "briefly" briefly is. Say the rule in the specify phase, and pin it with a test
   beside `kit/rest/signed_in_closure_tenant_test.go:21`.
4. **`highlight.filter` is a declaration, and a declaration that cannot be honoured must refuse at mount.**
   0085's rule for the rest of the hints is that the mount gate refuses what would draw the wrong screen
   (`kit/rest/hints.go:31-135`); a `highlight` naming an unknown field, a non-comparable field
   (`kit/crud/crud.go:380-388` refuses `TypeList`), or an operator the grammar has no form for must not
   silently return the unfiltered total — that is a wrong number wearing a real label ("Due today": 4,300).
   Specify the filter's grammar as data (the `{field, op, value}` shape and the closed operator set), not as
   a SQL fragment.
5. **`asOf` needs one clock per response, injected.** `entity.Base` timestamps are UTC and every module takes
   `Clock` in `Deps`; this route is the composition's, so its clock is the composition's — and "the highlight
   is due-today *as of* this instant" must agree with the trend's day boundaries in the same body, to the
   second, from one reading.
6. **The catalogue is a compatibility contract.** `/api/v1/app/resources` is what the shipped phone parses
   (`ui/screens/catalog.go:19-33`); new optional keys are additive and keep `CatalogVersion` at 2, but a key
   that *changes meaning* later (e.g. `highlight` becoming server-resolved rather than client-computed) is a
   version move. Also name the two gates that will fail on a new door if nobody tells them:
   `apps/platformkit/openapi_contract_test.go:66-88` and its golden (`wireAuthorizationAllowances`), and
   `tests/inventory.json` via `make check-test-inventory`.
7. **This worktree has no stack of its own** (ports allocated at 61349/62349/63349/60349 with nothing behind
   them — see *Boundary check*). Every acceptance case here is a database case; `make check` on this tree
   fails for that reason alone, with nothing wrong with the code. Either bring this worktree's stack up
   (`make up`) or point the suite explicitly at a database, the way the boundary run above did — and say
   which database a report's `Verified:` line was run against, because the shared one on 5432 serves every
   task on this host at once.
8. **Do not fix the web dashboard here.** `pages.dashboard` is T-0349's surface; 0085 also warns that its
   copy is English written in Go (`modules/admin/internal/pages.go:557-568`, `Language: writtenHere`).
   Reusing an extended `Count` may change what its cards show; that is a regression to keep honest with
   `TestTheDashboardCountsOnlyWhatTheCallerMayRead`, not an invitation to re-word the page.

TRACE: complete
