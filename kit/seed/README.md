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
resource owner's write path supply transaction scope, source parsing, graph order,
canonical comparison and domain validation. A page is created, patched and deleted
through `rest.Spec`'s write core — the body its JSON route runs, exported so that a
seed cannot grow a second spelling of it — and its lifecycle moves through
`content.Service.Publish`. A person is created through the user module's own
invitation. `auth.Service.Permissions` is what the grant check reads.

**Added:** `seed_keys` under database RLS and an explicit `Writer` service were
needed because no existing table records seed provenance and no existing kit
operation coordinates several owner writes in one selected run. `app.RunCommand`
opens the cross-tenant transaction an operator command needs: the system token is
internal to the kernel, so a composition cannot mint one for itself.

**Made reusable:** `Writer`, `Authorizer`, `Plan` and `Apply` let another app
declare its own resources and embedded files without copying the loader or the
reconciliation logic. The reference application composes them in
`apps/platformkit/seed.go` — a page writer, a site writer and a person writer — and runs them with
`platformkit seed --tenant <slug> --as <email> [--demo] [--dry-run]`, which
`make seed` wraps.

## Limits

The demo refusal reads `tenants.demo` under the run's own transaction, not the
`tenancy.Tenant` value on the context, so a caller that builds its own tenant value
cannot bring demo records to a tenant whose row says false. A prune whose owner row
is already gone costs its mapping and the run continues: the write that finds none
is never refused, and a file with `prune: true` does not fail every later run once
somebody has deleted the row through the product.

A command's seed run carries a person. `seedGrants` refuses one that carries
nobody, and the command resolves `--as` to a user of the target tenant inside that
tenant's own transaction before any grant is asked; the roles it checks are the
rows the tenant holds, not a credential a caller asserts.

A tenant's own creation is the one run with nobody to ask, and it arrives through
`Service.ApplyProvisioned` rather than through a hole in `Apply`. Its proof is
state, checked in the authoritative transaction, not a token: the tenant holds no
`seed_keys` row at all, and every record the selected files declare is still
absent, so the run can only create. The reference composition adds the third
condition its own hook can check about a table the seed does not own — the tenant
holds no person — and composes that hook second in the literal `OnCreate` list,
after `auth.SeedRoles`, because the people a demo seed invites hold the roles those
lines just created. What `ApplyProvisioned` can therefore write is the records
every tenant of the application is created with, through the same owners and the
same events; anything else names a person.

The seed publishes nothing of its own. Every write goes through its owner, so the
owner publishes the event; what the seed adds is the attribution carried beside it.
`Apply` and `ApplyProvisioned` put `events.WithAttribution` around each owner write,
and the outbox row (migrations/000037) and the CloudEvents envelope the relay
publishes record `actor_kind=seed`, the record's own `source_file` and `source_line`
and the `initiator` the run named — with `actor` left NULL, because no session wrote
a seeded row and a command line is not a login. `Plan.String()` names `file:line`
for every record it decided. T-0190 translations have no owner path in this checkout, so `i18n` is
reported as skipped. A caller must not claim a successful apply before its
enclosing transaction commits.
