# Tenant module

`modules/tenant` is the control plane: which customers exist and which host
belongs to which one. The kernel resolves every request's host through its
service before the request is about anything, and `kit/jobs` walks the tenants
it lists, so `Module` returns the service to `main` beside the manifest.
`tenant:manage` is an operator permission guarding
`/api/v1/ops/tenant/tenants` — the control plane, so it is served at the
installation's host only — and the switcher at `/app/tenant/tenants`; `tenant.Bootstrap` creates the
first tenant inside `platformkit bootstrap`'s transaction.

Compose it first, with `tenant.Deps{OnCreate, Invite, Languages}` — `Languages`
being the ones the installation's catalogues answer in, which are the most a tenant
is ever served in. A new tenant is served in the one language its copy is written
in, the column's default, and so is a tenant whose row predates
`migrations/000029_tenant_locale.up.sql`: that file writes no rows, the read takes the
default out of the set, and the column alone answers for a tenant with none beside it,
because a tenant's set is a declaration and a create carries none. `POST
/api/v1/ops/tenant/tenants/{id}/locale` is what declares
more of them, and it refuses a language this installation has no copy for — a tenant
served in a language nobody wrote is a page that declares it and shows the source
copy (`tenant.locale_set` says so, and the host cache is invalidated for that
tenant because the languages of a page changed): `OnCreate` hooks —
`auth.SeedRoles` today — run inside the creating transaction, and `Invite`
gives a tenant its first administrator (without one the route is not mounted).
It imports no other module; the modules above reach it through
`contracts.Hook`. Consumers import [contracts/](contracts/) and its
[fake](contracts/tenanttest/), never `internal/`. See
[ADR 0006](../../docs/adr/0006-system-access-is-a-token.md) for the system token.

## Authorization

### Permissions

The manifest in `modules/tenant/module.go` (`permissions`) declares one key: `tenant:manage` (constant `PermissionTenantManage` in `modules/tenant/contracts/permissions.go`). It guards every route in the module and the "Tenants" nav entry for the screen `tenant/tenants`. `RegisterRoutes` in `modules/tenant/internal/handler.go` mounts eleven routes with it: `list`, `create`, `read`, `suspend`, `reactivate`, `rename`, `set-locale`, `add-host`, `remove-host`, `delete` and `invite`. The `invite` route is mounted only when `Deps.Invite` is set. Each of the eight lifecycle verbs — the four that move a fact (`rename`, `remove-host`, `reactivate`, `delete`) beside the four the module already had — carries this one permission, so no new grant was introduced with them, and each declares the events it publishes so `kit/app` refuses at boot a route that would publish something the manifest does not own.

### Object scope

None. `git grep` finds no `tenancy.Policy` use in `modules/tenant`. The routes act on tenants, not on tenant-owned objects, and they run in a `db.System` transaction (`system` in `RegisterRoutes`), not a tenant transaction.

### Duties the module enforces itself

- `Bootstrap` in `modules/tenant/internal/handler.go` refuses to run when any tenant exists (`crud.ErrConflict`). It is the only writer of the operator flag: it sets `in.Operator = true`.
- `NewTenant.Operator` is `json:"-"` (`modules/tenant/contracts/tenant.go`), so no request body can mark a tenant as the operator. `Service.Create` in `modules/tenant/internal/service.go` copies it from the input.
- `invite` takes no password and no roles from the caller. The roles are chosen by the `Inviter`.

- `RemoveHost` refuses a tenant's primary host and its last one, naming the verb that
  would lift each refusal; `Suspend` and `Delete` refuse the installation's own tenant,
  which is the tenant every one of these routes is reached through.
- `Delete` requires the tenant's slug repeated in `confirm`.
- Every lifecycle verb publishes its event in the subject tenant's scope and
  `tenant.lifecycle_recorded` in the operator tenant's, in the transaction that wrote
  the column. An installation with no operator tenant gets
  `contracts.ErrNoOperatorTenant`, the route answers 503, and nothing is written: a
  lifecycle change auditable from one side is a change nobody can account for.
- Every command reads its row `FOR UPDATE` before it compares it, because `kit/db`
  sets no isolation level and two writes that both read `active` would both publish.

The code shows no ownership or separation-of-duties rule beyond these.

### Public faces

None. No route uses `httpx.Public()`. `Service.ByHost` is host resolution for the kernel (`httpx.TenantLoader`), not a route. The module uses no `kit/limit`.

### The operator boundary

`tenant:manage` is declared with `Operator: true` in `modules/tenant/module.go`. All six routes declare `httpx.OperatorPermission(contracts.PermissionTenantManage)`. The comment on `path` in `handler.go` explains the effect. The control plane is served on every tenant's host. The kernel refuses the request at any tenant other than the operator's own, before it reads the roles table, and the wildcard does not satisfy the grant even there. The module has no `OperatorRead` or `OperatorWrite` route, because it uses `httpx.OperatorPermission` directly.

### Provisioning

The operator tenant is the first tenant, created by `Bootstrap`. Only a role in that tenant that lists `tenant:manage` explicitly can use the control plane. Tenants created through `create` get their roles from the `OnCreate` hooks in `Deps` (see `Deps.OnCreate` in `modules/tenant/module.go`; the README names `auth.SeedRoles`). The `invite` route gives a new tenant its first administrator. The code does not show a fixed persona for the operator role. Grant it with the auth module's roles API.

## Built on what came before

Decision 0022 asks a delivery to name what it composed rather than what it
rebuilt. **Reused:** the four new verbs are `Suspend`'s command shape — read the row,
compare, `Select(...).Updates` of the columns it changed, `events.PublishFor` inside
the writing transaction, read-back — and `crud.GetForUpdate`'s lock, restated here as
one `FOR UPDATE` because that helper is typed for a tenant's own rows and a
control-plane command reads through a `db.Tx[db.System]` whose policy would hide every
other tenant from it; `tenants.deleted_at`, the partial slug index of
`migrations/000006` and the four readers already filtering on it are the delete this
delivery finally writes, so no state, column or index is new; and the operator-side
audit row is `modules/audit`'s existing `SubscribeAll` subscription reached through the
tenant an outbox row *names* — the trail's schema, its `Record` and its routes are
untouched apart from the `traceparent` column this adds. **Added:** `Rename`,
`Reactivate`, `RemoveHost` and `Delete` with their routes, payloads and cases;
`contracts.ErrNoOperatorTenant`, because "no installation to audit into" is none of
`kit/crud`'s three answers; and `audit_events.traceparent`, which is the join the trail
could not make before. Nothing existing could carry the operator's half of the audit:
`events.Publish` takes the tenant from the transaction, and this transaction belongs to
no tenant by design. **Made reusable:** `Installed()` in `tenanttest`, and the same step
in the SQL fixture — stand up the installation's own tenant, then empty the outbox, so a
case asserts what *the case* published — which is the fixture any control-plane suite
that audits both sides needs; `Fixture.PublishedScopes` and `PublishedTraces`, which
turn "which trail does this row land in, and which request does it name" into
assertions; and the pair *an event in the subject's scope plus one in the
installation's, or neither*, which is the shape the next control-plane verb copies
instead of inventing a second audit table.

