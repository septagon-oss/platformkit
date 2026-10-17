# SPECIFY — T-0337 the kernel counts what each resource holds, for the person asking

Specified against `/home/jplr/pkit-999/worktrees/task-T-0337-the-kernel-counts-what-each-resource-holds`
at `42d4b8e` (the trace's revision; `git status --short --branch` is clean apart from nothing — no file
outside the two state documents is written by this round). No implementation, no commit, no registry edit,
no `loc-budget.json` edit, as the phase rules said.

Everything below cites the file and symbol that already exists, or names the unit the trace's reuse
inventory found and says *new, because* the inventory had nothing.

---

## 1. The shape of the change

Nothing here is a module and no new module is created: no new capability, no tenant-scoped state, no
permission, no event, no contract. The person asks a question the list already answers, about rows the
list already reads. Four layers move, in this order:

| # | File | Change |
|---|---|---|
| 1 | `kit/crud/crud.go` | a closed predicate grammar (`Op`, `Predicate`, `Where`) beside `Query.Filter`, `conditions` split so equality has one implementation; `CountWhere[T]`; `DayCount`, `CountDays[T]` |
| 2 | `kit/entity/hints.go` | `EntryHighlight`, `EntryPredicate`, `Anchors`/`ValidAnchor`; two optional fields on `EntryHints`: `Highlight`, `TrendField` |
| 3 | `kit/httpx/schemas.go` | `Resource.CountFiltered`, `Resource.CountDays`; both wrapped by the existing `(*API).guard` behind the *same* `read` `Auth` as `Count`. `kit/httpx/httpx.go`: `func (a *API) Cache() cache.Cache` |
| 4 | `kit/rest/screens.go` + `kit/rest/hints.go` | `Spec.resource()` fills the two closures from the two new `crud` calls, and refuses both where `Count` is already refused (no list route); `entryHintsFault` refuses an incoherent `highlight`/`trendField` |
| 5 | `ui/screens/hints.go` | `EntryPresentation.Highlight`, `.TrendField` — echo only, per the file's own rule |
| 6 | `ui/screens/summary.go` (new file) | `Summary`, `SummaryEntry`, `Trend`, `TrendDay`, `Answers`, `Summarise`, `TrendWindow`, `TrendDays` |
| 7 | `apps/platformkit/summary.go` (new file) | `summaryFace.mount(api)` — `GET /api/v1/app/summary` — the answering loop, the cache rule, the budget |
| 8 | `apps/platformkit/app.go:273` | `workspaceFace(workspaceCatalog(), face.mount, summary.mount)` — the one line the trace predicted |
| 9 | `modules/task/module.go` | adopt `Highlight{Label:"Due today", Filter:[dueAt gte today, dueAt lt tomorrow]}` and `TrendField:"createdAt"` |
| 10 | `modules/task/migrations/000112_task_due_index.up.sql` | `CREATE INDEX tasks_tenant_due ON tasks (tenant_id, due_at) WHERE deleted_at IS NULL AND due_at IS NOT NULL;` (version number is the implement round's, taken as the next free in `modules/task/migrations/`) |
| 11 | `modules/task/messages/pt-PT.json` | `hints.task/task.highlight.label` → "Vence hoje" |
| 12 | goldens + gates | `ui/screens` catalog golden, `apps/platformkit/testdata/openapi.json`, `deviceContractPaths`, `tests/inventory.json` |

Three deletions this spec depends on, stated so a reviewer can check them: no new address constant
(`httpx.AnyCredential()` and `api.Surfaces("").App` are the existing ones), no new permission, no new
cache adapter.

**The trace's claim "this task needs no migration" is wrong** and item 10 replaces it: `due_at` has no
index (`modules/task/migrations/000004_task.up.sql:32-39` indexes `created_at`, `status`, `priority`,
`assignee_id`, `sla_deadline` — never `due_at`), so the "Due today" total would read the tenant's whole
task set. `created_at` is already indexed by `tasks_tenant_created (tenant_id, created_at DESC, id) WHERE
deleted_at IS NULL`, which is exactly the trend's shape.

---

## 2. The response schema

### 2.1 The declaration, in `kit/entity` (what a Spec says)

```go
// EntryHints, two more optional keys beside the seventeen:
Highlight  *EntryHighlight // the one sub-count a card may wear beside the total
TrendField string          // the name of this entity's own timestamp field to break down by day

// EntryHighlight is one declared sub-count: the words, and the predicate that says
// which rows it counts. Filter is empty-and-refused: a label with no predicate
// would be a second spelling of the total.
type EntryHighlight struct {
	Label  string           `json:"label"`
	Filter []EntryPredicate `json:"filter"`
}

// EntryPredicate is one condition on one field of this entity, spelled as data.
// A SQL fragment is not a declaration: nothing here reaches a database.
type EntryPredicate struct {
	Field string `json:"field"` // this entity's own field name, e.g. "dueAt"
	Op    string `json:"op"`    // a name crud.Ops admits: eq, lt, lte, gt, gte
	Value string `json:"value"` // an anchor (entity.Anchors) or a literal in the field's own reading
}

// Anchors is every clock-relative instant a declared value may name; the server
// resolves it against the one instant of the response. `now` is the response's own
// instant; `today` and `tomorrow` are the starts of consecutive UTC days.
var Anchors = []string{"now", "today", "tomorrow"}
func ValidAnchor(v string) bool
```

`Value` is a string and nothing else, because a Go `time.Time` written in a declaration is a frozen date,
which is the bug this whole hint would ship. `today`/`tomorrow` resolve at request time (§4).

### 2.2 The echo, in `ui/screens/hints.go`

`EntryPresentation` gains `Highlight *EntryHighlight` and `TrendField *string`. Nothing else:
`declared`/`deviations` already print a non-zero declaration and print nothing for a zero one, so an
un-hinted entry's bytes do not move and **`CatalogVersion` stays at 2** — added optional keys, exactly the
rule written at `ui/screens/catalog.go:22-33`. A `highlight` whose *meaning* changed (say, becoming
server-resolved copy) would move it; this is not that.

### 2.3 The answer, in `ui/screens/summary.go` (new file)

```go
// Summary is what each resource the caller may read holds, at one instant.
type Summary struct {
	AsOf    time.Time      `json:"asOf"`    // one reading of one clock, UTC
	Entries []SummaryEntry `json:"entries"` // catalogue order; every entry the catalogue listed with a list
}

// SummaryEntry is one resource's counts. Every number is a *int64: a total the
// installation could not answer prints no key at all, and a total of zero prints
// `"total":0`. An int64 cannot say both, and "0 Contents" is 0085's own symptom.
type SummaryEntry struct {
	Module    string `json:"module"`          // spelled as entity.Schema spells them, so a shell
	Entity    string `json:"entity"`          // joins the two documents on (module, entity)
	Total     *int64 `json:"total,omitempty"`
	Highlight *int64 `json:"highlight,omitempty"`
	Trend     *Trend `json:"trend,omitempty"`
}

type Trend struct {
	Field string     `json:"field"` // the declared field, echoed: "createdAt"
	Days  []TrendDay `json:"days"`  // always exactly TrendDays long, oldest first
}
type TrendDay struct {
	Day   string `json:"day"`   // a UTC calendar date, "2026-10-03"
	Count int64  `json:"count"`
}

// Answers is one entry's counts as the composition read them. A nil Total or
// Highlight is "not answered", which is not zero and is never cached.
type Answers struct {
	Total     *int64
	Highlight *int64
	Days      []crud.DayCount // nil when the entry declares no trendField
}

const TrendDays = 14
func TrendWindow(asOf time.Time) (from, to time.Time)   // [start of asOf's UTC day − 13 days, +1 day)
func Summarise(resources []httpx.Resource, answers map[string]Answers, asOf time.Time) Summary
```

`Summarise` is pure: no context, no transaction, no cache, no words. It selects the entries the way the
catalogue does (§5), keys `answers` by `<module>/<entity>` (the same spelling `ui/resource.Words.Key`
uses in the middle and `reference:` uses for a target), and lays `Days` into the 14 buckets of
`TrendWindow(asOf)`, filling a day the query did not name with `Count: 0`.

The example body, the reference composition, the reference tenant:

```json
{"asOf":"2026-10-16T17:02:11Z","entries":[
 {"module":"task","entity":"task","total":41,"highlight":4,
  "trend":{"field":"createdAt","days":[
    {"day":"2026-10-03","count":0},{"day":"2026-10-04","count":2},
    {"day":"2026-10-16","count":1}]}},
 {"module":"content","entity":"page","total":3},
 {"module":"site","entity":"settings"},
 {"module":"file","entity":"file","total":0}
]}
```

Four states, four shapes, all of them reachable: answered (`"total":41`), genuinely empty
(`"total":0` for `file/file`, because the query ran and answered zero), declared-and-unanswerable
(`site/settings`: no list route, so no entry key at all — see §5's row selection), and answered with a
failure (`"module":"content"` with no `total` key while the response is still 200).

**No key of this body is a word a person reads.** `label`, `singular`, `plural` stay in `/resources`,
where `ui/resource.Words` resolves them once, because a key spelled beside a string that is already
written is a second copy of that string (`ui/resource/hints.go:10-27`). Two consequences, both pinned:

- the response carries **no `Vary: Accept-Language`** (`kit/app`'s `workspaceDocument` sets it for the
  catalogue because the catalogue carries words; this one does not), and
- the cache key therefore names **no language** (§6). A case fails if either stops being true.

A future key that *is* a word, or a per-person day boundary, must add `Vary: Accept-Language` and the
language into the cache name in the same commit — the two rules are one rule and are named together in
`apps/platformkit/summary.go`.

---

## 3. The declaration gate (`kit/rest/hints.go`, `entryHintsFault`)

The mount gate refuses, at boot, in the same voice as the seventeen existing hints:

| Refusal | Class |
|---|---|
| `highlight` is declared with an empty `Filter` — "a label with no predicate is the total wearing a label" | immutable |
| `highlight.filter` is empty (declared-but-empty list, the shape `sortable`/`summaryFields` already refuse) | immutable |
| `highlight.filter` names a field the entity's schema has not got (the same `crud.FieldNamed` question) | immutable |
| `highlight.filter` names a `TypeList` field — `comparable`'s own refusal (`kit/crud/crud.go:374-388`) | immutable |
| `highlight.filter` carries an `op` outside `crud.Ops` | immutable |
| `highlight.filter` uses a range operator on a field whose `Type` is not `TypeTime`, `TypeInt` or `TypeFloat` — "lt is no question about a list of words" | immutable |
| a `value` naming a time field is neither an `entity.Anchors` name nor an RFC3339 instant | immutable |
| a `value` naming an int field does not parse as an int64; a float as a float64; a bool as `true`/`false`; a uuid does not parse — the same coercion table `kit/rest.coerce` uses, so one table decides both | immutable |
| the same field appears twice with two range operators that admit nothing (`gte today` and `lt today`) — `from >= to` on one field | immutable |
| `highlight.label` is empty | immutable |
| `trendField` names a field the schema has not got, or one that is not `TypeTime` — "a day breakdown needs a column of days" | immutable |
| `highlight` or `trendField` declared on a Spec that mounts no **list** route (the same `s.offers(httpx.CRUDList)` question `kit/rest/screens.go:150-156` asks of `Count`) — "nothing would ask it" | immutable |

Every refusal here is immutable: it is a Go declaration that cannot be honoured, and the alternative —
answering "Due today" with the unfiltered total — is 0085's own worst case, a wrong number wearing a
real label. Nothing at *request* time can be refused for these reasons, because none of them can reach
the wire.

`CheckReferences`/`CheckRowHints` (`kit/rest/hints.go:383`, `:421`, booted from
`apps/platformkit/fault.go:259-269`) gain nothing: a highlight names no *resource*, so no
cross-module question needs the whole list. It stays a per-Spec question.

---

## 4. The query per entry

### 4.1 The grammar (`kit/crud`)

```go
type Op string
const (OpEq Op = "eq"; OpLt Op = "lt"; OpLte Op = "lte"; OpGt Op = "gt"; OpGte Op = "gte")
var Ops = []string{"eq", "lt", "lte", "gt", "gte"}
func ValidOp(name string) bool

type Predicate struct { Field string; Op Op; Value any }
type Where []Predicate

// CountWhere counts this tenant's live rows the predicates admit — the same
// lifecycle predicate Count applies, and the same field-name check conditions
// applies to a filter, so a column name never comes from a declaration it did
// not check.
func CountWhere[T Entity](tx db.Tx[db.Tenant], w Where) (int64, error)

type DayCount struct { Day time.Time; Count int64 } // Day is the start of a UTC day

// CountDays breaks this tenant's live rows into the UTC days of one timestamp
// column inside [from, to). It names the days the query answered; Summarise is
// what turns an unnamed day into a zero.
func CountDays[T Entity](tx db.Tx[db.Tenant], field string, from, to time.Time) ([]DayCount, error)
```

`conditions` is refactored into `predicate(fields, Predicate) (sql string, args []any, err error)`;
`conditions` (equality, from `Query.Filter`) and `CountWhere` both call it, so `column = ?` is written in
one place and `comparable`'s two refusals — an unknown name, a `TypeList` — are the same refusals. The
existing list route, `Query.Filter` and `?filter=field:value` do not change at all (the public list
grammar stays equality-only; §12's last row says why a new operator needs a new parameter
spelling).

`Where` is built only by server code from a checked declaration. It is never parsed from a request:
there is no path from a query parameter to a `Where` value in this change, and a case pins that
(`TestASummaryTakesNoFilterFromTheCaller`, §8).

### 4.2 The statements

Per entry, and never more than the entry's own declaration asks:

| Question | Statement | Bound |
|---|---|---|
| total | `SELECT count(*) FROM tasks WHERE deleted_at IS NULL` — today's `crud.Count`, unchanged | `tasks_tenant_created`'s partial index covers the predicate; the plan does not change from what `modules/admin`'s dashboard already pays |
| highlight | `SELECT count(*) FROM tasks WHERE deleted_at IS NULL AND due_at >= $1 AND due_at < $2` | new `tasks_tenant_due (tenant_id, due_at) WHERE deleted_at IS NULL AND due_at IS NOT NULL` — one narrow index range, not a scan of the history |
| trend | `SELECT date_trunc('day', created_at AT TIME ZONE 'UTC') AS day, count(*) FROM tasks WHERE deleted_at IS NULL AND created_at >= $1 AND created_at < $2 GROUP BY 1 ORDER BY 1` | `tasks_tenant_created` covers both the predicate and the count; the window is 14 days and never wider |

`AT TIME ZONE 'UTC'` is not decoration and is not a style choice: `date_trunc` on a `timestamptz`
truncates in the *session's* `TimeZone`, and nothing in `kit/db` sets one (grep for `TimeZone` in
`kit/db` answers `search_path` only), so without it a server configured to `America/Sao_Paulo` would
move every bucket by three hours. `TestADayBucketIsAUTCDayUnderAnySessionTimeZone` sets
`SET TIME ZONE 'America/Sao_Paulo'` inside the test transaction and requires the buckets not to move.

### 4.3 The count, and the brief's phrase it does not mean

The brief's "one query per entry" is **replaced**, deliberately, by:

> at most three statements per entry — one per question — and only the questions that entry declares:
> one for an entry with no hint, two with a highlight, two with a trend, three with both. The reference
> composition has 15-ish readable entries and exactly one that declares anything, so its worst case is
> (n−1) + 3 = n + 2 statements, all in the request's own transaction.

The alternative that would honour the letter — one statement with
`count(*) FILTER (WHERE …)` for the total, the highlight and all 14 day buckets — is one scan of the
tenant's whole table with sixteen aggregate states, because no FILTER can be answered from an index
while `count(*)` alone can. The narrow three-statement form reads an index for the two declared
questions and reads exactly what it must for the total. The brief's *concern* (bounded work per entry,
no N+1 explosion) is kept; its *letter* is traded for a plan that does not seq-scan a million rows. This
is the one place where this specify round contradicts a Decided line, and it is the review round's to
reverse if it disagrees.

