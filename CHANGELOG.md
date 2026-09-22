# Changelog

## Unreleased

Two of these entries, one property: a tenant keeps somebody who can administer
it. Each was written because the state was reachable, not because a race was
reported, and each says below what it leaves open rather than leaving that to a
reader who depends on it.

**A migration now says what kind of migration it is, and the runner holds it to
that shape.** A rewrite and a ten-million-row backfill were the same file: one
transaction, no bound, and the expand/contract rule a review comment. A file may
carry a `-- pkit:` header — `phase=expand|contract|data`, with `batch=`, `table=`,
`autocommit=`, and `allow=<rule> reason=…` for the exceptions a reviewer reads — and
the runner then applies it in the mode it declared: transactionally, as one
nontransactional statement that must be re-runnable, or as a backfill wrapped over a
window of its table's primary key, one committed transaction per window, resumable
from the last key it committed in `schema_migration_backfill`. Every file runs with a
five-second `lock_timeout` (configurable, `database.lock_timeout`) and no statement
bound by default, and one stopped by the lock budget returns `db.ErrContended`:
nothing new was applied, and it may be run again. A `contract` file refuses while the
`expand=` version it names has not already applied, and nothing of an owner applies
past a drain that has not finished — the worker's `schema-backfill` job finishes
that, so a boot never waits behind a table it cannot empty. The rules the runner
refuses before connecting, the keys, and the floors each source declares are written
down once in [migrations/README.md](migrations/README.md); the mode-scoped ban on
nontransactional SQL is the amendment to
[ADR 0011](docs/adr/0011-migration-ownership.md).

**The user screen cannot take away a tenant's administration.** Setting the sole
administrator's roles to none, deactivating them and deleting them each answered 2xx,
and each left a tenant where nobody inside it could change a role again: whoever was
left standing was answered 403 by the roles screen and by
`PUT /api/v1/auth/roles/{name}`, because the write they now needed was the one the
tenant could no longer authorise. Those three writes now answer 422 and name the rule.
The rule is `user/contracts.CheckedAdministration`: a tenant keeps at least one
**active** person holding a role that grants `role:manage`. It asks two questions with
two predicates on purpose — `User.Administers` is wide, so removing somebody who has
not accepted their invitation is a write the floor looks at, and `User.CanAdminister`
is narrow, so an unaccepted invitation cannot hold the floor up. The narrow half is
measured rather than theoretical: in this repository's default configuration, which
ships no `mail.host`, inviting an heir as an administrator and standing down answered
200 while the heir's sign-in answered 401. Active rather than "has a password",
because `active` is exactly what `Service.Open` tests before it writes a session, so
the predicate and the thing it predicts agree by construction; counting passwords
instead would refuse writes in a composition that provisioned identity-provider
accounts with no hash, which nothing here creates.

The floor never refuses a grant, so the state stays one grant away from repair; in a
tenant where every holder of an administering role is still unaccepted, every removal
of one of them is refused until somebody active is appointed. `internal.Service.floor`
takes the advisory lock below after the subject's row lock and before its reads, so
two administrators standing down at once cannot both pass. The real service and the
`usertest` fake call the same decision, and the conformance suite holds both to it.

`user.Deps.Administration` is a new **required** field: a composition without it
panics at `user.Module`, as `file.Deps.Storage` does — and so does an adapter
handed to it with nothing inside, because a `&contracts.AdministrationFunc{}` looks
wired and would answer "nobody administers this tenant" to every question the floor
asks. `auth.AdministeringRoles` is what goes inside
`&usercontracts.AdministrationFunc{Ask: …}` — who holds a role is the user module's
table, what a role grants is the
auth module's, and the application joins them rather than either module reading the
other's rows. `Administration` is a one-method interface and the adapter is taken by
pointer, which is what keeps `user.Deps` comparable as it was published at v1.1.0: a
func value reached through an interface satisfies a checker and then panics the first
time two `Deps` are compared.

