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
Its one cross-module import is `modules/user/contracts`, for `invite`'s `Inviter`; the modules above reach it through
`contracts.Hook`. Consumers import [contracts/](contracts/) and its
[fake](contracts/tenanttest/), never `internal/`. See
[ADR 0006](../../docs/adr/0006-system-access-is-a-token.md) for the system token.

## Authorization

### Permissions

The manifest in `modules/tenant/module.go` (`permissions`) declares one key: `tenant:manage` (constant `PermissionTenantManage` in `modules/tenant/contracts/permissions.go`). It guards every route in the module and the "Tenants" nav entry for the screen `tenant/tenants`. `RegisterRoutes` in `modules/tenant/internal/handler.go` mounts thirteen routes with it: `list`, `create`, `read`, `suspend`, `reactivate`, `rename`, `set-locale`, `add-host`, `remove-host`, `delete`, `set-oidc`, `clear-oidc` and `invite`. The `invite` route is mounted only when `Deps.Invite` is set. Every verb the module has carries this one permission, so no new grant was introduced with the four that arrived here: the seven that move a lifecycle (`create`, `add-host`, `suspend`, `rename`, `reactivate`, `remove-host`, `delete`) and `set-locale`, which is a command but not a lifecycle change — it moves what a tenant is served in, not whether it is served, so it publishes `tenant.locale_set` and no operator mirror. The two provider routes beside it are commands in the same shape, and mirror nothing either: which provider a tenant's people sign in against moves neither whether nor in what language the tenant is served. Each declares the events it publishes so `kit/app` refuses at boot a route that would publish something the manifest does not own. The lifecycle has one verb this module does not answer: `export`, and with it `restore`. Neither is here — they are the pair that has to agree on a format for a customer's rows leaving and coming back, and until it lands a deleted tenant stays deleted, `Reactivate` answers it not-found, and `Delete` is the closest thing to a goodbye. `tenanttest` has no fake of a format either: the [fake](contracts/tenanttest/) retires a tenant the way the service does and no further.

### Object scope

None. `git grep` finds no `tenancy.Policy` use in `modules/tenant`. The routes act on tenants, not on tenant-owned objects, and they run in a `db.System` transaction (`system` in `RegisterRoutes`), not a tenant transaction.

### Duties the module enforces itself

- `Bootstrap` in `modules/tenant/internal/handler.go` refuses to run when any tenant exists (`crud.ErrConflict`). It is the only writer of the operator flag: it sets `in.Operator = true`.
- `NewTenant.Operator` is `json:"-"` (`modules/tenant/contracts/tenant.go`), so no request body can mark a tenant as the operator. `Service.Create` in `modules/tenant/internal/service.go` copies it from the input.
- `invite` takes no password and no roles from the caller. The roles are chosen by the `Inviter`.
- `SetOIDC` refuses half a provider: an issuer that is not a URL, is plain HTTP for a host that is not local, or carries a query; a client id that is empty; a secret reference that is not an environment variable's name; a registration mode outside `disabled | existing | provision`; and `provision` with no roles, which would be a door into an empty room. `migrations/000030` puts the same rule on the row as a CHECK, so a write that reaches the table by another route is refused there, and `modules/tenant/internal/oidc_test.go` is the case that tries it. Writing the same provider again changes nothing and publishes nothing.
- `Demo` is written by `Service.Create` and by nothing else: `modules/tenant/internal/handler.go` mounts no route that patches it, and the column (`migrations/000046_tenant_demo.up.sql`) defaults to `false` for every tenant that existed before it. The application's seed reads it to decide whether demo records are allowed for a tenant ([`docs/seed.md`](../../docs/seed.md)); the tenant module itself never branches on it.

- `RemoveHost` refuses a tenant's primary host and its last one, naming the verb that
  would lift each refusal; `Suspend` and `Delete` refuse the installation's own tenant,
  which is the tenant every one of these routes is reached through.
- `Delete` requires the tenant's slug repeated in `confirm`, and releases the two
  names the platform routes on — the slug, and the hosts the tenant answered at,
  whose rows it removes, because `tenant_hosts.host` is a global key and a customer
  who is not served cannot reserve a hostname from the next one. `tenant.deleted`
  names the released hosts, which is where the pairing is kept once the routing
  table stops recording it; everything else the tenant owns stays.
- Every lifecycle verb publishes its event in the subject tenant's scope and
  `tenant.lifecycle_recorded` in the operator tenant of the app that is writing, in
  the transaction that wrote
  the column, and asks that audit question before it writes: a verb that can audit
  neither side writes neither. A promotion is one of those writes — `add-host` is the
  only way to choose a tenant's primary host, and moving it publishes
  `tenant.host_added` the way the arrival does, because which name a tenant's links
  are built on is a fact two trails have to be able to date. Asking for the host that
  is already primary, or for a host already here without the promotion, changes no
  column and says nothing. An app with no operator tenant of its own gets
  `contracts.ErrNoOperatorTenant`, the route answers 503, and nothing is written: a
  lifecycle change auditable from one side is a change nobody can account for, and
  another app's installation is not this app's to audit into.
