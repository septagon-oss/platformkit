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
run nontransactional operations such as `CREATE INDEX CONCURRENTLY`. Review SQL
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
