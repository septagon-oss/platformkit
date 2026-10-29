# Database connection limits

`Open(ctx, url)` retains 16 open connections, four idle connections and a 30 minute
maximum lifetime. Use `DefaultPool`, change the required fields, then pass the
value to `OpenWithPool(ctx, url, pool)` for an independently configured pool.
Both constructors refuse a superuser or BYPASSRLS application role.

`Pool.Validate` rejects unbounded/negative open limits, idle limits outside the
open limit, and negative lifetimes. Explicit zero idle connections disables
reuse; explicit zero lifetime disables retirement. `Conn.Stats()` exposes
standard `database/sql` pool occupancy and cumulative wait counts/time, without
exposing connections or a way around the scoped transaction API.

The application maps optional `database.max_open_conns`, `max_idle_conns` and
`conn_max_lifetime` fields to these limits. Omission retains the defaults. It
validates them before migration or startup. Every app role requires at least two
open connections: HTTP handlers open detached transactions while retaining their
authentication transaction; jobs hold an advisory-lock connection during work.
This minimum does not prevent saturation by concurrent requests that each hold a
connection while waiting for another; independent `OpenWithPool` still allows one.
Budget database connections across all processes, including web/worker replicas,
migrations and maintenance. A larger pool does not establish greater throughput;
use the [tenant-work benchmark](../jobs/README.md) to measure a concrete workload.

## Test suite capacity

`dbtest.Open` and `OpenFor` cap each owner pool at four open connections and one
idle connection, without a lifetime limit. The same policy covers schema/role
cleanup and private-database setup/removal. Four leaves room for tests that hold
a lock while a writer and an observer use separate sessions. `dbtest.Schema`
retains the application defaults above; test cleanup closes both handles.
The events package uses `dbtest.RunInOwnDatabase` to isolate durable advisory
locks from other packages; schemas do not scope database-wide lock keys.

The Makefile runs `test`, `check` and `check-race` with four package processes.
Compose and the CI PostgreSQL services allow 256 connections, including three
reserved for superusers. Count every pool across the active packages, including
multiple application boots, migration sessions and cleanup; four packages do not
mean four pools. When growing the suite, sample all client backends in
`pg_stat_activity` during a fresh `make check`, including unnamed clients and
all databases. Keep the observed peak at most 224 including the sampler, leaving
32 physical slots for maintenance, health probes and short overlaps that sampling
can miss. Finite sampling reports an observed peak, not a mathematical bound.

### Composition

**Reused** — `database/sql` limits, `dbtest` fixture cleanup, `db.DefaultPool`,
and the existing Makefile and PostgreSQL service definitions.
**Added** — one private owner-pool opener and its limit proofs, because fixture
owner handles previously had no connection maximum.
**Made reusable** — `dbtest.Open` and `OpenFor` expose the shared test-owner
pool policy to tests and benchmarks.

### Limits

The owner policy covers pools opened by `dbtest`. Raw `sql.Open` handles in
`kit/db`'s independent fixture and session probes, and in `pkit` tests, retain
their existing owners and settings. Include their sessions when measuring capacity.
Package parallelism bounds processes, not fixtures: parallel tests and multiple
runtimes can keep several pools alive in one process. The 224-client target needs
a full-suite observation; multiplying one fixture's limits is no strict suite bound.

# Migrations

`Migrate(ctx, url, sources...)` applies each capability's files in owner and version
order, one file and its history row per transaction. What a file may say about
itself, and the rules it is refused by, are the schema author's reading:
[migrations/README.md](../../migrations/README.md). This section is what the runner
itself guarantees.

