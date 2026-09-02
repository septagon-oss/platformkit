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
capability. Using a repository outside a tenant transaction is a compile error;
escaping the tenant at runtime requires the database to be wrong.

## Consequences

- A missing `WHERE tenant_id` clause returns no rows instead of another tenant's.
- Cross-tenant work (billing rollups, admin reports) must ask the kernel for
  `Tx[System]` explicitly, which makes every such site greppable.
- Tests must run against a real Postgres as the app role; a superuser test
  proves nothing.
- Every tenant table pays a policy and an index on `tenant_id`.

## Evidence

```sh
go test ./kit/db -run TestTenantIsolationBlocksCrossTenantReads   # E1
```