Two more bounds:

- **Per-request deadline.** The answering loop runs under `context.WithTimeout(ctx, summaryBudget)` with
  `summaryBudget = 5 * time.Second` (a named package constant beside `summaryTTL`, with the same
  right-of-reply as `cache.budget`'s 250 ms: a cache sits in front of the truth, a summary sits in front
  of a count). gorm honours context cancellation, so a count that overruns is cancelled at the database
  and its entry is *not answered* — absent from the body, never zero. The response is still 200 with
  whatever the deadline allowed, and the entries it dropped are logged by module/entity.
- **Transaction and tenancy.** One `httpx.TxFrom(ctx)`, the request's own transaction, for every
  statement, exactly as `apps/platformkit/connection.go:118-122` does. Row-level security is the tenant
  scope; the handler passes no tenant anywhere, because there is no parameter that could name one.

### 4.4 `asOf`, and what it does not promise

`asOf` is `db.Now()` (`kit/db/clock.go:18`, UTC, microsecond-truncated), read **once**, before the first
count, and carried into the body and into the highlight's anchor resolution and `TrendWindow`. It arrives
through a `now func() time.Time` field on `summaryFace`, late-bound like `connectionFace.describe`, and
tests set it; a composition with no opinion gets `db.Now`.

It is the instant the request was asked. It is **not** a snapshot: the transaction runs at READ COMMITTED
(each statement its own snapshot) and §6 caches each entry for up to 15 s, so a number in a body may be
up to `summaryTTL` older than `asOf`. The rule a consumer may state is exactly: *no number in this body
is older than `asOf` minus 15 s, and two numbers in one body may straddle one another's writes*. A
product that ever needs a body from one snapshot asks for a REPEATABLE READ read-only transaction here;
nothing asks now, and a read-only RR read is the only change that would buy it.

