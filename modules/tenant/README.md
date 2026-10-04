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

## The tenant's own languages

`tenant:locale_manage` (constant `PermissionLocaleManage` in
`modules/tenant/contracts/permissions.go`) lets a tenant's administrator say which
languages that one tenant is answered in. It is a tenant permission, not an operator
one: `Deps.LocaleManager` answers the question inside the write, and the reference
composition wires it over the same authorizer the route guard consulted
(`tenantLocaleManager` in `apps/platformkit/locale.go`), so a link and a door cannot
disagree about who may use it.

`Service.SetOwnLocale` (`modules/tenant/internal/service.go`) takes no tenant id — a
tenant cannot be shown a door it could point at a neighbour — and inside its own
transaction rechecks the actor's grant, locks its own row `FOR UPDATE` under
row-level security, normalises the pair with the operator's own rules, compares the
expected revision, writes `default_locale`, `locale_revision` and `updated_at` in one
statement, replaces the `tenant_locales` rows, and publishes `tenant.locale_set` in
the same transaction. A refusal returns no row, writes nothing and emits nothing; a
declaration that changes nothing publishes nothing, however stale the revision it
came with. `SetLocale`, the operator's door, moves the same counter so the two
writers cannot each believe they are first.

### Interface

- The page is `/app/tenant/languages`, written by `modules/admin` and mounted in the
  tenant's own namespace, offered in the sidebar only to somebody the authorizer
  says holds `tenant:locale_manage`. It offers the languages this installation's
  catalogues carry and no others, and posts the pair with the revision the page was
  drawn at.
- `GET /api/v1/tenant/languages` reads the declaration and the revision it is held
  at; `PUT` to the same address replaces it. Both are the app surface, both are
  guarded by `tenant:locale_manage`, both run on the request's own tenant
  transaction, and no argument of either names a tenant — the request resolved to
  one. They perform `SetOwnLocale`, the same command the page posts to, so the form
  and the JSON door cannot decide one fact differently. `modules/admin` writes the
  screen; this module writes the door, which is what a client that is not a browser
  has to have.
- The same write is discoverable: the module registers the `tenant/languages`
  resource (a singleton — one declaration per tenant, no create, no delete) and
  records the command beside it, so a shell generated from
  `/api/v1/app/resources` offers the door rather than inventing one, and reads its
  `endpoint` instead of guessing an address it cannot derive.
- `tenants.locale_revision` is read: `SetOwnLocale` refuses a declaration read at a
  revision the row has left behind with `crud.ErrConflict`, so the person sees which
  declaration they overwrote rather than losing it.
- `migrations/000031_tenant_locale_self_service.up.sql` splits `tenants_scope` into
  read-your-own-row and change-your-own-languages, grants `tenant_locales` self
  insert/update/delete scoped to this tenant, and adds a `BEFORE UPDATE` trigger for
  the column rule a policy cannot state. PostgreSQL applies `WITH CHECK` to
  inserted and updated rows and never to a `DELETE`, which is why the old policy —
  written `WITH CHECK (system)` with no `USING` clause — was a self-delete grant on
  the row that says whether a customer exists.

### Limits

The screen at `/app/tenant/languages` is written by `modules/admin` and this module
leaves it that way: registering the resource means the generated screens would
collide with it, and the shell mounts the hand-written page instead (its own
resource-screen check). What the resource carries is the API and the catalog entry,
not a second page. The removal
warning the page shows is the truthful generic one — this module keeps no inventory
of what content exists in which language, and a count invented here would be a
number nothing can check. The last language cannot be removed by construction: every
declaration names a default, and an empty default is refused. The page keeps one copy
column: its review step is the heading-and-description block above the ticks it warns
about, not a boxed Alert whose icon would move that sentence 68px away.

### Verification

`apps/platformkit/tenant_languages_command_test.go`
(`TestAnAdministratorSetsTheTenantsLanguagesAndTheTrailHoldsIt`) sets the languages
through the composed service and reads back the row, the `tenant.locale_set` row in
the outbox, and the `audit_events` row with a non-null actor; it then posts the same
form against the revision it moved and shows none of the three changed.
`apps/platformkit/tenant_languages_page_test.go` follows the link a dashboard gives
the administrator and expects a page with controls for each served language.
`modules/tenant/internal/language_api_route_test.go`
(`TestTenantLanguagesHaveAnAppAPICommand`) mounts the manifest against a real
`httpx.API` and refuses a build where the command has no `PUT
/api/v1/tenant/languages` route, or where that route is on the wrong surface, or
where the write is missing from resource command discovery.
`modules/tenant/internal` covers the operator's door and the floor under the last
language, and `own_row_column_rule_test.go` runs the column rule of 000031 at the
application role: `slug`, `name`, `status`, `operator` and `created_at` are refused to
the tenant's own transaction, and `default_locale`, `locale_revision` and `updated_at`
are not. The tenant's own command is decided by the shared suite, not by a case in one
world: `contracts/tenanttest` exercises `SetOwnLocale` — the grant it rechecks, the
revision it compares, the declaration it already holds — through `RunService`, which
both the fake and the SQL service pass, so a fake that drifted from the service on any
of the three is a red suite and not a consumer that tested against the wrong answer.