**Budgets.** Every file runs with `lock_timeout` five seconds and no
`statement_timeout`, re-asserted on the runner's session before each file so a file
that sets one for itself cannot leak it to the next; `database.lock_timeout` and
`database.statement_timeout` change them, and `MigrateWithOptions` takes the same values
in its `MigrateOptions.Budget` (`MigrateWith`, `MigrateDeclaring` and their `Backfill`
partners are that one door's older spellings and are deprecated in its favour). The budgets are the runner's patience, not the operator's: a
migration that cannot take a lock in five seconds of *waiting* stops. The statement
budget defaults to no bound because a legitimate index build on a large table is the
statement a duration bound would kill. **The one exception is the file in autocommit
mode, which runs with the lock budget off and the statement budget as configured.**
What that statement waits for is the transactions
already in the database ceasing to be able to see the table — the wait `CONCURRENTLY`
exists to make — and the server implements it as a lock wait (`wait_event_type=Lock`,
`wait_event=virtualxid`), so a configured lock budget reaches it and cancels it:
measured on PostgreSQL 16, a build that finds one session holding a snapshot older
than its own is cancelled at `55P03`, and what it leaves behind is an INVALID index
that the file's own `IF NOT EXISTS`, the re-runnability the rule table demands of this
mode, then excuses on the retry the refusal tells the operator to run — the rerun
reports the file applied and the index does not exist. Two things about that wait are
wider than the sentence above them, and an operator needs both. It is any older
transaction, not one touching the table being built: measured on the same server, a
build of one table waited 5.0s for a transaction whose only lock was a row of another.
And with the lock budget off, nothing the runner owns ends the wait — `statement_timeout`
is off by default and `app.Bootstrap` hands the run a context with no deadline of its
own, so a boot stopped at this file waits as long as the oldest transaction in the
database does. The one bound a deployment can put on it is
`database.statement_timeout`, which is therefore a trade and not a safety: it refuses
the build at that bound, and leaves the INVALID index above for the retry that then
reports the file applied. Waiting is also the shape of the queue for the composition
lock, which has no budget on it either: that wait is taken off the session rather than
left on it, because the lock is taken a second time mid-run, after an autocommit file,
on a session that by then still carries the statement budget — the lock budget came off
for that file, and a statement budget bounds a lock wait as surely as it bounds the
work done inside the lock. The same two values go on the session before
every batch of a drain (`BackfillWith`, and through it `app.Drain`,
`jobs.BackfillMigrations` and `platformkit migrate --drain`), because the fifty
transactions behind a data file are the half that waits longest for rows the running
application is holding: an operator who shortens the wait because a boot must not sit
on a busy table has to get it for the work, not only for the file beside it.

**Contention is not failure.** A file stopped by that budget returns
`db.ErrContended` naming the file and both budgets; nothing it had not already
applied was applied, the files already applied stay applied, and the operator may
run it again. The runner does not retry inside itself, because it holds the
composition's advisory lock while it waits, and a queue inside that lock stops every
other replica's boot behind it. The retry is a command rather than a loop:
`platformkit migrate` is `app.Migrate` — the same sources, floors and budgets every
role's boot composes — for somebody who is not deploying, and `--drain` finishes a
backfill the migration left behind instead of waiting for the worker's tick.

What the budgets are *not* put on is the window one `autocommit` file opens: its
statement runs with the lock budget off, and the `pg_advisory_lock` that takes the lock
back after it waits with both budgets off. Both halves are left patient, and each is
bounded by what is left on it — the caller's context for both, `database.statement_timeout`
for the statement alone. ADR 0011 is the reason for each half in turn. That statement is
the one a run must not hold the lock across, and it waits for the transactions already in
the database — that is what `CONCURRENTLY` is for, and no `lock_timeout` bounds that wait;
a rebuild cut short at a budget is not a refused file but a half-built index left standing
invalid. The wait to take the lock back is the queue of boots this upgrade is ahead of,
which no other file's patience describes: the replica that gets the lock second has the
first one's applied files to read and finds nothing pending, so a boot that waits and
applies nothing beats one that refuses at five seconds and is read as a failed deploy. The
statement budget is what survives of the window, because it is the bound the deployment
named and ADR 0011 takes none away: with no duration bound either, nothing the runner owns
would end a build that waits behind a transaction that never ends. The budgets go back on
before that file's history row, so what they bound is what a file waits *inside a
transaction* — which is every other statement of the run, and every batch of a drain. When
the caller's context runs out the operator gets a context deadline, not `ErrContended`:
nothing was refused, the run did not finish.

One answer to that patience is neither a refusal nor a deadline: being picked out of
somebody else's deadlock (`40P01`) cancels an ask while the run holds nothing, because the
ask is the first statement the session has sent and the cycle is already broken by the
cancelling. The ask is therefore re-issued, four times at most, and a database that picks
the same session every time is reported with the server's own words and its files
unapplied. `kit/db/composition_lock_deadlock_test.go` raises `40P01` of the ask from the
catalog side and pins both answers.

**What each file cost.** Every applied file logs `db: applied migration` at info
with the runner's own `duration_ms`, and a data file's line carries `batches=` beside
it — the transactions it committed, which is what tells three windows from fifty at
the same duration. A schema file writes no such number: it has one transaction and
nothing to count. Every finished drain logs `db: drained data migration` the same
way, and each batch that commits logs `db: backfill batch` at debug with the rows it
wrote, the cursor it left and how long it took — the line an operator watching a drain
over a big table reads, which is otherwise only a row in a table. The runner measured
them from inside the transaction or the batch, which is the only place that knows
where a file began; the rehearsal reports those numbers rather than an estimate taken
around the process. Contention is not a fifth line: it is the returned error, logged
once by whoever has the run.

**Two tables, not one.** `schema_migrations` says what is applied forever;
`schema_migration_backfill` says where an unfinished drain restarts, holding the
last key it committed, and its row is deleted in the transaction that writes the
history row — which is the transaction that wrote the drain's last window, not one after
it, so there is no moment at which every row is written and the ledger still reads as a
drain to resume. Both are revoked from the application role: a ledger an application can
edit is a release that never happened, and a progress row it can edit is a backfill
that repeats rows. Neither table is there for a run that applied nothing: the rule table
answers from the files, before the runner opens a connection, so a run it refuses opened
no connection and left no ledger to query — the log line names the rule and the catalogue
says what is in the database, which is nothing of that owner. The boundary is the same one
a refused command keeps: a refused write writes nothing, the runner's own two tables
included.

**The drain, and who runs it.** A `phase=data` file's body is wrapped over a window
of its table's primary key and run once per window, one committed transaction per
window — unless the file said its body bounds itself, in which case it runs once, in
one transaction. The key may be of any single-column primary key type: the window and
the top of it are both asked of PostgreSQL's ordering — `ORDER BY key LIMIT n`, then
`ORDER BY key DESC LIMIT 1` — rather than of an aggregate, and the key travels as its own
text rendering and comes back as a comparison against that type's own name, read from the
catalogue. So the drain keeps no list of the types it is willing to name, in either
direction: `max` does not exist for `uuid` or `bytea`, which is what every entity table in
this repository is keyed by and what `modules/auth` keys its token hashes by, and an
ordering operator is all a window ever needed. What the ledger holds as a NULL cursor is
"no window has committed yet", not an empty string, because for a table keyed by `text` the
empty string is a key — the smallest one in every collation — and a cursor that could not
tell the two apart took the same window again forever over the row keyed by it. What it
refuses is a table it cannot window over at all — one keyed by two columns, or a
`table=` no selected owner creates —
naming the key it could not find or the table that is not there, and writing neither a
history row nor a progress row. `migration.windowed` answers that one question, once, and the rule
table and the executor both take the answer from it — of one reading of the body
(`scanSQL`: comments gone, case folded, and the contents of every string literal put
away, because two dashes inside a value are data and not the comment that would
otherwise hide the rest of the line from the guard; and the boundary of a comment and
of a literal is taken wherever PostgreSQL takes it, across a `/* … */` that nests and
spans lines, a `$tag$ … $tag$` value that carries apostrophes as data whatever letters
its tag carries (a tag is a name, and in a UTF-8 database a name takes the database's
own letters, not ASCII alone), and an `E'…'` that ends past its own escapes) — so the body the rule table
judged is the body the executor runs, and a body that names the window only inside a
value it writes is neither wrapped nor excused by accident.
An installation with no history for that owner drains it during migration, bounded at
fifty batches, because nothing is reading and the rows are the ones the installation
just wrote. An owner that already has history is a table under readers, and what decides
which of the two runs the drain is what waits behind the file. With files pending behind
it, `Migrate` stops there, applies nothing further for that owner and returns nil — this
run cannot reach those files either way, and may not spend itself emptying a hot table to
get almost there — and `jobs.BackfillMigrations` — composed into the worker by `kit/app`
as `schema-backfill` — is what finishes it through `db.Backfill`. With nothing behind it,
filling that column is the release's last step and one the run can finish, so it drains
the file itself under the same fifty-batch bound: a table longer than the bound ends that
run with `ErrBackfillBudget` and the tick takes the rest. A body that said it bounds
itself has no window to count, so that bound holds nothing to it and it stays the
worker's even when it is last. A boot that refused a drain would stop the only role that
can finish it, and a tick that never ended would stop everything behind it: the worker's own
drain is bounded too, at ten thousand windows, because a run that holds the `schema-backfill`
advisory lock while it repeats work is worse than one that stops, reports
`ErrBackfillBudget` with its batches and cursor standing, and is continued by the next tick.
Neither answer to a drain in flight
is a failed boot: a run that resumes one and reaches its own bound returns
`ErrBackfillBudget` with the committed batches and the cursor standing, and `kit/app`
logs that as the work its tick still has rather than refusing the start — the same
failure one bound down. `platformkit migrate` and `Bootstrap` asked for a run that
finishes, so those doors keep the error.
A refusal of a data file leaves nothing resumable: the progress row means "this drain
started, resume it", so a shape the window cannot run is refused before that row exists,
and three shapes are that window's own: a body of two statements; a body whose own CTE
list binds the window's name `batch` (PostgreSQL answers two members of one CTE list by
refusing the statement, so such a file would be re-refused by every later run); and a
body that moves the key set the cursor runs over — one that writes the column the cursor
is ordered by, which puts the rows it touched back above the position the drain just
committed, or one that puts *new* rows into the table it drains, which stands keys above
that position that no window ever ordered. Either way the table never empties and each
window behind the first re-commits rows an earlier one wrote. The first is counted; the
other two are read as constructs and not as spellings — the members of the list the
wrapper joins, with each name taken in either spelling the server accepts and a list
inside a sub-expression left to the scope that shadows it, and, for the key set, which
table each statement of the body writes: the assignment targets of the UPDATEs whose
target the reader can name, in both shapes PostgreSQL writes a column with and over
every spelling it takes for a target (`ONLY` and the parenthesis it may carry, the
schema qualification, the `*`, the alias with or without its `AS`), and the target of
every INSERT and MERGE. An upsert's `DO UPDATE SET` and a `MERGE` arm name no target for
the list reader but do for the statement carrying them, which is where a re-key written
that way is refused; what the reading leaves to the bound is a body that reaches its
table without naming it — through a view over it, or a function the server runs or a
trigger the table carries. A body that merely *opens* with a CTE list of its own is none
of them: the window joins that list, `RECURSIVE` included, because PostgreSQL takes one
`WITH` per statement and pasting a second in front of the body answered a file the
kernel had assembled with the server's own syntax error, after the progress row. A key
merely *mentioned* — in the predicate, as the source of another column's value, or in
the words of a value the body writes — is none of them either, and neither is another
table's key of the same name, nor a body that only takes rows *away*, which moves the
key set the one safe way.

