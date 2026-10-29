# Tours

This optional kernel module records tour runs against the tenant transaction. It uses `kit/tour` declarations and the current policy provider; applications compose it with `pkit.NewApp(...).Use(tours.Module)` after supplying the required clock and policy contracts.

## Composition

**Reused** — `kit/tour` declarations, `pkit` typed composition, `db.Tx[db.Tenant]`, `crud.GetForUpdate`, `tenancy.Policy`, and the transactional event outbox.

**Added** — persisted tour run state, revision-checked transitions, tenant-authenticated offers/run/action routes, and durable automatic reservation rows, because the declaration contract has no runtime lifecycle or tenant-owned run storage.

**Made reusable** — a tenant-scoped run service and common transition decision function for this module's SQL command path.

## Limits

This delivery does not yet implement the complete tour runtime contract: persisted edition decisions, automatic daily frequency enforcement, eligible offer selection, reservation consumption/expiry, operation outcome observation, idempotency, or content-bound playback responses. The four HTTP paths are registered, but the offers path currently returns an empty list. Do not compose this module as a complete tour player until those pieces and their acceptance suite are delivered.
