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

# Migrations

`Migrate(ctx, url, sources...)` applies each capability's files in owner and version
order, one file and its history row per transaction. What a file may say about
itself, and the rules it is refused by, are the schema author's reading:
[migrations/README.md](../../migrations/README.md). This section is what the runner
itself guarantees.

**Budgets.** Every file runs with `lock_timeout` five seconds and no
`statement_timeout`, re-asserted on the runner's session before each file so a file
that sets one for itself cannot leak it to the next; `database.lock_timeout` and
`database.statement_timeout` change them, and `MigrateWith` takes the same values as
a `MigrationBudget`. The budgets are the runner's patience, not the operator's: a
migration that cannot take a lock in five seconds of *waiting* stops. The statement
budget defaults to no bound because a legitimate index build on a large table is the
statement a duration bound would kill. The same two values go on the session before
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

Waiting for the composition lock itself is a different wait, and it is left patient:
`pg_advisory_lock` runs before the budgets go on the session, so it waits on the
caller's context alone. The replica that gets the lock second has the first one's
applied files to read and finds nothing pending, so a boot that waits and applies
nothing beats one that refuses at five seconds and is read as a failed deploy. When
that context runs out the operator gets a context deadline, not `ErrContended`: nothing
was refused, the run did not finish.

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
that repeats rows.

**The drain, and who runs it.** A `phase=data` file's body is wrapped over a window
of its table's primary key and run once per window, one committed transaction per
window — unless the file said its body bounds itself, in which case it runs once, in
one transaction. The key may be of any single-column primary key type: the cursor
travels as the key cast to text and comes back as a comparison against that type's own
name, read from the catalogue, so the drain asks PostgreSQL for the ordering instead of
keeping a list of the types it is willing to name. What it refuses is a table it cannot
window over at all — one keyed by two columns, or a `table=` no selected owner creates —
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
can finish it. Neither answer to a drain in flight
is a failed boot: a run that resumes one and reaches its own fifty-batch bound returns
`ErrBackfillBudget` with the committed batches and the cursor standing, and `kit/app`
logs that as the work its tick still has rather than refusing the start — the same
failure one bound down. `platformkit migrate` and `Bootstrap` asked for a run that
finishes, so those doors keep the error.
A refusal of a data file leaves nothing resumable: the progress row means "this drain
started, resume it", so a shape the window cannot run is refused before that row exists.
The guard cannot make that refusal instead of the executor: a guard refuses a whole owner
before any of it runs, and the wrongness here is the window's own — the owner's earlier
file has applied, and the version behind it is one statement too many. How many statements
that is comes from the cut PostgreSQL makes, not from the reading the rule table reads a
dollar body inside: a `;` inside a value the body is writing leaves the body one statement,
and the window wraps it whole. A rule that over-reads a construct can be answered with
`allow=` and a sentence; this refusal, `data-with-ddl` and `autocommit-not-rerunnable` — the
three that read a statement split and state no exception — can be answered with nothing, and
a refusal an author cannot answer may not rest on an approximation.

**Refusals below the rule table.** Four refusals are not judgements about a file's text but
facts about this database — a table that is not here, a key the window cannot walk, an
expansion the ledger has not seen, a drain past its bound — so no `allow=` reaches any of
them, and they are named as the rules are, with the id in front of the sentence that
explains it (`refusal <id>: …`) because a log line is what an operator greps from:

| id | who refuses | what the run says |
| --- | --- | --- |
| `data-table-missing` | the drain, before a progress row exists | the `table=` a data file names is not in this database — a file of another owner that was not selected, or a release that has not applied yet |
| `data-key-not-primary-key` | the drain, before a progress row exists | the table has no single-column primary key to window over; a table keyed otherwise needs a drain its owner owns, in a job |
| `contract-without-expansion` | the plan, after the ledger is read and before any file of that owner runs | the contract half waits for an `expand=` version this installation has not applied, and nothing of the owner applied |
| `backfill-exceeds-install-budget` | the inline drain, as `db.ErrBackfillBudget` | the bound a migration gives itself was reached; the committed batches and the cursor stand, and the worker's tick finishes the table |

`kit/db/refusal_names_test.go` refuses one of each and reads the id back off the message, and
reads this table besides: an id named here that nothing prints, or a printed id nothing
names, fails that case rather than drifting. A contended file is a report and not a refusal
— it is `db.ErrContended`, and the paragraph above states what it promises.

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
