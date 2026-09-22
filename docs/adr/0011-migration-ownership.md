# 0011: Capabilities own migration progress

Status: accepted for the clean rebuild; supersedes the global ledger in 0009.

## Problem

A composed client reached migration 2003. Adding public migration 23, or enabling
catalog migration 1001, returned success while silently skipping the new SQL.
One scalar version cannot describe independently evolving capabilities.

## Decision

`db.Migrate` accepts ordered `MigrationSource` values: a stable owner and its
filesystem. `kit/app` supplies the foundation first, then module manifests in
composition order. Module names already identify ownership; no registry is added.
The public foundation remained one source in that first change; the amendment
below moves each reference module's SQL to the module.

One `schema_migrations` table records `(owner, version)`, filename, SHA-256 and
application time for each committed file. There are no global version ranges.
All selected histories are validated before pending SQL runs. Changed or missing
applied files, duplicate identities and insertions before applied versions fail.
Omitted owners retain their data and history for later re-enablement.

A database advisory lock serializes the whole composition. Each file executes
inside a transaction with its history insert. Failure or cancellation rolls both
back; retry reads committed history and resumes. Earlier successful files remain
committed. The application role receives no privileges on the history table.

The old runner, flattened filesystem and down files are removed. Per-owner copies
of the previous engine would fix numbering, but would retain separate dirty-state
bookkeeping and add integrity tracking beside it. Transactional PostgreSQL SQL
and one applied history make failure recovery explicit in the same implementation.

## SQL and release contract

Files are `<positive-version>_<name>.up.sql`, ordered numerically within an owner.
They contain transactional PostgreSQL SQL. They must not manage transactions or
run nontransactional operations such as `CREATE INDEX CONCURRENTLY` — a ban that is
now *mode-scoped*: a file that declares `-- pkit: autocommit=true` is the file
shape whose one nontransactional statement is the whole file, and the rule table
below refuses such a statement anywhere else. Review SQL
for that contract; the runner is not a SQL parser or a sandbox for untrusted SQL.
Cross-capability schema dependencies follow the application's composition order.

This is a fresh baseline. Nothing converts the old `(version, dirty)` ledger or
preserves old installations. Provision a clean database when adopting the rebuild;
the application does not drop databases automatically. Once this baseline is
used, keep applied files unchanged and append corrections as new revisions.

For later breaking schema changes, stop old processes before migration. A rolling
release needs an explicitly tested schema that both running versions can use.
An older artifact missing applied migrations fails startup. Downgrading an image
is not a schema rollback; use a qualified forward repair or restore procedure.

## Amendment: modules own their own SQL, and adopt the history they had

The foundation shipped all twenty-four files under the owner `platformkit`,
including the tables that belong to `task`, `user`, `auth`, `audit`,
`notification`, `billing`, `content`, `site` and `file`. Enabling a module did
not bring its schema, disabling one did not leave its schema behind, and a
module could not add a table without editing the foundation.

Each of those modules now embeds its own `migrations/` directory and exports a
`MigrationSource`; its manifest sets `Migrations`. `migrations/` keeps the
kernel's own tables: the tenancy functions, tenants and hosts, the outbox,
handled claims, dead letters, and the limits ledger. Version numbers did not
change, so the gaps a module took with it are visible in both places.

An existing installation's ledger still says `platformkit` applied those files.
`MigrationSource.Adopts` names the previous owner and the versions to take
over: the runner re-owns those rows by `(owner, version, checksum)` in one
transaction, under the same advisory lock and before any history is read. A row
that is absent was never applied or was adopted already; a row present with a
different checksum is a changed applied file and refuses with the ledger
untouched, exactly as an unchanged owner's would. The adopted SQL never runs
again, so no `CREATE TABLE` fails and no seeded `INSERT` doubles.

Validation happens before the runner connects: every adopted version must be a
file of the adopting source, an owner may not adopt from itself, and no
selected source may still list a version another source adopts. One owner per
version is the property the ledger's primary key already assumed.

A module that never shared an owner declares nothing. A downstream module that
was always its own owner is unaffected. A composition that omits a module still
leaves that module's tables and history in place, which is now true of the
reference modules as well.

## Amendment: a file says what kind of migration it is

A migration that rewrites a table, and a migration that backfills ten million rows,
were the same kind of file: one transaction, no bound on either, and no way for the
runner to tell the dangerous one from the ordinary one. The rule the industry calls
expand/contract was a habit nobody could enforce.

A file may now carry a header — a run of `-- pkit: key=value` comment lines at the
very top, before any SQL — declaring `phase=expand|contract|data`, and the keys that
phase needs. The checksum still covers the whole file including its header, so an
applied file can never be marked: marking it would mean changing bytes some
installation already applied, which the paragraph above already refuses.

Three execution modes follow from the header, and nothing is inferred from SQL: an
expand or contract file runs transactionally as always; an `autocommit` file runs its
one statement with no transaction and its history row commits alone, which is why its
statement must be re-runnable; a `phase=data` file's body never runs as written — it
is wrapped over one window of its table's primary key at a time, one committed
transaction per window, and the key and its type are read from the catalog so the
header cannot claim a key the table does not have. Progress lives in a second table,
`schema_migration_backfill`, holding the last key committed: every reader of
`schema_migrations` assumes a row there means applied forever, and an unfinished
drain is exactly the state that must not look like one. The row and the history row
are never both present. The application role is revoked from both.