---

## 5. Entry selection, and who is asked

For each `r` in `api.Resources()`, in registration order:

1. skip `r` when `r.CommandsOnly` or `!r.Offers(httpx.CRUDList)` — the same question the catalogue asks
   before it prints an entry's `operations`, and the reason `Count` is refused at
   `kit/rest/screens.go:150-156` for a Spec with no list: a total belongs to the list.
2. skip `r` when `!r.Readable(ctx)` — `kit/httpx/schemas.go:242`, the same question `DescribeLanguage`
   asks (`ui/screens/catalog.go:200`). The set of entries in `/summary` and the set in `/resources` are
   then the same set, decided by the same method, and no number can exist for an entry the shell was
   never given.
3. **only now** form the cache key and consult the store (§6). Then ask the guarded closures.

So the authorization sequence per counted entry is: `Readable` (one decision, decides the entry's
presence) + one decision inside each closure the entry actually declares (`Count`, `CountFiltered`,
`CountDays` — each wrapped by `(*API).guard` at `kit/httpx/schemas.go:320-327` with the *same* `read`
`Auth`). The duplication is bought, not ignored: the guard is welded to the closure precisely so "a
Resource built anywhere else is unguarded until it is registered" cannot happen, and the entry-set
question must be asked separately or the two documents can disagree about which cards exist. It is one
in-process `Authorizer.Allowed` call, never a query; `kit/rest/count_test.go` pins the one-decision rule
for each closure itself and this task adds no second authorization path.

