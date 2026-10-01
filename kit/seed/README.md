# Embedded application seeds

`kit/seed` loads embedded YAML or JSON documents, orders declared references,
and compares the requested values with owner snapshots. An application supplies
an explicit `Writer` for each resource; that writer calls its owner's normal
create, update, command and delete paths. `Service.Plan` is read-only;
`Service.Apply` operates within a caller-owned `db.Tx[db.Tenant]` and uses a
per-tenant advisory transaction lock. An error must be returned to the caller
so the enclosing transaction rolls back.

## Composition

**Reused:** `db.Tx[db.Tenant]`, `db.InTenant`, `Load`, `Order`, `Decide`, and each
resource owner's write path supply transaction scope, source parsing, graph
order, canonical comparison and domain validation.

**Added:** `seed_keys` under database RLS and an explicit `Writer` service were
needed because no existing table records seed provenance and no existing kit
operation coordinates several owner writes in one selected run.

**Made reusable:** `Writer`, `Authorizer`, `Plan` and `Apply` let another app
declare its own resources and embedded files without copying the loader or
reconciliation logic.

## Limits

This package currently has no composed application writer, tenant creation
hook, operator command or event attribution. `Plan` and `Apply` are usable only
after those adapters are supplied; the accepted intended contract and examples
are in [docs/seed.md](../../docs/seed.md). A caller must not claim a successful
apply before its enclosing transaction commits. T-0190 translations have no
owner path in this checkout; `i18n` is reported as skipped.