An autocommit statement is also the one statement a migration run must not hold the
composition's advisory lock across. Such a statement waits for the transactions already
in the database — that is what `CONCURRENTLY` is for, and no `lock_timeout` bounds the
wait — while that lock is what every other replica's boot queues behind. Holding both
was measured, and it deadlocks the queue:

```
Process 30201 waits for ShareLock on virtual transaction 10/17350; blocked by process 30222.
Process 30222 waits for ExclusiveLock on advisory lock [16384,0,7240101,1]; blocked by process 30206.
```

so the run puts the lock down for that one statement and takes it back for the history
row, which is inserted `ON CONFLICT DO NOTHING`: two replicas may then both reach a
statement the rule table already requires to be re-runnable, and the one that loses the
race learns the file applied rather than failing its boot for a benign race. A statement
that failed writes no row either way.

The order of a release is now a rule rather than a review comment. A `contract` file
refuses while the `expand=` version it names is not already in that installation's
history — one release of separation is the whole of what the ledger can state; how
long ago is a release calendar, which belongs to the product. Nothing of an owner
applies past a data file that has not finished draining; the installation with no
history drains its own, bounded, and the worker drains a table with readers, because
a boot that refused one would stop the only role that can finish it.

Alongside it, the static rules the runner refuses before connecting — the rewrites,
the plain index build, the dropped column outside a contract file — each named, each
with a remedy, and the correctable ones exceptable by `-- pkit: allow=<rule>
reason=<one sentence>`, the reason being the whole content of an exception and
`unused-allow` refusing one that was not needed. Guards apply from a version the
source states, because a rule cannot be refused on a file already applied somewhere:
the bytes are immutable and the only remedy left would be to stop the installation.
[migrations/README.md](../../migrations/README.md) is the canonical table of keys and
rules; this ADR stops short of duplicating it.

Two things the runner cannot decide are given doors instead of opinions. The first is
the wait: a contended migration is not a failed one, and the retry belongs to the
operator because the runner holds the composition's advisory lock — so
`platformkit migrate` exists, which is every role's boot migration over the same
composition, sources, floors and budgets (`app.Migrate` is that composition once),
without a server attached, and `--drain` for the backfill the migration left to the
worker. The second is size: a `lock_timeout` bounds a wait and the rule table refuses
a shape of statement, and neither says what this release will cost against the table
this installation actually has. So a release is rehearsed rather than reviewed.
`scripts/rehearse_migrations.sh` (`make rehearse`) builds the previous release's own
binary from its revision, lets that release migrate a fresh database, seeds ten
thousand rows per table into a copy of it, applies this tree's pending files against
that copy while sampling `pg_stat_activity` for lock waits, and exits 0, 1, 2 or 3 so
a pipeline can tell "applied inside budget" from "failed" from "could not run" from
"over budget". A contended file in a rehearsal is a finding, never a pass. What it
cannot measure is stated in the script: the wait behind a table a running application
holds, and any lock wait shorter than the 100 ms sample.

## Evidence

`kit/db/migrate_test.go` covers late module installation, upstream advancement,
retained data, disable/re-enable, immutable history, numeric order, concurrent
startup, failure/retry, cancellation and ledger permissions. `kit/app` exercises
the same source collection through a real application boot.

For the amendment: `kit/db/migrate_test.go` applies a single-owner layout, then
the split layout, and reads back the adopted row under its new owner with the
adopted table's data intact, through a repeat run, a further migration by the
new owner and a fresh installation; a changed adopted file refuses and leaves
the ledger as it was, and the pre-connection refusals cover a missing file,
self-adoption and a version the previous owner still lists.
`apps/platformkit/app_test.go` runs the upgrade a person would: it migrates
every file under the old single owner, boots the current release over it, and
checks that all twenty-four rows keep their `applied_at`, that each reads under
the owner that now ships it, and that the upgraded installation serves.
`migrations/rls_test.go` walks the kernel's schema and every reference module's
together, so the row-level-security claim still covers every table this
repository creates.

For the header, the rules and the drain: `kit/db/migrate_expand_contract_test.go`
states the budgets and the contended report against a real database, the eighteen
grammar refusals, one case per rule beside the `allow=` that excepts it, the contract
half refusing and then applying, the batched drain proved from `xmin`, and the files
behind an unfinished drain waiting for the worker.
`apps/platformkit/migrate_test.go` shows `platformkit migrate` applying exactly the
sources the composition selected — every owner and every one of its files — and the
second run applying nothing further.
`bash scripts/check_architecture_test.sh` holds the rehearsal's own refusals, which
land before it touches a database; the step itself needs `psql`, an owner connection
and a previous revision, and was run against a PostgreSQL 16 with the fixture's ten
thousand rows per table: one file applied, its duration the runner's own, and the
same step exiting 3 when `--max-file-seconds 0` put every file over budget.