A closure that refuses (a Spec with no list, a `crud.ErrInvalid` from a declaration the gate somehow
mis-checked, a cancelled deadline, a database that stopped answering) makes that **one** entry's answer
absent, and nothing else. The route's own failures:

| Condition | Answer | Class |
|---|---|---|
| no tenant transaction on the request (`httpx.TxFrom` fails) | `503` "the database is not reachable right now", the same sentence `connection.go` uses | immutable to the caller; retry |
| host resolves to no tenant | `403`/`401` from the surface itself, before this handler runs — this is not a `Public` door | correctable by signing in / by the operator fixing the host |
| caller holds no `read` grant for any resource | `200 {"asOf":…,"entries":[]}` — the empty document, not a refusal: the address describes what this caller may reach, and "nothing" is the truth | correctable by the ask door (`httpx.Options.Access`) |
| every count failed | `200` with entries carrying no keys — the shell draws no numbers; a whole-screen error because a card has no number is 0085 principle 2's opposite | — |

The route is `httpx.AnyCredential()`, `OperationID:"app-summary"`, `GET /summary`, `Tags:["kernel"]`,
`Errors:[503]` — the same declaration `app-resources` carries, for the reason at
`kit/app/app.go:1096-1105`: it spends no authority beyond the caller's own, and the credential a
non-browser client holds is a bearer key. `kit/app` prints every anonymous door at boot; this is not one
of them and the boot audit stays as it is.

---

## 6. The cache rule

One row added to `docs/cache.md`, and this is its text:

| Value | Where | Lifetime | Invalidation | Forbidden |
|---|---|---|---|---|
| a workspace total, per tenant, per resource | the shared store, one copy, no second copy in the process | 15 s (`summaryTTL`) | nothing wired: the TTL only | a key formed before the read guard was asked; caching a refused, failed or cancelled count (absence is never stored, and a stored absence reads as a decision); a key without the tenant; a per-body entry, which would key one document's resource list; a second in-process copy |

- **Scope**: `cache.Of(tenant, "summary")`, the tenant from `httpx.TxFrom(ctx)` — `Of` refuses
  `uuid.Nil`, so a request that resolved nothing cannot form a key (`kit/cache/cache.go:112-120`).
  Namespace holds no `/`, which is why it is `summary` and not `app/summary`
  (`kit/cache/cache.go:466-468`). The app segment comes from `cache.New`'s slug, as always:
  `platformkit:<tenant>:summary/<name>`.
- **Name**: `<module>/<entity>` for an entry that declares neither highlight nor trend;
  `<module>/<entity>/<YYYY-MM-DD>` — `asOf`'s UTC date — for one that declares either. The name names
  every input the answer depends on: the tenant (in the scope), the resource, and the day whose bounds
  the window was resolved against. At a UTC midnight the key changes rather than one day's window being
  served as the next one's. The declaration itself is absent from the name and must be: it is fixed at
  boot, and if it ever stops being so the name has to carry it — the falsifiable half of this rule.
- **Value**: the JSON of `summaryAnswers{total,highlight,days}` — the *answers*, one entry per resource,
  not the body. A body would key the caller's resource list, which is a grant-derived fact (§6.1), and
  would re-answer fifteen resources to refresh one.
- **Rule states are never stored**: an entry whose `Count` errored is not written; a `Get` that *errors*
  is treated as a miss and the load runs (a store outage costs a query, never a refusal —
  `kit/httpx/tenant.go:117-121`'s rule, and `Group.local`'s); a `Set` that errors serves the answer
  anyway and logs, as `Group.Get` does.
- **Invalidation is not wired**: no module is told this namespace exists, `Move` is never called, and the
  accepted staleness is 15 s of "a row was created since". Wiring an invalidation would put one module's
  writes in another module's cache namespace — a second owner of a scope — for a card that refreshes on
  every foreground anyway.
- **`GroupOf` is not used, with the reason.** The trace's inventory listed it, and it is the wrong entry
  point here for two checks: `Group`'s value lives in *this process's memory* with a marker in the store
  (`kit/cache/group.go:3-11` — "an instance does not build 999 routers at boot"; the value is a graph of
  closures no serialisation reaches), and a count is a serialisable 40-byte JSON value. A `Group` would
  put a per-replica copy of one tenant's total in every pod — the shape `docs/cache.md` forbids in the
  host row as "a second local copy, which would need a second invalidation" — and nothing here is composed
  per key. What is used is the port itself, `Cache.Get`/`Cache.Set` with the generation the `Get`
  answered, which is the one production pattern this repository has (`kit/httpx/tenant.go:121,164`).
  Nothing is hand-rolled: no TTL arithmetic, no local map, no LRU, no singleflight, no generation
  bookkeeping — all four are `kit/cache`'s.

### 6.1 Why the key may name no person, and why it may not be trusted to

