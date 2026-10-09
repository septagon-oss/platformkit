# Audit module

`modules/audit` is the audit trail: it records every event the composed
modules emit and keeps the records for a configured period. The manifest
declares `module.SubscribeAll`, so a module that emits an event is audited by
having emitted it, wherever it sits in the composition. `audit:read` guards
`/api/v1/audit/events` and the admin entry at `/app/audit/events`; the
retention job removes expired rows tenant by tenant.

Compose it with `Use(audit.Module)`: the provider value in
[provider.go](provider.go) declares its needs and builds this same `New`, so the
module wires itself and `apps/platformkit/app.go` only names it.
`config.example.yaml`'s `audit.retention_days` supplies the period (the build
reads that one section, and `pkit.Server.Explain` prints that it did), zero means
a year, and the trail sits behind a plan feature when a composition provides an
`auditcontracts.Plan` and in front of everybody when it does not — which features
a product sells is never this module's decision. The provider reads
`config.Database` too, for `retain_url`: the one connection the expiry trigger
admits. `audit.Deps{}` stays for a client that hands the four values over by hand.
Consumers import
[contracts/](contracts/) for the record, events and permission, never
`internal/`. `make test TEST_PACKAGES=./modules/audit/...` needs the
development database.

**Reused:** `jobs.TenantLister`, which the tenant module has always handed the
retention sweep, and `pkit.Config`, which reads `config.Audit` the way every other
deployment setting is read. **Added:** `contracts.Plan`, because a bare `string`
need has no type to key a provider by and the feature name is the composition's
decision, not this module's. **Made reusable:** the pattern of naming a product's
own decision as a one-method port in the consuming module's `contracts/`, which
any module that used to take a string from `Deps` can copy.

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

