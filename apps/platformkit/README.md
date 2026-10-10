# The reference application

`apps/platformkit` is the application the foundation ships and the one every gate
runs: `platformkit run`, `platformkit start`, `platformkit migrate` and
`platformkit bootstrap` all go through the one sentence in
[app.go](app.go), and the text `pkit.Server.Explain` answers for it is committed
beside that file as [COMPOSITION.development.md](COMPOSITION.development.md) and
[COMPOSITION.production.md](COMPOSITION.production.md). A client's own
composition looks like this one with different modules in `Use` and different
values in `product`.

## Composition

The whole of it is `pkit.NewApp("platformkit")` and the words after it. `Use`
names every module this application is made of, in the order the composition is
built in, and each of them wires itself: an edge between two modules is a contract
one of the two declares in its own `provider.go`, so there is no edge table here to
keep in step with the graph and no `Deps` literal in this directory. `product` is
first in the list and needs nobody — it is what this installation decides and no
module may (`file.Local(dir)`, the rego policy, the flag that gates a settings
write, the plan feature the trail is sold under, the two addresses the public site
links, the sign-in form the shell posts to, the languages a locale may be set to,
the roles a new tenant is seeded with, and the empty grant-check holder). `access`
is second to last because it is the first thing here that can name a person, a
notice, an authorizer and a site at once: the reach a refusal's ask has here, and
the one subject this product puts under change control. `admin` declares
`After(Everything)` and is last, because it generates a screen for every resource
the modules above it mounted.

**Reused** — `auth.Module`, `user.Module`, `tenant.Module`,
`notification.Module`, `file.Module`, `task.Module`, `billing.Module`,
`content.Module`, `site.Module`, `web.Module`, `audit.Module`,
`change.Module` and `admin.Module`, each of which builds its own manifest from
the contracts it declares; `auth.EmailRegistration` for the one public signup
door this installation opens; `pkit.Composition` for the shell's navigation,
`pkit.Config` for the settings a module cannot decide, `Wiring.Skin` for the
theme and the copy the app named with `Theme` and `Languages`, and `pkit.Value`
for the services `bootstrap`, `migrate` and these tests read off the resolved
plan. **Added** — the two modules in [product.go](product.go), because a
composition still has to say what is its own, and saying it as two modules that
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
named by `platformkit.composition.v1` and refused on drift by the same
`UPDATE_GOLDEN` check — names only, so a machine can read what this installation
is composed of without reading a configuration value out of it.

The one edge no module declares is `user`'s promotion check, answered by the auth
service. Declaring it either way is the cycle `user → auth → user`, which is a fact
about the graph and not a mistake a composition can edit: `product` puts the empty
holder, `user` reads it and `auth` fills it in its own build. That is
`pkit`'s tested late-bound holder, and an unfilled holder fails the roles write
with no row, no event and no stale read.

## What lives where

| file | what it is |
|---|---|
| [app.go](app.go) | the sentence, the resolved composition, and the two entry points every command shares |
| [product.go](product.go) | the two modules this application composes: the values it owns, and the joins |
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
answers for the two modules this application declares in `product.go`, because no
`modules/<name>/README.md` exists to answer them. [authorization_readme_test.go](authorization_readme_test.go)
reads this section against the two manifests, the same way it reads the kernel
modules'.

### Permissions

Neither `product` nor `access` declares a permission: a manifest with no grant to
hand out. Every permission the running application offers belongs to the module
that owns the subject it governs — `user` for people and roles, `content` for
pages, `billing` for plans — and this application adds none.

### Object scope

Both work inside one tenant and never across two. `product` hands out values that
are read once at composition — the session cookie, the rate buckets, the storage
bucket, the scheduler cadence — and joins that are constructed from a `db.Tx`
carrying the tenant the request resolved, which is where tenancy is enforced here.
`access` joins a person, a notice and a site to decide who may be let in, and every
row it reads is behind that same transaction.

### Duties the module enforces itself

Neither enforces anything. They provide no route, no duty and no check: `product`
answers contracts, `access` fills the reach an ask has here and the authorizer the
sign-in page asks. The enforcement is the modules' whose services they join, and
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