The guard cannot make that refusal instead of the executor: a guard refuses a whole owner
before any of it runs, and the wrongness here is the window's own — the owner's earlier
file has applied, and the version behind it is one statement too many. How many statements
that is comes from the cut PostgreSQL makes, not from the reading the rule table reads a
dollar body inside: a `;` inside a value the body is writing leaves the body one statement,
and the window wraps it whole. A rule that over-reads a construct can be answered with
`allow=` and a sentence; this refusal, `data-with-ddl` and `autocommit-not-rerunnable` — the
three that read a statement split and state no exception — can be answered with nothing, and
a refusal an author cannot answer may not rest on an approximation. The same argument reaches
the two rules that ask after the word `CONCURRENTLY` and state no exception either: they ask it
of the file's own SQL with the contents of every value put away, because the word inside a
value the file is writing says nothing about what the file runs, and a reading wrong about that
left a `phase=data` file refused by one of them with every answer refused in turn — the marker
as a bypass, `autocommit=true` as a key a data file may not carry — and excused the other for
the same word. [migrations/README.md](../../migrations/README.md) holds the table, which rule
reads which text, and what each reading gives up.

**Refusals below the rule table.** Four refusals are not judgements about a file's text but
facts about this database — a table that is not here, a key the window cannot walk, an
expansion the ledger has not seen, a drain past its bound — so no `allow=` reaches any of
them, and they are named as the rules are, with the id in front of the sentence that
explains it (`refusal <id>: …`) because a log line is what an operator greps from:

| id | who refuses | what the run says |
| --- | --- | --- |
| `data-table-missing` | the drain, before a progress row exists | the `table=` a data file names is not in this database — a file of another owner that was not selected, or a release that has not applied yet |
| `data-key-not-primary-key` | the drain, before a progress row exists | the table has no single-column primary key to window over; a table keyed otherwise needs a drain its owner owns, in a job |
| `contract-without-expansion` | the plan, after the ledger is read and before any file of that owner runs | the contract half waits for an `expand=` version this installation has not applied — or one no release of this owner can have applied, because the version is not a file before the half that names it, which is bounded the way a source's `RulesFrom` floor is and for the same reason — and nothing of the owner applied |
| `backfill-exceeds-install-budget` | the inline drain, and one tick of the worker's, as `db.ErrBackfillBudget` | the bound that run gives itself was reached — fifty windows for a drain a migration performs, ten thousand for one a tick performs, and the sentence carries the count it stopped at; the committed batches and the cursor stand, and the next run finishes the table |

`kit/db/refusal_names_test.go` walks each of the four through the run that refuses it and
reads the id back off the message, counting the ids it is counting off every non-test file of
the package rather than off `refusals.go` by name — an id *is* the constant
`refusal<Name> = "…"`, wherever the runner keeps it, and a gate that opened one file by name
could only ever hold the ids that stayed there, which is the tenth review's fourth finding.
`kit/db/refusal_ids_are_the_table_test.go` reads this table against the ids
`refusals.go` declares and refuses the two lists to differ, and
`kit/db/refusal_id_is_declared_wherever_the_runner_keeps_it_test.go` refuses an id
whose home is not that file: the file the two lists are read from stays one file however the
code is arranged, which is what makes the promise below hold and not merely happen.
Between them the two halves of that promise hold: an id named here that nothing prints, or a
printed id nothing names, fails one case or the other rather than drifting. A contended file
is a report and not a refusal — it is `db.ErrContended`, and the paragraph above states what
it promises.

