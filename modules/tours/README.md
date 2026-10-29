# Tours

This optional kernel module records tour runs against the tenant transaction. It uses `kit/tour` declarations and the current policy provider; applications compose it with `pkit.NewApp(...).Use(tours.Module)` after supplying the required clock and policy contracts.

## Composition

**Reused** — `kit/tour` declarations, `pkit` typed composition, `db.Tx[db.Tenant]`, `crud.GetForUpdate`, `tenancy.Policy`, and the transactional event outbox.

**Added** — persisted tour run state and revision-checked transitions, because the declaration contract has no runtime lifecycle or tenant-owned run storage.

**Made reusable** — a tenant-scoped run service and the common transition decision function; the automatic offer, event-observation, and HTTP journey remain unfinished.

## Limits

This delivery does not yet implement the complete tour runtime contract: persisted edition state, automatic frequency reservations and expiry, offers, operation outcome observation, idempotency, or HTTP endpoints. Do not compose this module as a complete tour player until those pieces and their acceptance suite are delivered.