**The roles screen the navigation always named exists, and it has a floor of its own.**
`/admin/auth/roles` was declared by `modules/auth` and served by nothing, so every
installation logged `admin: a nav entry leads to a path no route serves` at boot and
every operator saw a menu item that answered 404. `modules/admin` now serves the
screen that entry names, guarded by the same `role:manage` the two JSON routes
declare: what each role grants, as ticks over the permissions this tenant may name,
and one button per role that writes the whole list back through `SetRole`. An operator
permission is offered only in the operator's own tenant, because that is what the
write accepts, and a grant the composition no longer offers is named in the form that
would drop it rather than hidden and then destroyed. The list pages at
`ui/resource.PerPage`, as every generated list does. Applications wire
`admin.Deps.Roles`, which stays optional: nil mounts no screen, and a composition
whose navigation names the entry while wiring nothing is told so at boot.

Emptying the last role that grants `role:manage` left nobody able to change a role
again, through the screen or through the route. The rule is
`auth/contracts.CheckedAdministration`, so both doors refuse it and the conformance
suite holds every implementation to it, and `internal.SetRole` takes the advisory lock
before its first read rather than after the read that decides. `ValidRoleName` also
bounds a name at `contracts.MaxRoleName`, the 64 the route and the form already
advertised and nothing underneath them enforced.

**One lock, and what it does not buy.** Both floors take
`"administration/<tenant id>"` through
`pg_advisory_xact_lock(hashtextextended(key, 0))` — the same literal written out in
both modules rather than imported from one, pinned by a test on each side and by a
test across them. That makes the two writes queue behind each other, in both orders,
which is a real defect closed, and it is nothing more: **the two floors do not
compose** into the property they are both for, which is that somebody who can still
sign in holds a role granting `role:manage`. Nothing asks that question. Two sequences
reach a tenant nobody can administer with every individual write permitted and no
concurrency involved — create a role granting `role:manage`, give it to nobody, then
empty the role everybody holds; or, with two administrators holding two such roles,
strip one person's roles and then empty the other's role. The first is
`TestTheTwoFloorsStillDoNotComposeIntoOneInvariant`, which asserts the hole and fails
the day somebody closes it. The symmetric fix is named in
`auth/contracts.CheckedAdministration` and is not in this release: auth would ask the
composed question the way the user module already does, through a narrow capability
the application supplies, so one property is checked once instead of two halves of it
being checked separately.

How bad a lockout is depends on whose tenant it is, and the repair path is narrower
than "there is one". `POST /api/v1/tenant/tenants/{id}/invite` runs in a system
transaction and provisions somebody holding a role the application names — `admin`
here — so an operator can put an administrator into a *customer's* tenant whose people
lost their grants, through a supported route and without SQL. It does not help where
the named role is itself the one that was emptied, since that is the role it hands
out, and it does not help in the operator's own tenant at all: the route declares
`tenant:manage` as an operator permission, held through that tenant's own roles, and
there the control plane shuts and SQL is the only way back. That route is not the only
door of that shape either —
`POST /api/v1/tenant/tenants/{id}/suspend` against the operator's own tenant is a
single request after which every operator host answers as though no site were served,
and `modules/tenant` has no route that reverses it. That predates this work and
neither floor touches it.

The floor sees rows and not people: an active administrator who has forgotten a
password in a tenant with no mail, or whose identity provider is gone, is a lockout
nothing here reports. It is per tenant, so the operator's own tenant gains no extra
protection from it. Nothing here renames or deletes a role, so a held name cannot
vanish today; a module that adds either will need its own floor.

