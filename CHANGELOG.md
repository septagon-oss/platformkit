# Changelog

## Unreleased

Two of these entries, one property: a tenant keeps somebody who can administer
it. Each was written because the state was reachable, not because a race was
reported, and each says below what it leaves open rather than leaving that to a
reader who depends on it.

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
where the Spec soft-deletes. `db.Now` stamps the three timestamp columns, so an
answer carries the instant the column holds. What stays open: a `Singleton`
declares no command-owned field.

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
