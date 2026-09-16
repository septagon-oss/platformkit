# Billing module

`modules/billing` is what a tenant pays for: plans are a collection an
administrator maintains (a `rest.Spec` at `/api/v1/billing/plans`, guarded by
`billing:read` and `billing:manage`), the subscription is a tenant's singleton
at `/api/v1/billing/subscription`, and `billing:catalog` reads the plan list.
Renewal takes money from somebody else's machine, so the job never runs inside
a database transaction; [module.go](module.go) makes that argument and
[ADR 0008](../../docs/adr/0008-prices-are-the-operators.md) says who sets prices.

Compose it with `billing.Deps{Tenants, Payments, RenewEvery}`, where `Payments`
is your `contracts.PaymentProvider`. Consumers import [contracts/](contracts/)
and its [fake](contracts/billingtest/), never `internal/`.
`make test TEST_PACKAGES=./modules/billing/...` needs the development database.