That is now true of the database and not only of this module's code.
`migrations/000048_audit_history_append_only.up.sql` revokes `UPDATE`, `TRUNCATE`,
`REFERENCES` and `TRIGGER` from every grantee the catalog discovers (the shape
`kit/db/migrate.go` uses for the runner's own ledger, because a module may name no
role), and installs four triggers: `audit_events_never_rewritten` refuses every
`UPDATE`, from every role including the table's owner and a superuser;
`audit_events_expire_only_after` admits a `DELETE` only when the role holding it may
delete, may **not** insert — asked of every column as well as of the table, because
`INSERT (col, …)` is a grant an application really holds and `has_table_privilege` alone
answers "no" about a role that appends through one — and the row is past 365 days;
`audit_events_never_expired_through_a_door` refuses a `DELETE` by a role that can append
through a view the deployment built over the trail, which nothing in the trail's own
grants can see, asked once per statement because asked per row it costs a sweep three
orders of magnitude; and `audit_events_never_emptied` refuses `TRUNCATE` outright. The last is the reason the
revoke alone is not the fence. `TRUNCATE` ignores row-level security, so it is the one
write that reaches every tenant's history out of one tenant's transaction, and an
operator's `GRANT ALL … TO <app role>` hands it back in one statement. A `BEFORE
TRUNCATE` trigger fires `FOR EACH STATEMENT` and nothing else — no row trigger can see a
TRUNCATE — and it fires before any row is touched, so the refusal leaves the trail where
it was.

The two halves refuse different things: the revoke is what refuses the application today,
with PostgreSQL's own `42501`, and the triggers are what refuses it after an operator runs
`GRANT ALL … TO <app role>`, and what refuses the owner, which no revoke reaches.

`TestTrailPrivilegesRefuseARewrite` asks `has_table_privilege` what the application
role holds, `TestTrailRefusesEveryWriteTheApplicationCanAttempt` runs the review's own
three statements as that role inside an ordinary tenant transaction, and
`TestTrailRefusesTruncateWhenThePrivilegeComesBack` hands the privilege back with
`GRANT ALL` and asks whether both tenants' rows are still there.

The residual, in one sentence: the application role still *holds* `DELETE` on
`audit_events` — `has_table_privilege` answers true — and cannot use it, because the door
that admits an expiry must be a capability and not a setting the fenced role can write.
`TestExpiryRoleIsTheOnlyDoor` is the behaviour behind that answer,
`TestColumnAppenderCannotExpireHistory` is the same answer for the role whose `INSERT`
arrives by column rather than by table, and
`TestViewAppenderCannotExpireHistory` and `TestViewOverViewAppenderCannotExpireHistory`
are the same answer for the role whose append arrives through a view over the trail.

Retention runs as a third role, `database.retain_url`: the job opens it for the length
of one run, deletes a batch per transaction, and writes one row per batch into
`audit_retention_marks` — tenant, cutoff, count, instant — in the same transaction as
the delete it describes, so the record and the removal commit together. The job lists
the tenants on the connection the scheduler handed it, the application's, because
listing is an ordinary application read and the expiry role is provisioned with grants
on two audit tables and nothing else. `audit_retention_marks` carries its own triggers —
no `UPDATE`, no `DELETE`, no `TRUNCATE`, from any role — because a mark is the evidence a
reader checks the trail's arithmetic against, and evidence that can be edited is not
evidence. It is a record and not an event: the manifest declares none, and an expiry
event would be relayed into the table being trimmed. `kit/db`'s `Open` is what stops that
DSN being a superuser's, because a `BYPASSRLS` sweep would see every tenant's rows inside
the first tenant's transaction; `TestRetentionLeavesAnotherTenantTrailAlone` is the case
that says so, and TestTheTrailExpiresWithTheGrantsTheAuditREADMENames, written in apps/platformkit
where the reference composition lives, runs its hourly job as a role holding exactly
the two grants below.

A row says what moved because the module that emitted it said so. The trail stores
payloads verbatim and invents no before/after it was not given, so `events.Change` is the
shape a payload carries the answer in, `kit/rest`'s CRUD door fills it for every entity it
writes from the row it locked (events.Recorder), and a module that writes its own command
computes its own diff — `modules/site`'s `Save` does, over the stored row read `FOR
UPDATE`. An update event with no `changes` member is a save the trail cannot explain a
year later; TestEveryUpdateEventCarriesWhatChanged, written in apps/platformkit where the
reference composition lives, holds every `*.updated` event it composes to the member.

What every row does answer now is who (`actor`), what (`name`, `payload`,
`records`), when (`occurred_at`), from where (`client_ip`), and which call
(`request_id`, `traceparent`) — migrations 000035 to 000037, carried in
`migrations/000034` and `kit/request`. Nothing is backfilled: a request id, an
address and a trace that were never captured cannot be reconstructed, and inventing
them for old rows would be writing history a second time.

### Public faces

None. No route uses `httpx.Public()`, and there are no public writes, so `kit/limit` is not used.

### The operator boundary

None. `audit:read` is not marked `Operator: true`, and the module uses no `OperatorPermission`, `OperatorRead` or `OperatorWrite`. An administrator holding the `*` wildcard in a tenant can read that tenant's trail only.

### Provisioning

The expiry role is provisioned outside this module, because module SQL may name no
role. An operator creates a login role that is neither a superuser nor `BYPASSRLS`,
gives it `GRANT USAGE` on the namespace, then:

```sql
GRANT SELECT, DELETE ON TABLE audit_events TO <retain role>;
GRANT SELECT, INSERT ON TABLE audit_retention_marks TO <retain role>;
```

and names its DSN as `database.retain_url`. Those two grants are the whole provision:
the job reads the tenant list on the application's own connection, so the expiry role
needs nothing on `tenants`, `tenant_hosts` or `tenant_locales`, and holding nothing else
is what makes it a different role from the application rather than the application
wearing a hat. Without the DSN the trail never expires and
the job says so at its first tick rather than deleting nothing and reporting success;
`audit.retention_days` below 365 is refused at boot, because the trigger cannot read
config and the tempting ways to tell it one are ways for the application to choose its
own date for forgetting.

The expected holders are tenant administrators and any role an administrator names `audit:read` in. The built-in `admin` role has the `*` wildcard (`SeedRoles` in `modules/auth/internal/seed.go`), which covers `audit:read`. A composition grants it to another role through the roles API (`PUT /api/v1/auth/roles/{name}`, guarded by `role:manage`) or through default roles passed to `auth.SeedRoles`. A role in a client's `client.yaml` is not shown by the code read for this section.

## Reuse

**Reused** — `kit/db/migrate.go`'s grantee-discovery sweep (`aclexplode`,
`grantee = 0` → `PUBLIC`, `quote_ident`, `CASCADE`), `000010_audit.up.sql`'s
table/RLS/policy shape, `kit/jobs/backfill.go`'s ignore-the-scheduler's-connection
shape, `jobs.PerTenantConcurrent` and `jobs.TenantLister`, `kit/db`'s `Open`
role check, and the `Service` interface and hand-written `httpx.Register` shape
`internal/handler.go` already carries. **Added** — `000048_audit_history_append_only.up.sql`
(the revoke and the four triggers) and `000049_audit_retention_marks.up.sql`, because
nothing in the repository had ever revoked a privilege from a module table or written
a trigger, and a trigger is the only thing that refuses the table's own owner;
`Deps.RetainURL` and `database.retain_url`, because the expiry door must be a role the
application is not; and `dbtest.Role`, because the two handles a test had — the
superuser owner and the appending application role — cannot stand where a
delete-only role stands. **Made reusable** — `dbtest.Role`, a generic "create the role
this boundary needs and hand me its DSN" door for the next module that fences a table
by capability, and the four triggers plus the mark table as the pattern for any other
append-only table this kernel takes over.

## Limits

Four things this delivery leaves open, named where the reviewer will look.

* **The application role still holds `DELETE`** on `audit_events`, and
  `has_table_privilege` answers true. What it cannot do is use it: the door the trigger
  admits is a *shape* (may delete, may not append — by table, by column, or through a view
  the deployment built over the trail — and only past the floor) precisely so that no
  setting the fenced role can write opens it. What the privilege cannot buy is the delete:
  `TestExpiryRoleIsTheOnlyDoor` is the behaviour, and a role that can `DROP TRIGGER` is the
  DDL role, outside this boundary by definition. The door question walks views to any depth
  and stops there: a `SECURITY DEFINER` function that inserts into the trail and grants
  `EXECUTE` onward is another append door an installation could build, and it is refused by
  never writing one, the way the trail's own append path is.
* **No hash chain.** Decision 0013's per-tenant `seq` with a `prev_hash`/`hash` pair,
  an advisory lock over the append and a checkpoint row are still owed. What is
  delivered proves that nothing inside the application's reach rewrote or expired a
  row, and records every expiry; it does not prove that a row was ever really inserted,
  and a chain bolted onto a table whose writer could `UPDATE` it would have proved that
  anyway, which is why it comes after this boundary rather than with it.
* **Who may expire the trail is the product's.** `database.retain_url` names the role;
  `apps/platformkit/postgres-init.sql`, `deploy/` and the boot's grant are the
  product's files. The kernel's share is the grant shape (above, Provisioning), the
  refusal of a `BYPASSRLS` DSN, and the 365-day floor.
* **A reader cannot yet ask the trail whether it is complete.** The publication door
  (`kit/events/catalog.go` `checkDeclared`) makes an undeclared event impossible to
  lose silently, and `TestReviewPublishedEventsAllReachAudit` compares stamped outbox
  rows to trail rows. A `GET /api/v1/audit/coverage` that answers that question for a
  bounded window, names the missing event ids and reports the last trim is the next
  delivery's; it needs the outbox purge window to be readable by the module that owns
  the trail, and a proof that says nothing outside the evidence it still holds.
