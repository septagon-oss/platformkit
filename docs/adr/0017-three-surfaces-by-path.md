# 0017: An address says which surface it is, and a module never writes one

Status: accepted. `kit/httpx`, `kit/app`, `kit/rest`, `ui/{page,screens,resource}`,
`modules/admin`, `modules/auth`, `modules/file` and the reference composition
(`apps/platformkit`) are implemented and tested against each other. The one-release
redirects in [aliases.go](../../kit/httpx/aliases.go) are deleted in v1.3.0.

## Problem

One router answered everything, and an address was the only place a request's
promises were written down — in prose, in whichever module happened to read the
prefix first.

The generated shell lived at `/admin` and `/admin/<module>/<entity>`, in the same
namespace as a tenant's own content, so a storefront slug could collide with a
screen, and a stylesheet at `/admin/assets` was a workspace asset reachable at a
customer's host. `GET /api/v1/admin/resources` — the document a native client is
built from — was mounted by a module and therefore had a module's permission on
it, which is how a catalog ends up describing different installations to two
callers. The anonymous doors of `auth`, `content`, `file` and `site` were ordinary
routes on the same chain as authenticated workspace work: every one of them read
the session cookie a browser presented, and a public page that is cached by
everybody was answered with `no-store` because the chain had one opinion about
caching. `modules/tenant`'s tenant API was mounted at every host, so a customer's
administrator holding the wildcard their own role legitimately carries could list,
create and suspend tenants at their own host — which is the incident this repository
already documents, refused now by a flag on the grant.

There was nothing wrong with any of these routes. What was missing was a *layer*
that could say "this address belongs to a different surface, so it gets a different
chain" — and that layer cannot live in a module, because a module sees only its own
doors.

## Decision

`kit/httpx` classifies an address into one of three surfaces and gives each its own
middleware chain. A module's `Routes` receives `httpx.Surfaces` — `Public`, `App`,
`Ops` — and mounts on a router; the router composes the address from the surface and
the module, and refuses a relative path that repeats either.

| Surface | JSON | Document | Session | Caching |
| --- | --- | --- | --- | --- |
| `Public` | `/api/v1/public/<module>/…` | `/<module>/…` | never read, never set | public, 60s on a safe 2xx |
| `App` | `/api/v1/<module>/…` | `/app/<module>/…` | read; `Public()` routes are the only doors for nobody | `no-store`, `X-Robots-Tag: noindex` |
| `Ops` | `/api/v1/ops/<module>/…` | refused at mount | read | `no-store`, `noindex` |

Four things follow, and each is enforced where it can be:

**The workspace's JSON keeps its address.** `/api/v1/<module>/<rel>` is unchanged;
what moved is where a *document* lives (`/app/<module>/<rel>`) and the public doors,
which took `/api/v1/public/`. A native build already installed is a fact about the
world, and moving its door for tidiness would cost a release nobody asked for.

**The control plane is served where the installation is reached, and nowhere else.**
`app.Options.Installation` carries the host — `server.installation_host`, the
deployment's fact, named by nothing in a module — and `Ops` answers at any other
address with the *same bytes* an unmounted address gets. A second, independent
guarantee survives next to it: a data row may point the installation's host at some
tenant, and there the tenant flag refuses it before the `Authorizer` is consulted.
The host gate and the tenant flag are not one check written twice; either alone would
be defeated by the thing the other one covers.

**Mount-time contradiction is a boot failure.** A signed-in route on `Public` is
unsatisfiable — that surface reads no session — so `ValidateDeclarations` refuses it
before the process listens, as it refuses a page on `Ops`, a second claim of a surface
root, a duplicate mount, and a route whose address classifies as another surface than
the router that mounted it. The table is one pure function (`accepted`) so the gate and
the request chain cannot hold two opinions about it.

**A root has one owner.** `Surfaces.Home` claims `/` and `/app` for the first module in
composition order and tells the next claimant false, because "who answers the visitor at
the root" is one address and cannot have two answers in a composition nobody read in
order.

## What it costs

- `modules/*` lost the `const Path = "/api/v1/…"` each of them carried. A module that
  cannot write its prefix cannot write it wrongly, but a module that wants to *link*
  another module's screen needs the composition to say whether that namespace exists
  (`Router.Known`, `Router.ForModule`).
- Two addresses are composed rather than written: the generated screen of a resource
  (`httpx.Resource.Screen`) and the catalog's address. Both are read from one record
  rather than derived a second time in `ui`, which is what let `ui` and the kernel
  disagree about `/admin`.
- `modules/admin`'s catalog is gone: `/api/v1/app/resources` is mounted by `kit/app`,
  because the address is the kernel's, and the *document* is supplied by the
  composition (`app.Options.WorkspaceCatalog`), because `kit` may not import `ui`.
- `apps/platformkit` pins four literals (`/app`, `/app/admin/login`,
  `/app/admin/assets` and the file module's public door, which the site's markup
  links) and the auth door its login form posts to. It may, because the
  failure page is rendered by the kernel before any module can answer. It is the
  product pinning a composed address, and `TestPinnedAddresses` asks the running server
  that each one answers. A module pinning one would be the same mistake this ADR exists
  to end.
- The alias table is thirteen rows of dead addresses that must be deleted in v1.3.0. An
  alias is a redirect — never a 301, never a second mount, and the mount gate refuses
  anything that answers where a row points from, so a redirect cannot quietly become a
  route that shadows itself.

## Considered and refused

*A allowlist of anonymous paths in composition.* It is a second list of the same doors
the declarations already record, and such a list drifts towards being open: forget a row
and a route answers publicly, add a row and nothing breaks loudly. The declaration is
the authority; `AnonymousDoors` reads the doors back off the mounts and boot prints them.

*Separate hosts per surface.* Cleanest at the network layer, and it asks every
installation for three certificates, three DNS records and three release pipelines for a
promise Go can keep in one process. `Installation` is a string, and an operator who wants
the separation can already have it by pointing it at another host.

*Surfaces as a field on the route declaration* (`Auth` carrying where it lives). It puts
the address in the declaration and the declaration in the address, and the two then
disagree silently. The router an operation is mounted on is the fact; the declaration says
what it asks of the caller, which is a different question.

*A `/public` page prefix for anonymous documents* is refused, and the refusal is why
`module.Validate` refuses the module name `public` as well. A public document composes
`/<module>/…`, because what a visitor is handed is the tenant's own site and not a
partition of this installation: the module that claims the public root answers that
site's published slugs at `/` and `/{slug}` beside it. A module named `public` would
therefore take `/public/…` as a namespace — the word the surface is named by, the
segment the public JSON address already carries, and a prefix every other module is
refused at mount for writing. Prefix and surface would be spelled with the same letters
and mean two different things, which is the disagreement this ADR exists to make
unwritable.
