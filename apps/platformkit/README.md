# The reference application

`apps/platformkit` is the application the foundation ships and the one every gate
runs: `platformkit run`, `platformkit start`, `platformkit seed`, `platformkit
migrate` and `platformkit bootstrap` all go through the one sentence in
[app.go](app.go), and the text `pkit.Server.Explain` answers for it is committed
beside that file as [COMPOSITION.development.md](COMPOSITION.development.md) and
[COMPOSITION.production.md](COMPOSITION.production.md). A sixth command,
`platformkit repair-roles`, reads that sentence with no process running: it lists,
or with `--remove` narrows, the grants an older seeder wrote that this composition
no longer defines ([modules/auth](../../modules/auth/README.md)). A client's own
composition looks like this one with different modules in `Use` and different
values in `product`.

## Composition

Identity authorization is provided by `auth.Grants` independently of session
construction. User builds next, then Auth; the resolver checks these typed edges
before opening resources. App commands retain `Lister`, `HostLookup`, provider
readers, `Bootstrapper`, `LocaleSetter`, `SystemProvisioner`, `AddressLookup`,
`SeedWriter`, `PermissionReader` and `RoleRepairer` outputs from the plan.

The whole of it is `pkit.NewApp("platformkit")` and the words after it. `Use`
names every module this application is made of, in the order the composition is
built in, and each of them wires itself: an edge between two modules is a contract
one of the two declares in its own `provider.go`, so there is no edge table here to
keep in step with the graph and no `Deps` literal in this directory. `product` is
first in the list and needs nobody — it is what this installation decides and no
module may (`file.Local(dir)`, the rego policy, the flag that gates a settings
write, the plan feature the trail is sold under, the two addresses the public site
links, the sign-in form the shell posts to, the languages a locale may be set to,
the roles a new tenant is seeded with). `access`
is second to last because it is the first thing here that can name a person, a
notice, an authorizer and a site at once: the reach a refusal's ask has here, and
the one subject this product puts under change control. `media` sits immediately
before `file`, because the media library's page is mounted by the module that owns
the files and the chrome it is drawn in is this installation's. `admin` declares
`After(Everything)` and is last, because it generates a screen for every resource
the modules above it mounted.

**Reused** — `auth.Grants`, `auth.Module`, `user.Module`, `tenant.Module`,
`notification.Module`, `file.Module`, `task.Module`, `billing.Module`,
`content.Module`, `site.Module`, `web.Module`, `audit.Module`,
`change.Module` and `admin.Module`, each of which builds its own manifest from
the contracts it declares; `auth.EmailRegistration` for the one public signup
door this installation opens; `pkit.Composition` for the shell's navigation,
`pkit.Config` for the settings a module cannot decide, `Wiring.Skin` for the
theme and the copy the app named with `Theme` and `Languages`, and `pkit.Value`
for the services `bootstrap`, `migrate` and these tests read off the resolved
plan. **Added** — the three modules in [product.go](product.go), because a
composition still has to say what is its own, and saying it as modules that
declare what they provide is the only shape in which the resolver can check it,
refuse a missing provider by name and print the edge; nothing existing could
carry it, since the file they replace (`modules.go` and `wiring.go`) built every
service by hand and stated each edge a second time. **Made reusable** — the
`product`/`access` pair itself: a first module of product-owned values with no
needs, and a late one for the joins that need services, which is the shape every
client's `Use` list now has; and `deploymentInputs`, the one place a process names
which `FromDeployment` implementations its deployment has inputs for; and
`reference.describe`, which commits what the resolved composition says as
[COMPOSITION.development.json](COMPOSITION.development.json) and
[COMPOSITION.production.json](COMPOSITION.production.json) beside the text file,
named by `platformkit.composition.v1`, written by `make generate` and refused on
drift by the same check — names only, so a machine can read what this installation
is composed of without reading a configuration value out of it.