## Authorization

### Permissions

The manifest in `modules/tenant/module.go` (`permissions`) declares two keys: `tenant:manage` (constant `PermissionTenantManage` in `modules/tenant/contracts/permissions.go`), which guards every route in the module and the "Tenants" nav entry for the screen `tenant/tenants`. `RegisterRoutes` in `modules/tenant/internal/handler.go` mounts six routes with it: `list`, `create`, `read`, `suspend`, `add-host` and `invite`. The `invite` route is mounted only when `Deps.Invite` is set. The second key, `tenant:locale_manage`, guards the tenant's own two routes — `GET` and `PUT /api/v1/tenant/languages`, mounted by `mountOwnLanguages` in `modules/tenant/internal/own_languages.go` on the app surface — and is the permission the resource above declares for both its read and its write. It is also what `SetOwnLocale` asks again inside its own transaction, which is what makes the route guard an early refusal rather than the whole answer, and what the admin page's sidebar link is offered on.

### Object scope

None. `git grep` finds no `tenancy.Policy` use in `modules/tenant`. The routes act on tenants, not on tenant-owned objects, and they run in a `db.System` transaction (`system` in `RegisterRoutes`), not a tenant transaction.

### Duties the module enforces itself

- `Bootstrap` in `modules/tenant/internal/handler.go` refuses to run when any tenant exists (`crud.ErrConflict`). It is the only writer of the operator flag: it sets `in.Operator = true`.
- `NewTenant.Operator` is `json:"-"` (`modules/tenant/contracts/tenant.go`), so no request body can mark a tenant as the operator. `Service.Create` in `modules/tenant/internal/service.go` copies it from the input.
- `invite` takes no password and no roles from the caller. The roles are chosen by the `Inviter`.

The code shows no ownership or separation-of-duties rule beyond these.

### Public faces

None on the control plane. No route uses `httpx.Public()` — the tenant's own two routes are guarded by a permission and by being signed in, not by anonymity. `Service.ByHost` is host resolution for the kernel (`httpx.TenantLoader`), not a route. The module uses no `kit/limit`.

### The operator boundary

`tenant:manage` is declared with `Operator: true` in `modules/tenant/module.go`. All six routes declare `httpx.OperatorPermission(contracts.PermissionTenantManage)`. The comment on `path` in `handler.go` explains the effect. The control plane is served on every tenant's host. The kernel refuses the request at any tenant other than the operator's own, before it reads the roles table, and the wildcard does not satisfy the grant even there. The module has no `OperatorRead` or `OperatorWrite` route, because it uses `httpx.OperatorPermission` directly.

### Provisioning

The operator tenant is the first tenant, created by `Bootstrap`. Only a role in that tenant that lists `tenant:manage` explicitly can use the control plane. Tenants created through `create` get their roles from the `OnCreate` hooks in `Deps` (see `Deps.OnCreate` in `modules/tenant/module.go`; the README names `auth.SeedRoles`). The `invite` route gives a new tenant its first administrator. The code does not show a fixed persona for the operator role. Grant it with the auth module's roles API.

## Built on what came before

Decision 0022 asks a delivery to name what it composed rather than what it
rebuilt. **Reused:** `SetLocale` is `Suspend`'s command shape — `Get`, compare,
`Select(...).Updates` of the two columns it changed, `events.PublishFor` inside the
writing transaction, read-back — and a language is refused with `crud.ErrInvalid`
like every other bad input here; `tenant_locales` is `tenant_hosts`' table shape and
policy, so the new fact inherits the row-level scope rather than declaring one; the
set travels on the host resolution `ByHost` already performs, because three owners ask
"same tenant as before?" with `==` and a slice would answer that wrong. **Added:** `default_locale`, `tenant_locales`,
`SetLocale`/`ValidLocale`/`EventLocaleSet`, the route, and `Deps.Languages` — the
installation's own languages, which had no owner because the catalogue decided alone.
**Made reusable:** a control-plane write that rechecks its own rules, publishes one
event, and invalidates the cache whose truth it moved, which is the shape the next
`/tenants` command copies; and `tenant.locale_set`, which is what a second process
will subscribe to when a TTL stops being good enough.
