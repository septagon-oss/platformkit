# Tenant module

`modules/tenant` is the control plane: which customers exist and which host
belongs to which one. The kernel resolves every request's host through its
service before the request is about anything, and `kit/jobs` walks the tenants
it lists, so `Module` returns the service to `main` beside the manifest.
`tenant:manage` is an operator permission guarding `/api/v1/tenant/tenants`
and the switcher at `/admin/tenant/tenants`; `tenant.Bootstrap` creates the
first tenant inside `platformkit bootstrap`'s transaction.

Compose it first, with `tenant.Deps{OnCreate, Invite}`: `OnCreate` hooks —
`auth.SeedRoles` today — run inside the creating transaction, and `Invite`
gives a tenant its first administrator (without one the route is not mounted).
It imports no other module; the modules above reach it through
`contracts.Hook`. Consumers import [contracts/](contracts/) and its
[fake](contracts/tenanttest/), never `internal/`. See
[ADR 0006](../../docs/adr/0006-system-access-is-a-token.md) for the system token.