The one edge no module declares is `user`'s promotion check, answered by the auth
service. Declaring it either way is the cycle `user → auth → user`, which is a fact
about the graph and not a mistake a composition can edit: `product` puts the empty
holder, `user` reads it and `auth` fills it in its own build. That is
`pkit`'s tested late-bound holder, and an unfilled holder fails the roles write
with no row, no event and no stale read.

What the counting door (`summary.go`, `/api/v1/app/summary`) composed and what it
wrote: **Reused** — `crud.Count` for the total, `(*API).guard` and
`Resource.Readable` for who may hear it, `httpx.TxFrom` for the tenant scope,
`cache.Of`/`Get`/`Set` and their generations for the fifteen seconds a total is
believed, `Options.WorkspaceCatalog` and `workspaceFace` for the address, the
`connectionDocument` envelope for the way a body type survives into the published
document, `entity.EntryHints` and `entryHintsFault` for the declaration and its
refusal, `ui/screens`' pointer-per-key `declared`/`deviations` rule for the echo,
`ui/resource.Words` for the one word. **Added** — `crud.CountWhere` and
`crud.CountDays`, because `Query.Filter` reaches equality only and `comparable`
says so in its own comment, and no grouped read of an entity's column existed;
`Resource.CountFiltered`/`CountDays` and `API.Cache()`, the store having been
reachable only as a private field; `screens.Summary` and `Summarise`, `ui/screens`
having held no per-resource number; the `highlight` and `trendField` hints; and one
partial index, `due_at` having never been read as a range before. **Made
reusable** — the predicate grammar (`crud.Ops`, `Predicate`, `Where`) with the one
coercion table `Coerce`, which `?filter=` now shares with a declared value, the
anchor resolution (`Declared`, `DayStart`) that lets a declaration name "today"
instead of a date, and a second worked `/api/v1/app` mount whose body type reaches
the OpenAPI document.

What the counting door does not do, stated here because a shell reading
`/summary` decides what to draw from it:

- **The budget bounds the wait, not the query.** `summaryBudget` is a deadline on
  the parts of a request that carry it — the store round trips and the read-guard
  decision. The counts run in the request's own transaction, whose session was
  opened before that deadline existed and whose reads take no context, so a
  statement is cut short by the installation's `database.statement_timeout` and by
  nothing in this file.
- **A card costs one statement per question it declares**, not one per card: the
  task card asks a total, a sub-count and a daily series, so three.
- **"Due today" is a window on `dueAt` and nothing else.** A task resolved or
  closed during the same day is still counted in it, because the grammar has no
  `ne` or `in` and the task entity's own `doneValues` vocabulary has no way to
  reach a declared filter. The brief specified the window; narrowing it to open
  work is the module's, and the operator it needs is not written here.
- **"Today" is the UTC day**, not the person's, and no locale or time zone enters
  the cache key — which is also why the body carries no word.
- **One cache key per tenant and resource** is sound while a read grant is
  tenant-wide, which every grant in this installation is. An authorizer that ever
  decides per row makes that count the wrong answer, and the key would have to name
  the grant set or the document would go uncached.
- **A resource that carries no count closure has no number, permanently.** `file`
  registers its routes and its own library page by hand and names no `Count`, so
  its card is absent for every caller, including an administrator — which is the
  truth rather than a zero, and is the file module's to add over the rows its page
  lists.
- **The count is the whole live set**, never the open one: the "total open" the
  brief first asked for is not answered here, because "open" is a set of statuses
  no shared module may name.

## What lives where

| file | what it is |
|---|---|
| [app.go](app.go) | the sentence, the resolved composition, and the two entry points every command shares |
| [product.go](product.go) | the three modules this application composes: the values it owns, the chrome it hands a module's own screen, and the joins |
| [summary.go](summary.go) | the workspace's counts: what each resource the caller may read holds, at one instant |
| [roles.go](roles.go) | the list somebody wrote down of who a new tenant begins as |
| [change.go](change.go) | the one subject under change control, its flag, and the gate |
| [fault.go](fault.go) | the refusal page, the two ask pages, and the addresses they are pinned to |
| [catalog.go](catalog.go) | the one copy table every shell and page is worded from |