`usertest.NewFake` keeps the signature it published in v1.1.0, and a fake built that
way has no role system, exactly as it did before the floor existed. A test that wants
the floor asks `usertest.NewFakeWithAdministration`, and `TestFakeConforms` runs the
shared suite through it, so the fake and the real service cannot disagree about the
floor quietly. That shape is a release constraint, not a preference: this package is
published, and [RELEASE.md](RELEASE.md#choose-the-compatible-release-line) measures a
break against v1.1.0 with no accepted-break baseline, so changing this signature would
owe the `/v2` migration it describes.

`TestEveryNavEntryLeadsSomewhere` makes the unserved-nav class a gate rather than a
boot warning. Two entries are named as known debt with their reason —
`modules/file`'s `/admin/file/files` and `modules/audit`'s `/admin/audit/events` — and
the gate fails if either becomes served and is left in the list. Serving them needs a
read-only mode in `httpx.Resource` and `ui/screens`, which mount create, edit and
delete unconditionally today.

`make check-race` is called by CI. The goal and the Makefile target have been here
since the concurrency kernel, and CONTRIBUTING has said CI runs it, but no workflow
called it: the gate that would catch a lock released before the commit that needed it
had never run outside a contributor's shell. ci.yml now runs it after `make check`,
and this change widens `RACE_PACKAGES` to `./modules/auth/internal/...` and
`./modules/user/internal/...` beside the defaults, because that is where the advisory
locks above live.

Two direct dependencies moved, and one test helper had to be told why. `dave/dst` goes
to v0.28.0 and `jackc/pgx/v5` to v5.11.0, each on the requirement line Dependabot
proposed; the one graph move beyond them is dst's own `dave/jennifer` 1.5.0 → 1.7.1,
which `go.sum` carries and nothing in `./...` links. Nothing unrelated came with them.
pgx 5.11 rewrote its connection-string
parser to match libpq, which reads `+` as a literal, and `modules/task`'s isolation
DSN was built with `url.Values.Encode`, which writes an encoded space as `+` — so
all 34 `TestConcurrentTaskCommands` subtests refused to connect, asking PostgreSQL
for an isolation level called `repeatable+read`. Percent-encoding the space is
correct on both pgx versions, so that is a repair here rather than something the
bump has to work around. What could reach a consumer is dst's own floor: it moved to
`go 1.26.0`, which this module clears and a pinning module on an older toolchain
would not.

**A command-owned field is refused where a body is read, an empty `PATCH` says
nothing, and a write asks whose row it is.** `refuseImmutable` asked whether the
decoded map held the
declared key while `encoding/json` binds a field a key merely folds onto, so
`{"Author": …}` wrote the author through every generated create route of every
module declaring `Immutable`. The five doors that read a body — create and patch,
the two a page calls beneath HTTP, and `Values` beneath a create form — now ask
it with `strings.EqualFold`, the comparison the decoder itself makes, and refuse
the whole write naming the declared field rather than the spelling it arrived in.
A `PATCH` that named no column moved `updated_at` and published
`<module>.<entity>.updated` for a change nobody made; it now validates nothing,
writes nothing, publishes nothing and returns the row as read. The tenant-scope
recheck a body naming a column triggers still runs first, so an empty `PATCH`
of a row outside the request's tenant is 404 like any other body. The `DELETE`
asked that question of nothing at all: row-level security filters a delete by its
`USING` clause alone, since a delete produces no new row for a `WITH CHECK`
clause to inspect, so on a table every tenant may read, one tenant deleted
another's row — 204, event and all — or answered 500 out of the policy violation
where the Spec soft-deletes. Both doors now ask `crud.RecheckTenant`, the tenant
compare `Update` already made, exported rather than copied: the change's one new
exported name, and a nil entity answers it `ErrInvalid` as every other door in
`kit/crud` does. `db.Now` stamps the three timestamp columns, so an
answer carries the instant the column holds. What stays open: a `Singleton`
declares no command-owned field.
**The address an operation is mounted on now says which surface it belongs to, and a
module no longer writes one.** A module's `Routes` receives `httpx.Surfaces` — the
public face, the workspace and the control plane — and the router composes the address
from the surface and the module: a relative path that repeats either is refused at mount,
so `/api/v1/tasks/tasks` cannot be written again. The workspace keeps its JSON addresses;
what moved is where its documents are (`/admin/<module>/<entity>` is now
`/app/<module>/<entity>`, the shell at `/app/admin/…`), and the anonymous doors of
`auth`, `content`, `file` and `site`, which took `/api/v1/public/…`. Each surface has its
own chain: the public one reads no session, sets no cookie (a handler that mints one gets
`PUBLIC_SETS_A_COOKIE` withheld at the writer at every body size, and a 500 of that
name wherever the response can still be replaced) and is cacheable for sixty seconds;
its anonymous writes are counted by tenant, route and address, so one customer's
office does not exhaust another's budget;
A page whose body its owner can republish asks that cache to revalidate rather than answer
from what it kept, so a publish is on the site the next time the page is loaded;
the workspace is `no-store`, `noindex`, and admits an anonymous caller only at a route
that declares `Public()` — and boot prints those doors rather than keeping a list of them;
the control plane is served at `server.installation_host` and answers as an unmounted
address at every other host — the same status, the same body and the same headers every
other refusal of that host carries, because the host gate runs inside the middleware
that writes them — including to a tenant that is not the installation's, which
is the second half of the tenant-API incident recorded in
[ADR 0015](docs/adr/0015-a-refusal-has-one-value-and-two-shapes.md)'s neighbourhood and
in [`modules/tenant`](modules/tenant/README.md). `modules/tenant`'s routes and
`modules/billing`'s plan catalog therefore moved to `/api/v1/ops/…`.
`GET /api/v1/admin/resources` is now `GET /api/v1/app/resources` and is mounted by
`kit/app`, with the document supplied by the composition
(`app.Options.WorkspaceCatalog` = `screens.Describe`); every catalog entry gained a
`screen` key, a `write_path` key and a command a `path` key — each only where the
derivation from the entry's own `path` is no longer true, so a document that could
always be read the same way still is. The workspace is described as an empty surface
in OpenAPI, stamped `x-platformkit-surface` per operation.

What a caller sees: `/admin`, the admin module's own pages beneath it (`/admin/login`,
`/admin/health`, `/admin/assets`, `/admin/_gallery`), `/api/v1/admin` and the four
public doors answer with a 302 (307 for a write, never a cached 301) for one release,
and are deleted in v1.3.0 — see [aliases.go](kit/httpx/aliases.go).
`/api/v1/tenant/tenants` deliberately has no alias: a control plane does not announce
itself by leaving the old door open at every customer's host. Refusals now name a code
in the JSON `detail` — `AUTH_ANONYMOUS`, `AUTH_DENIED`, `AUTH_NOT_OPERATOR`,
`AUTH_NO_TENANT`, `AUTH_PRINCIPAL_CHANGED`, `CSRF_ORIGIN`, `PUBLIC_SETS_A_COOKIE`,
`LIMIT_EXHAUSTED` (the public surface's own write limit) and
`WRITE_ELSEWHERE` (a write of a resource whose writes are served on another surface,
answered at its read door, naming `write_path`) — where the sentence used to be a
lowercase prefix.

- The kernel answers every refusal it makes for itself in the shape the requester asked
  for: the problem document to a client that wanted a value, the shell's own page — in the
  request's language — to a client that came to be shown one. This includes an address
  nothing is mounted at, an address mounted for other verbs, a CSRF failure and a panic
  that escapes a handler. A guard's refusal — a missing session, a missing grant, a row of
  another tenant, a route the plan excludes, the public surface's write limit — answers the
  same way: it is the same decision, and it is the one a *navigating* person was being
  shown JSON. An htmx write counts as a client that parses a value: its controller
  (`ui/assets/js/htmx-config.js`) reads the refusal's code out of the problem body and
  swaps nothing for a 4xx, so a page answered there would reach nobody.
- Those codes are what a refusal *page* is translated by. `ui/page` holds the one table
from a code to a catalog key (`fault.<CODE>`, and `fault.<status>` for the 404, the 405
and the 500, which carry no code because the kernel wrote the sentence), the page is
negotiated from the request's
`Accept-Language` — a guard answers before a session, a tenant or a stored preference
exists to ask one — and it carries `Content-Language` and `Vary: Accept-Language`. A
translated page keeps the code in front of the sentence (`AUTH_DENIED: Não pode fazer
isto.`), because the code is what a person reads back to support. The
declaration follows the sentence on the page: a shell with no catalog, or none for that
code, says the kernel's English and declares `en`. Two narrower promises came with it:
the pointer to a split resource's write door now goes to a caller holding the
credential that door reads, because the door would refuse anybody else the moment they
reached it; and a tree mounted with `Static` answers a missing file, its own prefix and
any directory beneath it through its surface's chain, in the shape the client asked for,
instead of net/http's plain-text note and a listing of the shell's filenames. The hourly
purge of the rate-limit counters moved from `modules/auth`'s sweep to `kit/app`, beside
the outbox's, because the kernel's public write limit made the kernel the table's second
writer. See
[ADR 0017](docs/adr/0017-three-surfaces-by-path.md) for what this costs a module.

**A module's schema opens to the application role without either of them naming it.**
`apps/platformkit/postgres-init.sql` is where the read role is given `USAGE` on
`public` and its default table and sequence privileges, so a module migration
that created a schema of its own produced a namespace only its owner could open
and every later query answered 42501. The module cannot grant its way out — a
module's SQL may not name a deployment's role — and the migration runner already
refuses to, decoding grantees out of an object's own ACL when it revokes
application access to the ledger. `platformkit_module_schema(owner)`, in the new
`migrations/000026_module_schema.up.sql`, is the one line a module's first schema
revision runs instead: it creates the schema named by that owner and hands each
role the migration role already grants defaults to *in the namespace that schema
opens beside* the privileges that deployment pinned for it there — grantee and
privilege list both read out of `pg_default_acl` rather than written down, one
namespace's rows and no union of several, so a role pinned to `SELECT` stays a
reader inside a module's schema and a role pinned only in some other namespace is
handed nothing by this function — a database-wide pin still reaches a module's
tables, because PostgreSQL applies it there and not because this mirror copied
it. No table moves here — the reference modules
keep theirs in `public` — and the check that every table is scoped to a tenant
stops holding a second copy of its own query:
`dbtest.TenantTablesSQL` is exported, gained the schema column, and matches a
schema against the ledger's owners instead of `current_schema()`, which was the
only schema until this. `migrations/README.md` states the consequences a
deployment can hit.

**A refusal now has an owner that imports nothing.** `kit/fault` declares
`ErrNotFound`, `ErrInvalid` and `ErrConflict`, and `kit/crud` re-exports those same
values rather than declaring its own, so the adapter that classifies a driver error
and the value package that refuses a write before a transaction exists hold one
object and `rest.Fault` answers 404, 422 and 409 once for either name. The package
exists because the alternative was a link: a module's `contracts/`, `events/` or
`domain/` package needing one of the three had to import `kit/crud` — gorm, a
driver, a `db.Tx` in every signature — for an error value. It links the standard library and
nothing else, which its own test asserts against `go list -deps`, the package gate
holds to an empty allowance, and that gate's fixture refuses as out of bounds if it
ever reaches `kit/db` — the edge back into `kit/crud` is an import cycle the
compiler refuses before any gate sees it. The messages do not move and they are not
inert: all three are still what a log line carries, `crud: invalid` and `crud:
conflict` are what a bare refusal reaches a client with as its problem detail, and
`crud: no such row` is not — a 404 is answered with the sentence `kit/rest` holds
for a row this tenant cannot see — while the literal `crud: invalid: ` is what
`kit/rest` trims off a detail to derive the field message a client reads, and
`modules/admin` asserts a person is never shown that prefix: the prefix names the
package that used to own these values, and renaming one is a wire change with
consumers on the other side of it. Eleven of this repository's `contracts/`
packages wrap one of the three under the adapter's name: three of a module's own
contract source — `modules/content/contracts` and `modules/file/contracts`, which
wrap `crud.ErrInvalid` for a body the Markdown renderer refuses and for bytes
that are not the type they were uploaded as, and `modules/user/contracts`, whose
own `ErrRegistrationExists` is a wrap of `crud.ErrConflict` — and the eight
shipped test-support packages one directory deeper, which refuse the way the
service each stands in for refuses. No file of `modules/auth/contracts` imports
that adapter now: this change moved it to `fault.ErrInvalid` and retired its
direct link. `kit/fault/README.md` names all eleven and what each still costs it. No
closure moves for any of them — each names `db.Tx` and imports `kit/crud` or
`kit/db` for `crud.Base` and a transaction-aware service of its own. The
reachability runs from the module to the kit package:
`modules/auth/contracts` reaches `kit/crud` through `modules/user/contracts`,
which imports it for `crud.Base` and not for a sentinel, and reaches `kit/db`
through four of its own files' service signatures; no kit package reaches a
module, which the compiler and `./scripts/check_imports.sh` both refuse. So
nothing here is a gate that now passes. What moves is adoption: the
alias has a value package wrapping the owner rather than only a name, and a
downstream consumer's value package does the same when its pin moves.

## [1.1.1] - 2026-09-18

A tooling patch. No exported API moved and no shipped behaviour moved: the diff from
v1.1.0 is one browser assertion under `tools/designexport/openpencil` and seven lines
of [RELEASE.md](RELEASE.md).

It exists because the Gitea run for v1.1.0 (88) concluded **failure** on the step
*Native browser observations*, and that step drives a file shipped inside this module:
anybody who checked out the tag and ran `npm run test:browser` in
`tools/designexport/openpencil` got the same `TimeoutError`. The suite now follows the
label-to-field association instead of asserting the literal control id
`pk-textarea-description`, which v1.1.0's namespace change had replaced. The full
browser suite is 265 of 265.

The prose change is the part that outlives this defect. `make check` and `make e2e` do
not run `test:browser` — the CI job does, as its own step — and when it failed on
v1.1.0, everything after it was skipped, including `make e2e` and the budget ratchet.
So RELEASE.md now states that an unread CI result is not a green one and that a release
is not ready while the verdict for the exact commit cannot be read. v1.1.0 was tagged
with that verdict unread: that, not the assertion, is the defect being repaired.

## [1.1.0] - 2026-09-18

**This release is not source-compatible with v1.0.0.** The pinned comparison
reports 71 exported changes between them — `kit/crud`'s row types are
`kit/entity`'s, `httpx.Document/Fragment/Script` and `design.CSS` moved into
`ui/page` and `ui`, `events.Memory()` became `memory.New()`, `migrations/` is the
kernel's schema alone, five methods joined `user/contracts.Registrations`, and
`ui.Compose` returns a `Sheet`. No `Deprecated:` shim repairs the 29 that are type
*moves*: apidiff resolves an alias and still reports
`contracts.User.Base: changed from crud.Base to entity.Base`, verified against a
two-revision module before this decision was taken.

It was taken deliberately rather than by omission. No consumer sits on the v1.0.0
stable line — the commercial catalog and the client application both pin
pseudo-versions of `main`, which establish no compatibility — so a v1.1.0 that
keeps the number breaks a build nobody has. It also breaks one nobody has *yet*:
`go get -u` from v1.0.0 now lands here and fails to compile, so **pin an exact
version**. What is owed and not yet paid is unchanged: an outside consumer needs
the `/v2` module path and import migration described in
[RELEASE.md](RELEASE.md#choose-the-compatible-release-line), and v1.1.0 is now the
tag every compatibility question is measured against. The
[accepted-break baseline](scripts/PUBLIC-API.md) that stood in for a release line
went with this tag; the line has no accepted break from itself.

The kernel stops deciding what it does not own. `ui/document` and `ui/resource`
render a document and a resource's screens from values — no database, no router
— and `ui/page` and `ui/screens` stay as the adapters that read a request and
carry the kernel's rules; `kit/entity/display` owns how a value reads, with
`kit/rest` delegating. `kit/app` no longer imports an event provider: the
application supplies `app.Transports{Memory, JetStream}` and the kernel selects
by name, refusing at `New` when the selected name has no constructor.
`events.Memory()` is removed — call `memory.New()`. Each reference module ships
its own SQL under `modules/<name>/migrations` and adopts the history the
foundation applied for it, so an existing installation is re-owned by checksum
and nothing re-runs; `migrations/` is the kernel's schema alone.
`scripts/check_packages.sh` records every new boundary. `ADR 0013` explains why
`kit/module` stays a typed manifest.

The composition layer becomes values a second shell can call. `ui.Compose`
returns a `Sheet`; `ui/page` holds `Chrome`, `Request`, `View`, `Frame` and
`Navigation`, with `page.Serve` as the one adapter between a handler and the
router; `ui/screens` is the seven generated pages of a resource as pure
renderers plus `Mount`, and `screens.Describe` publishes the same knowledge as
JSON at `GET /api/v1/admin/resources` for a shell that is not a browser.
`modules/admin` is composition only. Controllers read the sign-in path off
`<html>` and name no route; the confirm dialog's inline handler, which the
content security policy blocked, is gone. Ceilings re-baselined; two packages
join the binary.

## v1.0.0

The extracted reference architecture replaces the 0.x CLI scaffolder that lived
at this module path (releases to v0.15.1). The 0.x line is kept reachable under
the `legacy-0.x` branch and its tags; nothing from it is imported here. What
v1.0.0 is: `ARCHITECTURE.md`. What it promises about size: `loc-budget.json`.
