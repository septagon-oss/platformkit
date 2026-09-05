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
The public foundation remains one source in this change.

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

## Evidence

`kit/db/migrate_test.go` covers late module installation, upstream advancement,
retained data, disable/re-enable, immutable history, numeric order, concurrent
startup, failure/retry, cancellation and ledger permissions. `kit/app` exercises
the same source collection through a real application boot.