Run it with `make run`, and verify it with `make check` and `make e2e`. The
composition is refused at boot — `Build` answers every problem at once, each
naming the method that caused it — so a client that removes `user.Module` from
`Use` is told `auth needs authcontracts.Users: add user.Module to platformkit`
rather than finding out at the first login.

## Authorization

Decision 0011 asks every composed module's README the same six questions. This one
answers for the three modules this application declares in `product.go`, because no
`modules/<name>/README.md` exists to answer them. [authorization_readme_test.go](authorization_readme_test.go)
reads this section against their manifests, the same way it reads the kernel
modules'.

### Permissions

None of `product`, `workspace`, `access` or `media` declares a permission: a manifest with no
grant to hand out. Every permission the running application offers belongs to the module
that owns the subject it governs — `user` for people and roles, `content` for
pages, `billing` for plans — and this application adds none.

### Object scope

All four work inside one tenant and never across two. `product` hands out values that
are read once at composition — the session cookie, the rate buckets, the storage
bucket, the scheduler cadence — and joins that are constructed from a `db.Tx`
carrying the tenant the request resolved, which is where tenancy is enforced here.
`workspace` hands out one value, the shell a module's own page is drawn in, and
reads nothing. `access` joins a person, a notice and a site to decide who may be let in, and every
row it reads is behind that same transaction. `media` reads nothing: it hands
`file.Module` the shell the library is drawn in and the authorizer that decides
which of the page's two write controls a person is shown — the check itself is
`kit/httpx`'s, on the route.

### Duties the module enforces itself

None enforces anything. They provide no route of their own, no duty and no check:
`product` answers contracts, `workspace` names the chrome and mounts nothing,
`access` fills the reach an ask has here and the
authorizer the sign-in page asks, and `media` hands the library its chrome — the
guard on the page's two posts is `file`'s own permission, asked of the composition
by the router. The enforcement is the modules' whose services they join, and
`kit/rest` and `kit/httpx` for the grant a route names.

### Public faces

No face of its own. The two pages an unauthenticated visitor sees — the ask page
and the signed-out page — are mounted by `app.go` with `AskForAccess` and
`page.MountAccess`, drawn in the fault shell this application writes in `fault.go`;
the sign-in screen belongs to `admin`.

### The operator boundary

Nothing here is the operator's, because nothing here is a grant. What a deployment
rather than an operator decides is the two choices in `deployment.*` — how mail is
sent and how money moves — and both are refused at boot when the deployment leaves
them ambiguous.

### Provisioning

`product` provisions no subject either: a new tenant's people, roles and seeded
content are provisioned by `user`, `auth` and `site` when the tenant is created,
and `roles.go` is the list of who the first person is. What `product` decides at
composition is which storage backend, mailer and payment gateway a tenant's first
administrator will find.

## The test installation

Three sentences, for the next person who changes how this package gets a database.