The risk the trace names (#3): a total is permission-filtered, and `cache.Of` names a tenant, not a
person. The answer is *sequence*, not key material, and it is the load-bearing sentence of this task:

> **A cached total is never returned to a caller the read guard would refuse, because the guard runs
> before the key is formed.** §5's step 2 (`r.Readable(ctx)`) precedes §6's `Get`, and no code path
> reaches `Get` without passing it.

Within the tenant, the answer to "how many tasks are there" depends on nobody's identity beyond the
`task:read` grant that decided the entry's presence, because RLS scopes rows by tenant and the authorizer
decides per (tenant, permission) — there is no per-row grant in this kernel to be a per-person total.
So per-tenant is the correct grain, and a revoked grant cannot survive the TTL: the next request asks the
authorizer again and the entry is not read. A key naming the person would be *slower and no safer*, and
would need the person's id in a log-visible key (`Key.String()` is documented as reaching a log line).

The failure mode this leaves open, and its test: a future authorizer that decides per *row* (record-level
grants) makes a per-tenant count the wrong answer, and no cache rule could fix that. The rule to hold in
`apps/platformkit/summary.go`'s comment is therefore: this cache is sound for a *tenant-wide* read grant,
and a per-row grant must move the key to the grant set or go uncached. `TestACountIsNeverServedPastItsReadGuard`
(§8) is the case that makes the sequence checkable today.

### 6.2 The store's own requirements

No new ones: the generation counter still needs `maxmemory-policy noeviction` (`docs/cache.md`), and a
multi-process installation already names `valkey` at boot or is refused. An installation with one process
and no adapter gets `cache.Memory` and one tenant's totals per process — correct, just 15 s more stale
per pod, which is what `cache.Memory` is for.

---

## 7. The ten questions

1. **Entity fields.** No entity, no table, no column is added except the `due_at` index. The values this
   document carries are derived: `asOf` (a clock reading), `total`, `highlight`, 14 `days`, `trend.field`,
   plus the two new declaration fields `highlight{label,filter[]}` and `trendField`. Every derived field
   is read by the phone's Home (T-0338) or the web dashboard (T-0349); none is written back anywhere.
2. **Identity and relationships.** A summary entry's identity is `(module, entity)`, the same pair
   `entity.Schema` publishes (`kit/entity/schema.go:19-20`) and the same spelling the copy key uses in
   its middle (`ui/resource/hints.go:76-78`). No id, no path, no screen address, no `pictures.source` in
   this body: an entry joins the catalogue on the pair and derives whatever address it needs from the
   catalogue's own entry, which is where those live.
3. **States and transitions.** None: this task has no state machine because it keeps no state. The three
   *answer* states (answered / zero / unanswered) are its whole lifecycle and are in §2.3. A cache entry
   has two generations-worth of life and no transitions beyond `Get`/`Set`/expire, which `kit/cache` owns.
4. **Authorization.** §5 and §6.1. No new permission (`modules/task/contracts/permissions.go`'s
   `task:read` is the only grant a total may use, and it is the one `Resource.Count` already asks); no
   new `Auth`; no `OperatorRead` bypass; the summary never answers an anonymous caller.
5. **Transaction and concurrency.** One request transaction (`httpx.TxFrom`), READ COMMITTED per
   statement, no writes so no locks and no revision checks, no `GetForUpdate`, no retry loop; the
   5 s deadline and the per-entry absence are in §4.3; `asOf`'s promise is in §4.4. Two concurrent
   requests for one tenant may both miss the same cache entry and both load; `cache.Set` is stamped with
   the generation each one's own `Get` answered, so the slower one's write is believed by nobody if a
   `Move` landed meanwhile, and costs nothing if not — the racing-load argument in `kit/cache`'s `Move`
   comment, with no new code.
6. **Events and idempotency.** It is a GET: it emits no event, writes no row, needs no idempotency key,
   and publishes nothing to the outbox. It is idempotent in effect and not byte-identical in body
   (`asOf`, and a cache entry that just expired); no consumer may diff two bodies, and the acceptance
   cases never do. Retries are always safe.
7. **Retention and export.** Nothing to retain (no rows) and nothing to export: the body carries no
   personal data — five integers and fourteen dates per resource, identical for every person in the
   tenant holding the grant. Erasure has nothing here to reach; the cache entry expires in 15 s and holds
   no person's identifier in its key or its value.
8. **Provider integration.** None. No third-party service is called. **No library is added** — see §10 for
   the three names this spec touches and the proof each is already in the build.
9. **Locale, time, money.** Time: one `db.Now()` reading per response, UTC, microsecond; the day
   boundaries are UTC (the kernel holds nobody's timezone — grep `TimeZone` finds no session setting in
   `kit/db`); the trend's buckets are forced UTC in SQL, not by configuration (§4.2). Locale: this body
   carries no words, so no negotiation, no `Vary` and no language in the cache key (§2.3); the one word
   this task adds — the highlight's `label` — is a declared reading string resolved through
   `hints.task/task.highlight.label` (`ui/resource/hints.go`'s grammar, `Words.Entry` the seam), which is
   why `pt-PT.json` gains a key and no `en.json` exists (§2.3's rule and decision 0012 rule 2). Money:
   none — every value here is an `int64` count of rows, never minor units, and nothing in this task
   touches `FieldMoney`.
10. **Specialist validation.** Nothing in this task needs a specialist's facts: it counts rows. The
    validation it does need is the mount gate (§3) against the entity's own schema, which is the
    `kit/entity`-vocabulary-plus-`kit/rest`-gate arrangement the other hints use. The `Anchors` vocabulary
    is three words and is frozen for the same reason `Icons` is: a name outside it has to refuse to
    mount, and a vocabulary that grows with each screen refuses nothing.

---

## 8. The cases, written now, against the doubles that will run them

The fake this delivery conforms to is a *composition*, not a new type: `kit/rest`'s `countAuthorizer`
(`kit/rest/count_test.go:19-27`, counts decisions, answers allow/deny/error), `kit/db/dbtest`'s tenant
transaction, `cache.Memory` (the port's own in-process adapter, run by `kit/cache/cachetest` so it is not
a different cache), a pinned `now`, and the reference composition's own `deviceComposition`/`install`/
`start`. Every case below names which of those it runs on and *why it would fail* — a case that cannot
name its own failure is not a case.

| # | Case (name checks the thing, decision 0072) | Package · double | Passes when / fails because |
|---|---|---|---|
| 1 | `TestTheSummaryCountsOnlyWhatTheCallerMayRead` | `apps/platformkit` · composition + `countAuthorizer`-equivalent (the composition's own authorizer, denied for `content:read`) | passes with `content/page` absent from `entries` and no `summary/content/page` key ever written; fails if a denied resource is listed **or** if a key was formed for it — the second is the cache half, read off the store, not off the response |
| 2 | `TestASummaryTotalCannotBeReadAcrossATenant` | `apps/platformkit` · composition + two tenants' rows in one database | passes with each host answering its own count and the other's never appearing; fails if a body names the other tenant's number — and it is the case that stays green only because there is no parameter to name a tenant with, so it is written as: insert 3 rows in acme, 5 in globex, ask each host, assert 3 and 5 |
| 3 | `TestACountThatCouldNotBeAnsweredPrintsNoNumber` | `apps/platformkit` · composition, with the deadline forced to 1 ns (or `Resource.Count` replaced by a refusing closure through the same seam) | passes with the entry present and no `total` key, and `total:0` only where a real zero was counted; fails on the first `"total":0` produced by a failure — this is the acceptance case 0085's `"0 Contents"` exists for |
| 4 | `TestTheTasksHighlightCountsDueTodayAndNothingElse` | `apps/platformkit` (or `modules/task` with a list-mounted Spec) · dbtest + pinned `now` = 2026-10-16T17:00Z | passes with `total` counting every live task and `highlight` counting only `due_at ∈ [16 Oct 00:00Z, 17 Oct 00:00Z)`; fails when the highlight equals the total (the filter was dropped) or counts yesterday/tomorrow (the window is off by a day or half-open on the wrong side) |
| 5 | `TestADueTodayCountChangesAtTheUTCMidnight` | same, `now` = 23:59:59.999Z then 00:00:00Z | passes with the second answer counting the *new* today and the cache missing; fails if the 15 s entry is served across the boundary (the date is not in the key) |
| 6 | `TestADayBucketIsAUTCDayUnderAnySessionTimeZone` | `kit/crud` · dbtest, `SET TIME ZONE 'America/Sao_Paulo'` in the tx | passes with the same 14 buckets either way; fails if the SQL uses `date_trunc('day', col)` without `AT TIME ZONE 'UTC'` — the exact bug, in the exact sentence |
| 7 | `TestATrendNamesFourteenDaysIncludingAnEmptyOne` | `ui/screens` · **no database at all**: `Summarise` with `Answers{Days: nil or sparse}` | passes with exactly 14 entries, oldest first, an unnamed day answered `count:0`; fails on a 13- or 15-day body, on a missing-day hole, or on a nil `days` for a declared field |
| 8 | `TestTheSummaryBodyIsTheSameInEveryLanguage` | `apps/platformkit` · composition, `Accept-Language: pt-PT` vs `en` | passes byte-identical (and no `Vary` header); fails the moment a person-readable key enters the body or `Vary` appears — the case that protects the cache key's right to name no language |
| 9 | `TestASecondRequestWithinTheLifetimeAsksTheDatabaseOnce` | `apps/platformkit` · composition + `cache.Memory` + a `gorm` query callback collecting statements (the mechanism `kit/rest/count_test.go:44-49` already uses) | passes with the second request issuing 0 count statements and the same numbers; fails if the key misses twice (a key that changes per request — e.g. an `asOf` second stamped into the name — is invisible everywhere else) |
| 10 | `TestACountIsNeverServedPastItsReadGuard` | `apps/platformkit` · composition, primed with a permissive authorizer, re-read with a refusing one, same cache store | passes with the second call answering no entry and querying nothing; **fails if the guard moved after the `Get`** — the one case that would go green today and red the day the sequence is inverted, and the reason it is written before the code |
| 11 | `TestARefusedCountIsNotCachedAsNothing` | `apps/platformkit` · `cache.Memory`, asked of the store | passes with the store holding no key for the failed resource; fails if absence is encoded as a stored zero or a stored empty entry |
| 12 | `TestTheMountRefusesAHighlightThatNamesNothing` / `…ARangeOperatorOnAStringField` / `…ATrendFieldThatIsNotATimestamp` / `…AHighlightOnAResourceWithNoList` | `kit/rest` · `entryHintsFault` over a fixture Spec (the shape `kit/rest/hints_test.go` uses), no database | each passes with the specific sentence of §3 and fails when that clause is missing — four clauses, four names, because a single table-driven case would pass with one clause deleted |
| 13 | `TestTheSummaryEntryIsTheCatalogEntry` | `apps/platformkit` · composition, one caller | passes with `entries`' `(module,entity)` list equal to the catalogue's entries carrying `list` in `operations`; fails when either document narrows alone (a new `Singleton` or `CommandsOnly` resource would be found here, not in a shell) |
| 14 | `TestThePublishedDocumentDescribesTheSummaryBody` | `apps/platformkit` · the golden + `wiregate`, the pair `TestTheCatalogOperationDescribesWhatAShellParses` already forms | passes with `asOf`, `entries`, `module`, `entity`, `total`, `highlight`, `trend.field`, `trend.days[].day/.count` all named in `testdata/openapi.json`; fails when the body is handed to huma as `any` — the type must survive to the schema, which is why the mount is the composition's own `httpx.Register` with a `struct{ Body *screens.Summary }` envelope (the `connectionDocument` argument, `apps/platformkit/connection.go:104-110`) |
| 15 | `TestTheTaskHighlightIsAnsweredInPortuguese` | `apps/platformkit` — joined onto the existing `TestEveryDeclaredHintStringIsAnsweredInPortuguese` | passes once `pt-PT.json` answers `hints.task/task.highlight.label`; fails on the day a label is adopted with no second language, which is that gate's whole purpose |
| 16 | `TestAStatementBudgetLeavesTheRestOfTheBody` | `apps/platformkit` · budget forced small, one resource's `Count` blocked | passes with the answered entries present, the cut ones absent, 200, and one log line naming what was dropped; fails if the whole document 500s because one card was slow |

Two gates that go red on a new door unless they are told, and the row each needs:
`apps/platformkit/openapi_contract_test.go:66-88` gains
`{operation:"app-summary", method:http.MethodGet, path:"/api/v1/app/summary"}` and
`testdata/openapi.json` is regenerated (`UPDATE_GOLDEN=1 go test ./apps/platformkit -run OpenAPI`), with
`wireAuthorizationAllowances()` extended only if `kit/wire` refuses the new operation's authorization
line; `ui/screens`' catalog golden changes because `task/task` now declares hints, so it is regenerated
in the same commit and `TestCatalogGolden` reviewed by eye; and every new `_test.go` file is a row in
`tests/inventory.json` (`./scripts/check_test_inventory.sh` today: "3028 rows agree with the tree; 19
round-named file(s) of 22 ceiling" — this task adds no round-named name, and the round-named ceiling is
at 19/22, so a name like `test_T0337_*` would be a finding on top of a budget).

---

## 9. Reuse inventory → deliverable, piece by piece (decision 0022)

| Deliverable | Composed from |
|---|---|
| the count | *composed from* `kit/crud.Count` (`crud.go:131-135`) — unchanged, still the total; and `CountWhere`/`CountDays` are **new, because** the inventory's `Query.Filter`/`conditions` (`crud.go:95-97`, `:343-355`) reach equality only and `comparable` says so in its own comment, and no grouped read of an entity's column exists anywhere in the tree (`grep -rn "date_trunc\|GROUP BY" --include=*.go kit modules apps` → `kit/events/ledger.go:541` only). They share `conditions`' predicate builder rather than restating `column = ?` |
| the guard | *composed from* `(*API).guard` (`kit/httpx/schemas.go:304-327`) and `Resource.Count`'s own shape (`:129-131`); two new fields, one new wrapping line, no second authorization path |
| the request transaction, the tenant scope | *composed from* `httpx.TxFrom` and the pattern at `apps/platformkit/connection.go:118-122` |
| the address and its OpenAPI entry | *composed from* `httpx.Register(api.Surfaces("").App, …)` + `httpx.AnyCredential()` + the `connectionDocument` envelope shape (`connection.go:66-116`) + `apps/platformkit/fault.go:280-286 workspaceFace`, which is the seam that takes the second mount with **no kernel change** |
| the cache | *composed from* `kit/cache`'s `Of`/`Scope.Entry`/`Get`/`Set`/generation, the way `kit/httpx/tenant.go:117-164` composes them. `GroupOf` deliberately not used (§6) — a deviation from the trace's hint, with its reason stated there so review can refuse it if the reason is wrong |
| the declaration vocabulary | *composed from* `kit/entity/hints.go`'s arrangement: the list lives in `kit/entity` (`Icons`, `Tones`, `Formats`, `Visibilities`) and the refusal in `kit/rest/hints.go` — `Anchors`/`ValidAnchor` follow it; `EntryPredicate` is new because no declaration has ever named an operator before |
| the wire echo | *composed from* `ui/screens/hints.go`'s `declared`/`deviations` pointer-per-key rule; `CatalogVersion` stays 2 by the rule at `catalog.go:22-33` |
| the answer body | **new file, because** `ui/screens` has no per-resource numbers today (`grep -rn "asOf\|AsOf" --include=*.go kit apps ui` → nothing) — and it belongs in `ui/screens` because every other `/api/v1/app` body lives there (`Catalog`, `Connection`) |
| copy | *composed from* `ui/resource/hints.go`'s `hints.<module>/<entity>.<aspect>` grammar + `Words.Entry` + the module's own `pt-PT.json`; no new table, no new `en` file, and `make check-i18n` unchanged |
| the web twin | *not rebuilt*: `modules/admin/internal/pages.go:520-546` stays as it is (T-0349 owns that page). Its test `modules/admin/module_test.go:581` is the regression to keep green — and nothing in this spec changes `Count`'s semantics, so it stays green by construction |
| the phone | *not touched*: `/home/jplr/gitrepos/pkit/platformkit-mobile` is T-0338's checkout, outside this worktree |
| the migration | *composed from* the task module's own ownership (`modules/task/migrations.go`, adopted at `module.go:197-198`, `docs/adr/0011-migration-ownership.md`); a partial index in the shape `tasks_tenant_sla_deadline` already sets, and **this replaces the trace's "no migration needed"** |
| the test doubles | *composed from* `countAuthorizer`, `kit/db/dbtest`, `cache.Memory`/`cachetest`, `deviceComposition`/`install`/`start` — no new fake type |
| the docs | `docs/cache.md` gains one row (the policy is owned there), `kit/rest/README.md`/`kit/entity/README.md` gain the two hints' sentences; 0085's own record lives in the record repository and is not edited here |

Nothing here rebuilds a unit the inventory named. The closest calls are named openly: `CountWhere` (the
inventory offered a grammar that cannot ask the question), `GroupOf` (offered, refused, reason given),
and `api.Cache()` (below).

`func (a *API) Cache() cache.Cache` in `kit/httpx` is **new, because** the store is reachable only as
`a.opts.Cache`, private, and the composition that owns a `/api/v1/app` route has no other way to hold it:
`kit/app` builds the store internally (`app.go:712-733`, passed to `composeRoutes` →
`httpx.New(Options{Cache: store})` at `:780-783`) and `Options.WorkspaceCatalog` hands the composition
nothing but the `*httpx.API`. It is one accessor beside `Resources()`, documented with the sentence that
matters: *the caller re-asks its own authorization before consulting the store* (§6.1). Building a second
store in `apps/platformkit` was the alternative and is wrong — two stores means the invalidations the
kernel makes would never reach this one.

---

## 10. Third-party libraries

None added. The names this spec relies on are already required by `go.mod` and compiled into
`go build ./...` (which exits 0 today — §11), so no registry lookup is offered as proof of anything new;
what is offered instead is where each is decided here:

```
$ go list -m github.com/danielgtaylor/huma/v2 gorm.io/gorm github.com/google/uuid
github.com/danielgtaylor/huma/v2 v2.39.1
gorm.io/gorm v1.31.2
github.com/google/uuid v1.6.0
```

| Module | Version in this build | Licence, read from the file in the module cache | Used by this task |
|---|---|---|---|
| `github.com/danielgtaylor/huma/v2` | `v2.39.1` | `LICENSE.md` — "Copyright 2020 Daniel G. Taylor / Permission is hereby granted, free of charge…" (MIT) | the operation registration, unchanged (`httpx.Register` + the envelope struct) |
| `gorm.io/gorm` | `v1.31.2` | `LICENSE` — "The MIT License (MIT)" | the two new count statements, through the same `tx.DB()` the rest of `kit/crud` uses |
| `github.com/google/uuid` | `v1.6.0` | `LICENSE` — "Copyright (c) 2009,2014 Google Inc.…Redistribution and use in source and binary forms" (BSD-3-Clause) | `cache.Of`'s tenant argument, unchanged |

No GPL or AGPL dependency enters, and no new program is added to the deployment.

---

## 11. Commands run in this round (specify; nothing is built, so nothing that needs a database could be judged here)

```
$ make fmt-check check-loc check-packages check-gucs check-ui check-richtext-bundle check-versions \
      check-run-owner check-pillars check-step-library check-test-inventory
…
./scripts/check_test_inventory.sh
INVENTORY: 3028 rows agree with the tree; 19 round-named file(s) of 22 ceiling; 1025 test(s) of 3028 in the push tier
EXIT=0

$ timeout 900 bash -c 'go build ./... && go vet ./...'
EXIT=0                     # both halves, no output

$ go vet ./modules/T-0337-the-kernel-counts-what-each-resource-holds/...
stat …/modules/T-0337-the-kernel-counts-what-each-resource-holds: directory not found
                                            # this task adds no module; see §1
$ go test ./modules/T-0337-the-kernel-counts-what-each-resource-holds/contracts/...
stat …: directory not found                     # as above
```

`make build` and `make check-apidiff` are the two goals of the round's gate list that the loop's heavy
slot admits (`tools/shims/make`: `build` and `check-apidiff` wait; the ten static `check-*` targets above
do not). They are running in the background of this round as of writing, queued behind the suites other
tasks on this host are running; their result is in `specify.1.md` if they return before the cap, and the
runner will re-run both in any case. Not verified here, and not claimed: that `make build` and
`check-apidiff` exit 0. What supports the expectation is that the tree is unchanged (§11's `go build
./... && go vet ./...` at exit 0 is the same compilation `make build` performs) and that
`check-apidiff` refuses *incompatible* exported changes against the `v1.1.0` baseline, while everything
§1 adds is additive (new exported names, new optional wire keys).

The count boundary itself was already executed by the trace and is green at this revision
(`TestResourceCountChecksPermissionOnceAndLoadsNoRows`, run against a database named explicitly); this
round ran no database test, and the loop's own ports (61349/62349/63349/60349) still have nothing behind
them, which the implement round must fix with `make up` or by naming a database in its `Verified:` line.

---

## 12. Brief items renamed, split, dropped

| Brief item | What happened | Why |
|---|---|---|
| "one query per entry, bounded" (Decided 2) | **replaced** by "at most three statements per entry, one per declared question, in the request's transaction, under a 5 s budget" | the FILTER form that would be one statement reads the tenant's whole table; the narrow form reads an index for both declared questions. The concern (bounded, no fan-out) is kept and is now falsifiable per statement (§4.3) |
| "Adopt on tasks (**total open**, …)" (Decided 4) | **split, and one half dropped**: the entry total stays the *whole* count the list reports, and the highlight carries the declared sub-count | two "totals" for one resource, one on a card and one in the list header, is one number to a person and the card links to the list; the mechanism that expresses "open" is a highlight with `label:"Open"` and an equality filter on `status`, which is one line in `modules/task/module.go` and is the product's choice (§13). The brief's own "Due today" highlight is what ships |
| entry presentation `highlight: {label, filter}` (Decided 1) | **kept as the declaration**, and *split* from the response: the declaration is echoed in `presentation`, the answer is the summary's bare `highlight` number | a word spelled in two documents is two copies of one string (`ui/resource/hints.go:10-27`); the summary is numbers-only, which is what lets it carry no `Vary` and no language in its cache key (§2.3) |
| "`asOf`" (Decided 1) | **kept, and narrowed in meaning**: the instant the request was answered, *not* a consistent snapshot, and every number is at most `summaryTTL` older than it | a body assembled from cached entries and three statements per entry cannot be a snapshot, and a promise it cannot keep is worse than the sentence in §4.4 |
| "optional per-entry daily counts for the last 14 days (`trend`)" (Decided 3) | **kept**, with the 14 buckets materialised by the *document* (`ui/screens.Summarise`), not left to the query's result rows | a missing bucket is a zero, and a body that omits days makes the shell decide the difference between "no rows" and "the server did not answer" — the same confusion as absence-vs-zero, one level down |
| "cached briefly per tenant **and person**" (Decided 2) | **changed to per tenant**, with the guard sequenced before the key (§6.1) | the kernel decides per (tenant, permission) and has no per-row grant; a person-keyed cache is slower and no safer, and would name a person in a key that reaches a log line. The safety moves from the key to the sequence, where the acceptance case #10 can fail it |
| 0091 D3's "`resolved`" | **not added**, as the brief instructs | T-0403's policy carries the hint into `resolved`; this task ships the hint only |

## 13. Left to the product (Limits)

- **Which number Home prints, and its words.** The kernel answers the whole count and at most one
  declared sub-count; the phone (T-0338) and the web dashboard (T-0349) choose. Choosing "Open tasks"
  instead of "Due today" is one line in `modules/task/module.go` (`label:"Open"`, one equality predicate
  on `status`); wanting both at once needs `highlight` to become a list, which nothing has asked for and
  which is a version decision for `CatalogVersion`'s own rule to judge.
- **A person's day.** `today` is a UTC day because the kernel holds nobody's timezone. A product that
  wants a person's own midnight adds a timezone at composition and must, in the same commit, put it in
  the cache name (§6) — the day boundary is an input to the answer, and the key names every input.
- **Refresh policy, skeletons, "Couldn't refresh. Showing the last update from {time}."** All 0085's
  copy table is the shell's; the summary gives it `asOf` to say that sentence with.
- **Deep-linking a highlight into a filtered list.** Not here: the public list grammar
  (`?filter=field:value`, `kit/rest/rest.go:1492-1529`) stays equality-only, and a shell must not compute
  a window from a declared `filter`. If a product wants that chip, the honest form is a *new* parameter
  spelling (`?range=field:op:value`), not a new meaning inside `?filter=` — the existing grammar's two
  segments cannot take one without changing what `"filter":"status:eq:open"` has always meant, which no
  diff tool would catch and no shell would expect. That parameter, its coercion, its own cases and its
  effect on the generated list screens are a product task with its own brief; a *second* consequence
  waits there too: whether `highlight` is what a card taps.
- **A second consumer of `kit/cache`.** This is that second consumer, so `docs/cache.md`'s "Not yet in
  this policy" bullet retires with this change and the table gains the row in §6 — the doc edit is
  this task's, not a later one's.
- **The `Anchors` vocabulary**: three words, frozen. A product that needs "this week" asks for the fourth
  word, and adding one refuses nothing that mounts today.

## 14. Open decisions I did not leave to anyone (decided here, module's share)

1. `summaryTTL = 15 s` (host resolution's 30 s bounds a *resolution*; this bounds a *count a person acts
   on*, and a foreground refresh must not be able to see the same stale fifteen seconds twice in one
   read). 2. `summaryBudget = 5 s` per request, per-entry absence on overrun. 3. Range operators on
   `TypeTime`/`TypeInt`/`TypeFloat` only. 4. `Anchors` = {`now`,`today`,`tomorrow`}. 5. 14 buckets owned
   by `ui/screens`, one `TrendWindow` function so the composition and the document cannot disagree.
   6. `(module, entity)` as the join key, no address in the body. 7. The cache namespace is `summary`,
   the name `<module>/<entity>[/<UTC date>]`, the value the per-entry answers and never the body.
   8. `api.Cache()` as the seam, and not a second store, and not a new `app.Options` field.

SPECIFY: complete
