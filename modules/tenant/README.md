# Tenant module

`modules/tenant` is the control plane: which customers exist and which host
belongs to which one. The kernel resolves every request's host through its
service before the request is about anything, and `kit/jobs` walks the tenants
it lists, so `Module` returns the service to `main` beside the manifest.
`tenant:manage` is an operator permission guarding
`/api/v1/ops/tenant/tenants` — the control plane, so it is served at the
installation's host only — and the switcher at `/app/tenant/tenants`; `tenant.Bootstrap` creates the
first tenant inside `platformkit bootstrap`'s transaction.

Compose it first, with `tenant.Deps{OnCreate, Invite}`: `OnCreate` hooks —
`auth.SeedRoles` today — run inside the creating transaction, and `Invite`
gives a tenant its first administrator (without one the route is not mounted).
It imports no other module; the modules above reach it through
`contracts.Hook`. Consumers import [contracts/](contracts/) and its
[fake](contracts/tenanttest/), never `internal/`. See
[ADR 0006](../../docs/adr/0006-system-access-is-a-token.md) for the system token.

## Authorization

### Permissions

The manifest in `modules/tenant/module.go` (`permissions`) declares one key: `tenant:manage` (constant `PermissionTenantManage` in `modules/tenant/contracts/permissions.go`). It guards every route in the module and the "Tenants" nav entry for the screen `tenant/tenants`. `RegisterRoutes` in `modules/tenant/internal/handler.go` mounts six routes with it: `list`, `create`, `read`, `suspend`, `add-host` and `invite`. The `invite` route is mounted only when `Deps.Invite` is set.

### Object scope

None. `git grep` finds no `tenancy.Policy` use in `modules/tenant`. The routes act on tenants, not on tenant-owned objects, and they run in a `db.System` transaction (`system` in `RegisterRoutes`), not a tenant transaction.

### Duties the module enforces itself

- `Bootstrap` in `modules/tenant/internal/handler.go` refuses to run when any tenant exists (`crud.ErrConflict`). It is the only writer of the operator flag: it sets `in.Operator = true`.
- `NewTenant.Operator` is `json:"-"` (`modules/tenant/contracts/tenant.go`), so no request body can mark a tenant as the operator. `Service.Create` in `modules/tenant/internal/service.go` copies it from the input.
- `invite` takes no password and no roles from the caller. The roles are chosen by the `Inviter`.

The code shows no ownership or separation-of-duties rule beyond these.

### Public faces

None. No route uses `httpx.Public()`. `Service.ByHost` is host resolution for the kernel (`httpx.TenantLoader`), not a route. The module uses no `kit/limit`.

### The operator boundary

`tenant:manage` is declared with `Operator: true` in `modules/tenant/module.go`. All six routes declare `httpx.OperatorPermission(contracts.PermissionTenantManage)`. The comment on `path` in `handler.go` explains the effect. The control plane is served on every tenant's host. The kernel refuses the request at any tenant other than the operator's own, before it reads the roles table, and the wildcard does not satisfy the grant even there. The module has no `OperatorRead` or `OperatorWrite` route, because it uses `httpx.OperatorPermission` directly.

### Provisioning

The operator tenant is the first tenant, created by `Bootstrap`. Only a role in that tenant that lists `tenant:manage` explicitly can use the control plane. Tenants created through `create` get their roles from the `OnCreate` hooks in `Deps` (see `Deps.OnCreate` in `modules/tenant/module.go`; the README names `auth.SeedRoles`). The `invite` route gives a new tenant its first administrator. The code does not show a fixed persona for the operator role. Grant it with the auth module's roles API.
