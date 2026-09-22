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
history row. Both are revoked from the application role: a ledger an application can
edit is a release that never happened, and a progress row it can edit is a backfill
that repeats rows.

**The drain, and who runs it.** A `phase=data` file's body is wrapped over a window
of its table's primary key and run once per window, one committed transaction per
window — unless the file said its body bounds itself, in which case it runs once, in
one transaction. The same reading of the same text (comments gone, case folded)
decides both halves, so the body the rule table judged is the body the executor runs.
An installation with no history for that owner drains it during migration, bounded at
fifty batches, because nothing is reading and the rows are the ones the installation
just wrote. An owner that already has history is a table under readers:
`Migrate` stops there, applies nothing further for that owner and returns nil, and
`jobs.BackfillMigrations` — composed into the worker by `kit/app` as
`schema-backfill` — is what finishes it through `db.Backfill`. A boot that refused a
drain would stop the only role that can finish it. Neither answer to a drain in flight
is a failed boot: a run that resumes one and reaches its own fifty-batch bound returns
`ErrBackfillBudget` with the committed batches and the cursor standing, and `kit/app`
logs that as the work its tick still has rather than refusing the start — the same
failure one bound down. `platformkit migrate` and `Bootstrap` asked for a run that
finishes, so those doors keep the error.
A refusal of a data file leaves nothing resumable: the progress row means "this drain
started, resume it", so a shape the window cannot run is refused before that row exists.

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
