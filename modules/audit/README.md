# Audit module

`modules/audit` is the audit trail: it records every event the composed
modules emit and keeps the records for a configured period. The manifest
declares `module.SubscribeAll`, so a module that emits an event is audited by
having emitted it, wherever it sits in the composition. `audit:read` guards
`/api/v1/audit/events` and the admin entry at `/app/audit/events`; the
retention job removes expired rows tenant by tenant.

Compose it in [apps/platformkit/modules.go](../../apps/platformkit/modules.go)
with `audit.Deps{Tenants, RetentionDays}`; `config.example.yaml`'s
`audit.retention_days` supplies the period, zero means a year, and `Feature`
optionally puts the trail behind a plan feature. Consumers import
[contracts/](contracts/) for the record, events and permission, never
`internal/`. `make test TEST_PACKAGES=./modules/audit/...` needs the
development database.

## Authorization

### Permissions

The manifest in `modules/audit/module.go` declares one permission, `audit:read` (`contracts.PermissionAuditRead` in `modules/audit/contracts/permissions.go`). It guards both API routes in `RegisterRoutes` (`modules/audit/internal/handler.go`): `audit-event-list` at `GET /api/v1/audit/events` and `audit-event-read` at `GET /api/v1/audit/events/{id}`. It also guards the nav entry `audit/events` in `modules/audit/module.go`. When `Deps.Feature` is set, both routes also need that plan feature (`read.Needing(feature)`). The event handler `Service.Record` (subscribed with `SubscribeAll`) runs inside the emitting transaction and checks no permission.

### Object scope

None. The code searched shows no use of `tenancy.Policy`, `Check` or `Resource` in this module. Rows are scoped to the request's tenant by the transaction (`Record` writes `db.TenantOf(tx).ID`; `Service.List` reads through the tenant transaction). The filters `name`, `actor`, `record`, `since` and `until` narrow a result and are not access checks.

### Duties the module enforces itself

The trail is append-only in the sense the code makes true: the module mounts only
two read routes and no create, update or delete route (see the comment on
`RegisterRoutes`), and `Service.Record` in `modules/audit/internal/service.go`
inserts with `ON CONFLICT (tenant_id, event_id) DO NOTHING`, so a redelivered event
does not write twice. There are no ownership or separation-of-duties refusals. The
only deletion is the retention job (`Retention` in
`modules/audit/internal/retention.go`), which runs per tenant.

Say what that does not include. The trail is not hash-chained and the role the
application connects as still holds `UPDATE` and `TRUNCATE` on `audit_events`
through the cluster's default privileges, so "append-only" is a statement about
this module's code, not a property the database will refuse a rewrite of. The
chain that would make it one — a per-tenant `seq` with a `prev_hash`/`hash` pair, a
named advisory transaction lock over the append, a checkpoint row per retention
batch, both grants revoked and a `BEFORE DELETE` trigger that admits only the
retention transaction — is owed by decision 0013 and is not delivered here;
`modules/audit/contracts/audit.go` carries the same sentence for the next author.

What every row does answer now is who (`actor`), what (`name`, `payload`,
`records`), when (`occurred_at`), from where (`client_ip`), and which call
(`request_id`, `traceparent`) — migrations 000031 to 000033, carried in
`migrations/000030` and `kit/request`. Nothing is backfilled: a request id, an
address and a trace that were never captured cannot be reconstructed, and inventing
them for old rows would be writing history a second time.

### Public faces

None. No route uses `httpx.Public()`, and there are no public writes, so `kit/limit` is not used.

### The operator boundary

None. `audit:read` is not marked `Operator: true`, and the module uses no `OperatorPermission`, `OperatorRead` or `OperatorWrite`. An administrator holding the `*` wildcard in a tenant can read that tenant's trail only.

### Provisioning

The expected holders are tenant administrators and any role an administrator names `audit:read` in. The built-in `admin` role has the `*` wildcard (`SeedRoles` in `modules/auth/internal/seed.go`), which covers `audit:read`. A composition grants it to another role through the roles API (`PUT /api/v1/auth/roles/{name}`, guarded by `role:manage`) or through default roles passed to `auth.SeedRoles`. A role in a client's `client.yaml` is not shown by the code read for this section.
