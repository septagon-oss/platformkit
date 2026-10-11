# Billing module

`modules/billing` is what a tenant pays for: plans are a collection the
installation's operator maintains (a `rest.Spec` at `/api/v1/billing/plans`, read
with `billing:read` and written with `billing:catalog`, which is operator-only),
and the subscription is a tenant's singleton at `/api/v1/billing/subscription`,
read with `billing:read` and changed through its command routes (`POST /api/v1/billing/subscription/<verb>`) with `billing:manage`.
Renewal takes money from somebody else's machine, so the job never runs inside
a database transaction; [module.go](module.go) makes that argument and
[ADR 0008](../../docs/adr/0008-prices-are-the-operators.md) says who sets prices.

Compose it with `billing.Deps{Tenants, Payments, RenewEvery}`, where `Payments`
is your `contracts.PaymentProvider`. Consumers import [contracts/](contracts/)
and its [fake](contracts/billingtest/), never `internal/`.
`make test TEST_PACKAGES=./modules/billing/...` needs the development database.

## Authorization

### Permissions

The manifest in `modules/billing/module.go` (`permissions`) declares three keys, defined in `modules/billing/contracts/permissions.go`. `billing:read` guards the plan list and read routes (`Read` in the plan `rest.Spec`), the subscription read (`rest.Singleton` in `Module`) and the nav entry `billing/plans`. `billing:manage` guards `billing-subscription-subscribe` and `billing-subscription-cancel` (`command` in `modules/billing/internal/handler.go`). `billing:catalog` guards plan create, update and delete (`Write` in the plan spec, with `OperatorWrite: true`). The intro above describes `billing:catalog` as reading the plan list; the code shows it guards plan writes, and reads use `billing:read`. The renewal job `Renew` checks no permission.

### Object scope

None. The code searched shows no use of `tenancy.Policy`, `Check` or `Resource` in this module. The subscription is one row per tenant, reached through the tenant transaction. The plan catalogue is shared: `modules/billing/module.go` says the plans table is readable by every tenant and writable only from the operator's transaction.

### Duties the module enforces itself

`Service.Subscribe` in `modules/billing/internal/service.go` refuses a plan that does not accept new subscriptions and refuses a change while the subscription owes for a period. `Service.Cancel` refuses a subscription that has already ended, and refuses an immediate cancel while a period is owed. `RefuseWhileSubscribed` is the plan delete hook: it counts live subscriptions across all tenants under a system token and refuses to delete a plan still being billed. There is no ownership rule beyond tenant scoping.

### Public faces

None. The module registers no route with `httpx.Public()`, so it has no public writes and does not use `kit/limit`.

### The operator boundary

`billing:catalog` is declared `Operator: true` in `modules/billing/module.go`. The plan spec sets `OperatorWrite: true`, so create, update and delete use `httpx.OperatorPermission`, which the kernel refuses outside the operator's tenant before the roles table is asked. The `*` wildcard does not satisfy it (`contracts.Grants` in `modules/auth/contracts/auth.go`). Plan reads are not operator-only.

### Provisioning

Tenant administrators hold `billing:read` and `billing:manage` through the `*` wildcard on the built-in `admin` role (`SeedRoles` in `modules/auth/internal/seed.go`). `billing:catalog` must be named explicitly: `seedRoles` in `apps/platformkit/roles.go` passes it as an operator permission, so `SeedRoles` adds it to the `admin` role only in the operator tenant. Other roles get any of the three through `PUT /api/v1/auth/roles/{name}`, which refuses `billing:catalog` outside the operator tenant (`CheckedPermissions`). A role in a client's `client.yaml` is not shown by the code read for this section.
