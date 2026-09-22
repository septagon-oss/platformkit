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
statement a duration bound would kill.

**Contention is not failure.** A file stopped by that budget returns
`db.ErrContended` naming the file and both budgets; nothing it had not already
applied was applied, the files already applied stay applied, and the operator may
run it again. The runner does not retry inside itself, because it holds the
composition's advisory lock while it waits, and a queue inside that lock stops every
other replica's boot behind it.

**Two tables, not one.** `schema_migrations` says what is applied forever;
`schema_migration_backfill` says where an unfinished drain restarts, holding the
last key it committed, and its row is deleted in the transaction that writes the
history row. Both are revoked from the application role: a ledger an application can
edit is a release that never happened, and a progress row it can edit is a backfill
that repeats rows.

**The drain, and who runs it.** A `phase=data` file's body is wrapped over a window
of its table's primary key and run once per window, one committed transaction per
window. An installation with no history for that owner drains it during migration,
bounded at fifty batches, because nothing is reading and the rows are the ones the
installation just wrote. An owner that already has history is a table under readers:
`Migrate` stops there, applies nothing further for that owner and returns nil, and
`jobs.BackfillMigrations` — composed into the worker by `kit/app` as
`schema-backfill` — is what finishes it through `db.Backfill`. A boot that refused a
drain would stop the only role that can finish it.
