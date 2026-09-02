# 3. Tenancy is enforced by Postgres

Status: accepted, 2026-09-02

## Context

The previous codebase had three tenancy mechanisms: a Go query predicate applied
by convention, per-module row-level security on 22 of 54 modules, and nothing at
all on the rest. Only 4 modules used `FORCE ROW LEVEL SECURITY`, and the app
connected as a superuser, which bypasses row-level security entirely. The
control was on paper; a forgotten `WHERE tenant_id = ?` leaked another tenant.

## Decision

Tenant isolation is one mechanism: every tenant-scoped table has
`ENABLE ROW LEVEL SECURITY` and `FORCE ROW LEVEL SECURITY`, with a policy
matching `tenant_id` against a per-transaction setting. The application connects
as `platformkit_app`, a `NOSUPERUSER NOBYPASSRLS` role, so the policies bind it.
Migrations connect as the owner, which holds the DDL rights.

The setting is placed by the transaction wrapper, not by callers. `db.Tx[Tenant]`
and `db.Tx[System]` are distinct phantom-typed handles: repositories accept only
`Tx[Tenant]`, and `Tx[System]` is obtainable only through an unexported kernel
capability. Using a repository outside a tenant transaction is a compile error.

## What this does and does not claim

The claim is precise, because the imprecise version of it is false.

**The database enforces the boundary against a forgotten predicate.** A query
with no `WHERE tenant_id` returns this tenant's rows and no one else's, whoever
wrote it, in Go or in SQL, today or after a refactor. That is the failure this
decision exists to eliminate, and it is eliminated.

**The type makes accidental cross-tenant access impossible and deliberate
access greppable.** `Tx[System]` cannot be produced outside `kit/`, so no module
can ask for one; a repository that takes `Tx[Tenant]` cannot be handed anything
else.

**The database does not enforce the boundary against code that rewrites the
settings.** `platformkit.tenant_id` and `platformkit.system_access` are
placeholder GUCs, and Postgres classes placeholders `USERSET`: any role may set
them, and there is no privilege to withhold. One
`set_config('platformkit.system_access', 'true', true)` inside a tenant
transaction turns the policy off for the rest of it. So the escape is closed by
two things that are not privileges, and softened by a third:

1. the type parameter, so it cannot happen by accident;
2. `scripts/check_gucs.sh`, which fails the build when any `.go` file outside
   `kit/db` writes either setting, so it cannot happen quietly. **This is the
   control.** A deliberate escape has to be written, and this is what stops it
   being written;
3. a re-read in `db.Run` and `db.RunSystem`: before committing, each re-reads
   both settings and rolls back if either differs from what it placed.

The third one is a backstop and its limit is exact: it catches the escape that
sets a setting and leaves it set, which is the escape that happens by mistake.
It does not catch an escape that restores the value before the transaction
ends — that transaction re-reads clean and commits, with whatever it read or
wrote across tenants. No re-read can catch that, because the state it inspects
is the state the escape restored. `TestARestoringEscapeIsNotCaughtByTheReread`
is that gap, written down and asserted, so that nobody has to discover it.

A tenant table also needs `FORCE`, not only `ENABLE`: `ENABLE` exempts the
table's owner from its own policy, and the application role owns any table it
creates itself.

## Consequences

- A missing `WHERE tenant_id` clause returns no rows instead of another tenant's.
- Cross-tenant work (billing rollups, admin reports) must ask the kernel for
  `Tx[System]` explicitly, which makes every such site greppable.
- Every transaction pays one extra round trip, the settings re-read before
  commit.
- Tests must run against a real Postgres as the app role; a superuser test
  proves nothing.
- Every tenant table pays a policy and an index on `tenant_id`.
- The three helper functions are `PARALLEL SAFE` and set no `search_path`: a
  policy predicate that is not parallel safe makes every query on the table
  unparallelisable, and a `SET` clause would block the inlining a per-row
  predicate depends on.

## Evidence

The runtime half, as `platformkit_app` against a real Postgres:

```sh
go test ./kit/db -run 'TestTenantIsolationIsEnforcedByPostgres|TestOpenRefusesSuperuser'
go test ./kit/db -run 'TestForceRowLevelSecurityIsWhatBindsTheOwner'
go test ./kit/db -run 'TestATransactionThatRewritesItsOwnSettingsIsRolledBack'
go test ./kit/db -run 'TestARestoringEscapeIsNotCaughtByTheReread'
./scripts/check_gucs.sh
```

The compile-time half is `kit/db/scope_compile_test.go`: five lines showing that
a `Tx[System]` cannot be passed where a `Tx[Tenant]` is expected. It carries
`//go:build never`, so it documents the guarantee without being part of any
build; removing the tag is how you watch it fail.