**The rehearsal.** A release is rehearsed against a copy of a production-shaped
database before it is published: `make rehearse`, `scripts/rehearse_migrations.sh`,
with the interface, the four exit codes and the thing it cannot measure written down
in [migrations/README.md](../../migrations/README.md).

### Built on what came before

Decision 0022 asks a delivery to name what it composed rather than what it rebuilt.
**Reused:** the runner's own doors — `Migrate`, `pendingMigrations`, `(*runner).apply`,
the composition advisory lock and the `REVOKE … CASCADE` sweep, with
`schema_migration_backfill` created beside the ledger by the same statement list and
taken through the same sweep; `kit/events/relay.go`'s batch shape (one transaction
per batch, loop until a short one) for the drain, scheduled through `kit/jobs` with
the advisory lock `Scheduler` already takes by name; `dbtest.URLs` and
`fstest.MapFS` sources for every case, because the only double for a SQL service is
the real service; `scripts/e2e.sh`'s shape for the rehearsal — `set -euo pipefail`, a
database name of its own, a cleanup trap that refuses any other prefix;
`apps/platformkit/postgres-init.sql` as the one source of the application role and
its grants, and `bootstrap` as the base revision's own migration path.
**Added:** the header grammar, the rule table, the three execution modes, the two
budgets and `ErrContended`, the progress table with its compare-and-set cursor, and
the rehearsal step — none of them had an owner, because the runner knew nothing about
what a file was for and could not be asked what it cost. **Made reusable:**
`app.Migrate` and `app.Drain`, so a retry is not a second definition of migrating;
`db.MigrationBudget`, `MigrateWith` and `BackfillWith`, which every existing caller
gets without changing a line; the per-file `duration_ms` log lines the rehearsal
parses;
`scripts/testdata/rehearse/seed.sql`, the ten-thousand-row fixture any future
rehearsal or size-shaped test composes; and the batch-with-a-cursor pattern, which
`events.Purge` is the next candidate for.

# The transaction's owner

A tenant transaction opened through a `Pending` (`tx.go`) has two kinds of context, and they
are never confused.

The **owner** is the context the `Pending` was created with: the request's, for `Lazy`, or the
caller's, for a standalone `Run`. `BEGIN`, the `set_config`, the transaction span, the settings
re-read and `COMMIT` all run under it, because database/sql binds a transaction's life to the
context it began with. When the owner ends before `Close`, database/sql rolls the transaction back
and releases the connection, `Close` reports the owner's cause whatever `keep` asked for, and no
later `Tx` opens: a request whose client went away commits neither its rows nor its outbox events,
even when its handler answered 2xx.

An **operation** is each caller of `Tx`, each nested `Run`, and each `TxFrom` on a context already
inside the transaction. It gets a clone of the shared GORM handle bound to its own context, so its
statements carry its deadline, its values and the transaction span, and the shared handle is never
changed. An operation ending stops its statements alone. If PostgreSQL aborted the transaction
underneath — a statement cancelled mid-way — nothing later commits it: there are no savepoints.