**Reused** — `kit/db/dbtest`'s `RunInOwnDatabase` for the package's own database, `URLs` and `Open`
for a case's namespace and handles, this package's own `bootstrap`, `install`, `compose` and `start`
for what a case stands on, unchanged. **Added** — one prepared installation per package
(`dbtest.RunInOwnDatabaseWithTemplate`, run by `main_test.go`'s `prepareInstallation`) that a case
copies instead of reinstalling (`dbtest.Clone`, taken by `configure`), because two hundred cases were
each paying the composition's whole migration at one database-wide advisory lock, and no existing
unit handed out an installation that was already made; a case for which installing *is* the claim
takes `configureFresh` and installs in full. **Made reusable** — the template-and-copy primitive
itself, the namespace statements `URLsFor` and the template now share, and a parity case that says a
copy answers what a fresh install answers, which is what lets the rest of the package take the copy.

A copy is named after the case that took it, and the number a case that asks twice needs is part of
that name: `dbtest.Role` names the login role it creates after the namespace the copy lives in, so a
namespace that filled PostgreSQL's identifier limit would have named two copies' roles the same and
the second grant would have landed on the first copy's database. The budget in `dbtest.cloneName`
leaves room for the prefix and the ask number, and `dbtest.identifier` keeps the tail of a name the
server would otherwise cut — the contract is on `Role`, and
`TestEachRepeatedCopyCanHoldAnIndependentRoleInItsOwnDatabase` is the case.

### The port a case serves at

Three sentences, for the next person who changes how this package gets an address.

**Reused** — `freeAddr`'s band, its tries and its loopback probe, the `chosenPorts` map, `bootWait`,
and the two observations `waitServing` and `servingHandler` make, unchanged. **Added** — `claimPort`,
one exclusive `flock(2)` per port number in `/tmp`, because the choice released the socket it had just
proved free and the application under test binds that number only after migrating a whole
installation: the window between the two is seconds alone and minutes on a loaded host, and a second
run of this package from another checkout could be handed the same number inside it — `chosenPorts`
remembers one process, and nothing in the tree held a port across processes. **Made reusable** — the
claim itself, an empty file per port locked until the case ends and never unlinked, named by
`portClaimPath` and witnessed by `TestAPortTakenWhileBootingIsNotTheCasesPort`. A claim is advisory,
so the serving wait stays the proof of which application is behind an address.

## First use

`platformkit start` is the one command a stranger runs, so it answers every
question a machine can answer about itself before it downloads or writes anything.
The order is the whole of the repair: the pair `GOOS`/`GOARCH`, the bind of
`--addr`, whether `--data` can be created and written, whether the user's cache
can, and what `--data/postgres` already holds — a live postmaster, or a cluster of
another PostgreSQL version. Each refusal names the value it refused, the flag or
path that would move it, the retry, and what it did not do — which is stated
truthfully for how far the run got: nothing written, nothing deleted, no server
stopped, no tenant created. None of them stops a process or a port this run did
not start. Then the download, the role, the migrations and the bootstrap, and the
banner the README shows.

`--addr` defaults to `127.0.0.1:8080` — a host, not a bare port. This command
prints an administrator's password to the terminal, so the address it answers with
when nobody names one is the loopback; `--addr :8080` binds every interface the
machine has and is how a person says they mean it. The preflight holds the address
until the application is about to bind it and then lets go; if something takes it
in that gap the application's own bind refuses and says the same thing again, plus
that the installation under `--data` stands untouched and rerunning reuses it.

The supported rows are the four `linux/amd64`, `linux/arm64`, `darwin/amd64` and
`darwin/arm64`, declared once in `supportedTargets` and read by nothing but the
preflight and its refusal sentence. WSL2 is not a row: it answers as Linux, so
nothing in this directory can branch on it. Windows is refused rather than
tried — no release has run it, and a promise nobody has tested is not a promise.

The version the banner prints is the version the footer prints, which is
`admin.Build()`: a build made through the module proxy carries a version and no
repository stamp, a build made from a checkout carries the stamp and no version,
and each is the only answer the build that carries it has.

**Reused** — `startApp`'s own `startConfig`, `ensureRole`, `freePort` and
`composeReference`, `embedded-postgres` unchanged as the launcher, `admin`'s
existing footer stamp, `kit/app`'s `serve` and its `http.Server`, and the rule
`scripts/free_port.sh` and `check-run-owner` already state — allocate, refuse,
never drive a listener you did not start. **Added** — the preflight and its
refusal sentences, because the tree held no prerequisite check of any kind and the
only support record it holds, `scripts/supported_sources.sh`, answers which
installations a release upgrades from rather than which machines it runs on; and
the guard that keeps the launcher from deleting a data directory it does not
recognise, which is inside that dependency's own reuse decision and can only be
refused by the caller that owns the path. **Made reusable** —
`scripts/redact_credentials.sh`, which any harness that captures a first run can
pipe through, and `scripts/first_use.sh` with the receipt form in
[FIRST-USE.md](FIRST-USE.md): a measurement that writes nothing unless it is true.