- Every command reads its row `FOR UPDATE` before it compares it, because `kit/db`
  sets no isolation level and two writes that both read `active` would both publish.

The code shows no ownership or separation-of-duties rule beyond these.

### Public faces

None. No route uses `httpx.Public()`. `Service.ByHost` is host resolution for the kernel (`httpx.TenantLoader`), not a route. The module uses no `kit/limit`.

### The operator boundary

`tenant:manage` is declared with `Operator: true` in `modules/tenant/module.go`. All thirteen routes declare `httpx.OperatorPermission(contracts.PermissionTenantManage)`, the four lifecycle verbs this delivery adds among them: the grant is one because the surface is one, and a route that needs a second key would be a different surface. The comment on `path` in `handler.go` explains the effect. The control plane is served on every tenant's host. The kernel refuses the request at any tenant other than the operator's own, before it reads the roles table, and the wildcard does not satisfy the grant even there. One app's control plane holds one app's tenants: every read and write below is scoped to `tenants.app`, the operator it audits into included (see `kit/appname/README.md`). The module has no `OperatorRead` or `OperatorWrite` route, because it uses `httpx.OperatorPermission` directly.

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
untouched — `audit_events.traceparent` is `modules/audit`'s own `000035_audit_context`,
and what this delivery adds is the pair of rows that carry it. **Added:** `Rename`,
`Reactivate`, `RemoveHost` and `Delete` with their routes, payloads and cases;
`contracts.ErrNoOperatorTenant`, because "no installation to audit into" is none of
`kit/crud`'s three answers; and `record` writing both halves with the one `traceparent`
the request carried, the join the trail could not make while only one of the two rows
existed. Nothing existing could carry the operator's half of the audit:
`events.Publish` takes the tenant from the transaction, and this transaction belongs to
no tenant by design. **Made reusable:** `Installed()` in `tenanttest`, and the same step
in the SQL fixture — stand up the installation's own tenant, then empty the outbox, so a
case asserts what *the case* published — which is the fixture any control-plane suite
that audits both sides needs; `Fixture.PublishedScopes` and `PublishedTraces`, which
turn "which trail does this row land in, and which request does it name" into
assertions; and the pair *an event in the subject's scope plus one in the
installation's, or neither*, which is the shape the next control-plane verb copies
instead of inventing a second audit table.

The languages the installation answers in arrived the same way. **Reused:** `SetLocale` is `Suspend`'s command shape — `Get`, compare,
`Select(...).Updates` of the two columns it changed, `events.PublishFor` inside the
writing transaction, read-back — and a language is refused with `crud.ErrInvalid`
like every other bad input here; `tenant_locales` is `tenant_hosts`' table shape and
policy, so the new fact inherits the row-level scope rather than declaring one; the
set travels on the host resolution `ByHost` already performs, because three owners ask
"same tenant as before?" with `==` and a slice would answer that wrong. **Added:** `default_locale`, `tenant_locales`,
`SetLocale`/`ValidLocale`/`EventLocaleSet`, the route, and `Deps.Languages` — the
installation's own languages, which had no owner because the catalogue decided alone.
**Made reusable:** a control-plane write that rechecks its own rules, publishes one
event, and invalidates the cache whose truth it moved, which is the shape a second
`/tenants` command copies — the whole shape, including what it does when the shared
store will not take that invalidation: the write stands, and the route answers 503
rather than reporting an effect it did not achieve. And `tenant.locale_set`, which is
what a second process will subscribe to when a TTL stops being good enough.

The provider per tenant is the same delivery again. **Reused:** `SetOIDC` is
`SetLocale`'s command shape and `SetOIDC`'s read, `OIDCOf`, is a tenant
transaction reading its own row under the policy `000006` already puts over
`tenants` — no new policy is created for it, which is the check that the fact is
the tenant's and not a new privilege. **Added:** the six `oidc_*` columns, the
two CHECKs that make half a provider unstatable rather than a 500, `SetOIDC` /
`ClearOIDC` / `OIDCOf`, the `set-oidc` and `clear-oidc` routes and
`tenant.oidc_set` / `tenant.oidc_cleared`. **Made reusable:** a secret that is
never in a row — the column holds an environment variable's *name*, because
`modules/audit` copies every payload it is handed — and a control-plane fact a
lower module reads through its own port, so `modules/auth` resolves an issuer per
request without importing this module.