`InTenant` also bounds its borrowed handles to the provisioning callback. Nested `Run` and `TxFrom`
calls retain that restriction even when their caller uses `context.WithoutCancel`: their own
deadlines and values still apply, but every borrowed handle is cancelled before system access is
restored. A saved callback context cannot open a fresh usable handle after the callback has
returned.

What follows from that:

| Event | Outcome |
|---|---|
| An operation ended before the transaction opened | that `Tx` is refused; the next live caller opens it |
| The owner ended before it opened, or while it waited for a connection | refused with the owner's cause, not remembered by `Err`, so `Err` still means the database refused |
| The owner ended while it was open | rolled back; `Close` returns the owner's cause, and it is a confirmed rollback for `AfterRollback` too — see below |
| The owner ended after `Close` committed | reported as committed; `Close` is idempotent |
| `Close` under a live owner | the `Pending` may open again: the streamed upload route commits its guard transaction and its handler opens a second one |

Limits. Waiting for a pooled connection is bounded by the owner, not by the first caller: an
operation that ends during that wait gets a handle whose first statement fails, and the transaction
stays open for the next caller. Owner cancellation does not interrupt a running statement whose
context does not derive from the owner; every in-tree caller's does. A cancel that lands during the
`COMMIT` round trip is reported as not committed, though PostgreSQL may have committed. `RunSystem`
keeps one context for its whole transaction: its caller is owner and only operation.

## An effect that waits for the rollback

`AfterRollback(ctx, action)` (`after_rollback.go`) registers the effect whose reason to exist is
that the work was discarded — bytes no committed row will ever name — on the transaction
discarding them. Decision 0022, for this delivery. **Reused:** `afterQueue`, the queue
`AfterCommit` already drains, one per opened transaction; `current`, the lookup reaching a `Run`
frame, a joined nested `Run` and an opened `Pending` alike; `Detached` with `WithConn`/`ConnFrom`
for the context handed an action; `ErrNoTransactionToDefer` for its refusal. **Added:** a second
queue on `Pending` and `openTx`, and `ending` — `end` naming the outcome it confirmed instead of
leaving `finalise` to infer one. `AfterCommit` runs on a commit and nothing else, and an effect run
on an unknown outcome would delete behind committed rows. **Made reusable:** inside the
package, one place that decides which outcome happened, which `finalise` reads instead of
inferring one; for a caller, a rollback-side hand-off no one has to wrap a `ConnPool` to get.

| Ending | The commit queue | The rollback queue | The caller is answered |
|---|---|---|---|
| `COMMIT` clean | runs | dropped | nil |
| `COMMIT` itself refused | dropped | dropped: the outcome is unknown | the driver's error |
| the settings re-read refuses, `ROLLBACK` clean | dropped | runs | `ErrScopeTampered` |
| `ROLLBACK` clean | dropped | runs | nil |
| `ROLLBACK` refused | dropped | dropped | the driver's error |
| the owner had already ended | dropped | runs | the owner's cause |
| nothing open | nothing to run | nothing to run | nil, which is why ending twice runs the work once |

The owner-ended row is the one to read twice: `database/sql` binds a transaction's life to the context
it began with, so an owner that ended had its `ROLLBACK` issued already and its rows can never become
visible — measured over 40 such endings, our own `ROLLBACK` answered clean none. A caller that committed
out of band through a borrowed handle would make that answer mean *committed*, and no in-tree code does.

Limits. The queue is memory, not a row: a process that dies after the `ROLLBACK` loses it, so an
effect that must not be lost needs a row and a sweep. Actions run before `Run` or the request
returns, so their latency is the caller's, on a context detached from the transaction, no longer
cancelled by the owner and bounded by five seconds. What an action needs from the database it reads
before registering: a nested `SAVEPOINT`/`ROLLBACK TO` can un-name a row in between. A system
transaction, a `Pending` nobody opened, a transaction already ended and a nil action are refused —
and so is a registration on the frame a `Run` handler kept past its transaction's ending, because a
queue that ending took runs nothing whatever still points at it. Work an ending dropped is the
outcome's own decision, but it is memory and its transaction is over, so its count and the ending
that dropped it are written to the log before they go: at debug for the drop a confirmed ending
always makes, as a warning for the ending whose outcome the database left in doubt — the one
whose work is gone behind an answer that may never say so.

