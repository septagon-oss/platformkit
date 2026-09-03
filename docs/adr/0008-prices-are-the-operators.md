# 8. Prices are the operator's

Status: accepted, 2026-09-03

## Context

`modules/billing` shipped its plan catalogue as an ordinary tenant-owned table:
`billing_plans` carried a `tenant_id`, was scoped by row-level security like
every other table, and was written under `billing:manage` — the same permission
that subscribes and cancels.

Every tenant's own administrator holds that permission, because the admin role
a tenant is created with carries the wildcard. So the E5 review did this, from a
subscription that was `past_due`:

1. `POST /api/v1/billing/plans` with `{"code":"free","priceCents":0}`.
2. `POST /api/v1/billing/subscription/subscribe` naming it.

The plan change swapped the plan and left the period alone, as documented, and
the next renewal charged nothing. The debt vanished, and nothing in the system
had been asked a question it could refuse.

That is not a bug in the subscription lifecycle. It is a missing seam: a price
list is a thing the installation sells, and the customer is the party being
sold to. The two had one permission and one table because a module that owns one
entity per concern is the house style, and the concern here is two.

## Decision

**The catalogue is the operator's. There is one of it, every tenant reads it,
and only the operator writes it.**

Concretely:

- `billing:catalog` is a new permission, declared `Operator: true` in the
  manifest, and the plan `rest.Spec` declares `OperatorWrite: true` so create,
  update and delete are mounted with `httpx.OperatorPermission`. The kernel
  refuses those routes at any tenant but the operator's own *before* it asks
  the roles table anything, and no wildcard satisfies an operator grant
  (ADR 0006's sibling argument, and `modules/tenant`'s five routes).
- `billing:manage` keeps what a customer legitimately does to its own
  subscription: subscribe, and cancel.
- `billing:read` is unchanged, because every tenant has to read the price list
  it is choosing from.
- `migrations/000016` gives `billing_plans` the policy `USING (true) WITH CHECK
  (platformkit_tenant_match(tenant_id))`. Read by all; written only by a
  transaction scoped to the tenant a row names, which — given the routes above —
  is the operator's. The unique index on `code` is global rather than
  per-tenant, because there is one catalogue and two rows with one code would be
  two prices for one plan.
- A plan change is refused while the subscription is `past_due`. What is owed is
  owed for a period already served; it is settled, or the grace period runs out.
- The price a subscription is billed at is stamped on the subscription, so a
  catalogue the customer does not control is also a catalogue whose edits are
  not retroactive.

## The alternative we rejected

**Per-tenant copies, seeded by the operator.** The table stays tenant-scoped,
the operator publishes a catalogue, and each tenant gets its own rows.

It keeps one property we like — every query in the module stays a tenant query —
and costs four things we do not want:

- a fan-out on every plan change, and a reconciliation for the tenants it did
  not reach;
- an answer to "what happens to the copy a tenant edited", which is either "the
  operator's edit is lost" or "the tenant's is", and both are surprising;
- a `plan_id` that means a different row in every tenant, so a report across
  tenants has to join on `code` anyway;
- and a customer that can still write its own rows, because the table is still
  tenant-writable — which is the hole we started from, moved rather than closed.

One shared table has none of those. What it costs is one exception to "every
table is tenant-scoped", and the exception is written into the migration, into
`modules/billing/module.go` and into this file, because an exception nobody
wrote down is the one somebody copies.

## Consequences

- `RefuseWhileSubscribed`, the hook that stops the operator deleting a plan
  people are still on, counts under system access: the catalogue is shared, so
  the operator's own transaction would have seen only the operator's
  subscriptions and reported nothing about anybody else's.
- `rest.Spec` grows `OperatorWrite`. The alternative was `modules/billing`
  hand-writing five routes to change one declaration, which is what
  `modules/tenant` had to do and which the E5 review named as a kernel gap.
- `httpx.Resource` grows the same flag, because the generated admin screen calls
  the resource's closures directly and those carry their own authorization: a
  screen guarded by the bare permission would have let a customer's wildcard
  write a plan through the form after the API refused it.
- A deployment with one tenant that *is* the operator sees no difference.