## An effect that waits for the commit

Decision 0022, for this delivery. **Reused:** `Pending`, the one commit site
(`Close`), `Run`'s join, `Detached` as the context a deferred action is handed, and
`httpx`'s position for the application connection — which now lives here
(`WithConn`/`ConnFrom`), because a delivery runs on the same pool and code that
writes after its transaction has ended must read one name in both. **Added:**
`AfterCommit`, `ErrEffectNotRun` and the queue on `Pending` — nothing existing could
carry them: `Detached` commits *earlier*, and the outbox cannot hold the one effect
this exists for, a secret that may not become a row. The queue is the transaction's,
not the `Pending`'s: `open` starts one and `end` hands it over or drops it, so the
actions registered to a rolled back transaction are dropped with its rows and a
request that reopened under a live owner runs only what the transaction it is ending
registered. **Made reusable:** a hand-off
any module can register on, and an error that separates "committed, effect missing"
from a rollback, which is the fact every retry underneath this has to branch on.

## Composition of migration steps

**Reused** — `MigrateDeclaring`, the existing runner, history ledger and backfill
cursor remain the execution path. **Added** — private source/file/rule/adoption
steps and separate history, planning, runner, window and SQL-reader files make
those existing responsibilities independently readable. **Made reusable** —
source validation steps shared by migration and backfill entry points.

The public inputs, entry points and composition-lock window remain in [migrate.go](migrate.go).
[History](migration_history.go), [planning](migration_plan.go) and
[execution](migration_runner.go) retain the same pinned connection and lock order.
[Backfill lifecycle](backfill.go), [windows](backfill_window.go) and
[body checks](backfill_sql.go) share the [SQL reader](migration_sql.go) with
[migration rules](migration_rules.go). [Source reading](migration_files.go)
finishes before connecting; an out-of-range rule floor is still judged against
pending history before any pending file executes.

## Composition of the call-site doors

**Reused** — the runner's own doors: `MigrateWithOptions` and `BackfillWithOptions` are
`MigrateDeclaring`'s and `BackfillDeclaring`'s bodies with the budget and declaration read
out of one `MigrateOptions`, `Commit` and `Rollback` are `Close`'s `finalise`, `TryLockErr`
is `TryLock`'s session and its key, and the ladder above them is untouched. **Added** —
`MigrateOptions`, `Unlock`, `ErrHeld` and the options door itself, because a caller that
wants a budget *and* a declaration had to reach the door named after the least of its
arguments, "held by another process" had only a false in a triple whose third slot is a
failure, and a bare `bool` decided whether a delete was a hiding or an ending. **Made
reusable** — one options struct every migration door takes and a named `Unlock` any lock
caller can hand to a deferred path. The old/new pair case beside each door is the shape
this branch leaves for the next renaming of one: a description to copy, not something a
module can import.

# A re-runnable statement and the object it left

A migration file that runs its statement outside a transaction is required to be
re-runnable, and the rule is read as "the statement may be sent twice". That is
true of `CREATE INDEX CONCURRENTLY … IF NOT EXISTS` right up to the moment the
first send is cancelled — by `lock_timeout`, which the run's own budget puts on the
session before that statement runs, or by a client that gave up. PostgreSQL keeps
the half-built index and marks it invalid; the second send then answers "relation
already exists, skipping" and returns success. Certifying that answer writes a
history row for a constraint the database does not enforce, and the first write
that relies on it — an `ON CONFLICT` clause naming the index — answers 42P10
against an installation whose own ledger says the constraint holds.

So the object the statement names is read back from the catalog before the file is
recorded. A name that resolves to nothing is a statement that reported creating
something and did not; a name that resolves to an invalid index is the cancelled
build. Both get the same answer, once: the half-built object is dropped the way a
concurrent build is dropped, and the file's statement runs again. What still is not
valid afterwards refuses the file rather than recording it, because the alternative
is a boot that reports success and a table whose writes fail from then on. A
refused file records nothing, so the next boot finds it pending and meets the same
object with the same repair.

A file that builds several concurrent indexes in one statement needs each of them
behind `IF NOT EXISTS`, which is what "re-runnable" asks of it anyway: the repair
re-runs the whole statement, so a second name that is already valid has to answer
"skipping" rather than "already exists".

The drop is sent re-askably, because being picked out of another session's cycle
costs that statement nothing: cancelled or not, the object is gone afterwards, and
`DROP INDEX CONCURRENTLY IF EXISTS` says so twice. The rebuild is not, and that is
the one asymmetry in the pair. A concurrent build the deadlock detector cancels
does not leave nothing behind — it leaves the half-built, invalid index, which is
what this check exists to notice — so re-sending the build on its own meets the
`IF NOT EXISTS` the file has to carry and answers "relation already exists,
skipping": the statement doing nothing and reporting that it applied, which is the
answer the certification refuses to record. Being picked out of a cycle therefore
costs the repair a whole cycle, drop first, and not a re-send.

# Which role may bypass row-level security

`Open` refuses an application role that holds `SUPERUSER` or `BYPASSRLS`, and it
asks the second question too: can this role *become* a role that does? BYPASSRLS is
not inherited, so a clean reading of `current_user` says nothing about `SET ROLE`,
which needs no privilege beyond the membership one `GRANT` hands out. Inside a
transaction that statement moves the attribute along with the name: the GUC the
policies read stays set, `FORCE ROW LEVEL SECURITY` stays in place, and the rows of
every tenant answer. `pg_has_role` walks the membership graph to any depth, so the
chain that decides this is not one hop long and is not visible in the role's own
row. It reads membership rather than the `SET` option, which is the strict answer:
a grant written `SET false` today is one `ALTER` away from true, and an application
role should not be adjacent to a bypassing role either way.

What the query still does not ask is whether the role can `CREATE ROLE`, which on
some versions lets it write a role like the one it is not allowed to join. That is a
reason to keep the application role's grants short, not a check this query could
answer.

`pg_has_role` takes the candidate by OID, not by name. The list of candidates comes
from the statement's snapshot of `pg_roles`, but the name form resolves the role
again in the live catalog, so a role dropped in between made `Open` fail with
`role "..." does not exist` for a role it was never going to be. The OID form
answers false for a role that no longer exists, which is the truth: nothing can
become a role that is gone.

# The tenant-scope walk

`TenantTablesSQL` is the SELECT that lists every ordinary table a migration could
have created, in every schema a module owns, for a caller to assert is scoped to a
tenant or declared exempt. It is exported because the claim is one claim and two
repositories make it: the migrations' RLS test and the catalog's own application
test ran byte-identical copies of it, and both were pinned to `current_schema()`,
which was the only schema until a module's SQL could own one. A table in a module's
schema is a table that check never saw, and the way a walk goes wrong is by quietly
returning fewer rows, not by failing.

Six columns, in this order:

1. schema name — `n.nspname`. A table is only identifiable as `cart.items`, because
   under one schema per owner two modules may each have an `items`.
2. table name — `c.relname`. Report the two together, never the bare name: "items
   has no row-level security" is not a message anybody can act on when four schemas
   have an `items`.
3. RLS enabled — `c.relrowsecurity`, from `ENABLE ROW LEVEL SECURITY`.
4. RLS forced — `c.relforcerowsecurity`, from `FORCE`. `ENABLE` alone exempts the
   table's owner from its own policy, so a caller has to check both.
5. table comment — `obj_description`, `""` when there is none: this is where the
   exemption marker lives, and a walk that did not return it could not tell
   deliberate from forgotten.
6. policy expressions — the table's policies' `USING` expressions, from
   `pg_get_expr(polqual)`, joined with spaces, `""` when the table has none. A
   caller matches this against `platformkit_tenant_match(tenant_id)`.

Which schemas: `current_schema()`, which is `public` in an application and the
test's own schema under this package, and every schema whose name is an owner in
`schema_migrations` — the ledger is what makes a schema a module's, so a schema
created by raw SQL, or by a hand-written ledger row, is not one. That is also why
the walk belongs to a caller running on the same URL as the DDL: `schema_migrations`
resolves through the caller's `search_path`, and `pg_get_expr` deparses a policy's
function call qualified or bare depending on whether that function's schema is on
the path.

Which URL: the admin URL this package returns — the first one `URLs` and `URLsFor`
hand back, the owner of the schema — and not the application URL beside it. The
runner revokes the application role's rights on `schema_migrations`
(`kit/db/migrate.go`, `migrationLedger`), because a role that could forge history
could forge a migration; so the walk on that URL is refused for one table rather
than quietly listing fewer schemas, which is the loud answer and the only
acceptable one.
