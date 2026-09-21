# HTTP kernel

`kit/httpx` builds the one Huma API the application serves and enforces that
every operation declares its authorization. `New(Options)` returns the `*API`
and the router; `Register` mounts a handler with an `Auth` — `Public()`,
`SignedIn()`, `Permission(key)` or `OperatorPermission(key)` — and
`ValidateDeclarations` is the boot gate that refuses a route without one. Read
[httpx.go](httpx.go) for the middleware chain: which surface an address is, host
to tenant, the lazily opened tenant transaction (`TxFrom`, `ConnFrom`), the
request on the context (`RequestFrom`), the security headers and the per-request
nonce (`Nonce`).

A module's `Routes` receives `httpx.Surfaces` — three routers, one per surface —
and mounts on the one that fits the door. It never writes a prefix: `Prefix`,
`Path` and `PagePath` compose an address from the surface and the module, and
`Register` refuses a path that repeats either. See [surfaces.go](surfaces.go).

## Three surfaces

| Surface | Serves | JSON address | Document address |
| --- | --- | --- | --- |
| `Public` | a tenant's face to an anonymous visitor | `/api/v1/public/<module>/<rel>` | `/<module>/<rel>` |
| `App` | the workspace: the generated shell and a module's own values | `/api/v1/<module>/<rel>` | `/app/<module>/<rel>` |
| `Ops` | the installation's control plane | `/api/v1/ops/<module>/<rel>` | none — refused at mount |

The empty module is the composition's own namespace (`/api/v1/app/resources`,
`/app`): the kernel's routes, which no capability composes because the address
belongs to the composition rather than to a module. Only `kit/app` asks for it.

The public surface's JSON prefix says `public` and its document prefix does not:
a tenant's anonymous page answers at `/<module>/<rel>`, because the address a
visitor is given is the tenant's own site and not a partition of this
installation's — the module that claims the public root answers published slugs
at `/` and `/{slug}` beside it. That is also why `module.Validate` refuses the
module name `public`: a module of that name would compose `/public/…`, the one
address every other module is refused at mount for naming.

Each surface has its own chain, and the difference is not cosmetic:

- **Public** resolves no session and reads no cookie — a request holding one is
  answered as the anonymous request it is — sets no cookie (a handler that mints
  one is a 500 named `PUBLIC_SETS_A_COOKIE`, withheld at the writer at every body
  size, and a 500 with the body discarded wherever a response is still there to
  discard), caches a safe 2xx for sixty seconds, and puts a limit on an anonymous
  write because it is the one surface with no account to lock out
  (`Options.WriteLimiter`, counted by tenant, route and address).
- **App** recognises the caller, refuses an anonymous caller except at a route
  that declares `Public()` — and `AnonymousDoors` lists those, because "the
  workspace admits nobody by default" is only checkable once you can see who it
  admits — and answers `no-store` with `X-Robots-Tag: noindex`.
- **Ops** is served at `Options.Installation`'s host and nowhere else. Anywhere
  else it answers exactly what an address nobody mounted answers — the same
  status, the same body, and the same headers every other refusal of that host
  carries, because the gate runs inside the middleware that writes them, so the
  surface discloses nothing; and at the installation's host a tenant that is not
  the installation's is refused the same way, before the `Authorizer` is
  consulted. `app.Installation{Host}` is the deployment's fact, read from
  `server.installation_host`.

`Home` claims a surface's root for one module — the site's home page at `/`, the
workspace's at `/app` — and reports to the second claimant that it did not take
it. `accepted` is the closed table of which declaration each surface will host; a
contradiction is collected at mount and reported by `ValidateDeclarations`, so
the process never listens on a route whose address and chain disagree. A refusal names
itself with one of the published `Code*` constants (`AUTH_DENIED`, `LIMIT_EXHAUSTED`,
`WRITE_ELSEWHERE`, …) in the `detail` and in the log, and every guard answers it in the
shape the client asked for: the problem document for a value, the registered renderer's
page for a navigation (see [ui/page](../../ui/page/README.md)). An htmx write is a client
that parses a value — its controller reads the code and swaps nothing for a 4xx — so it is
answered in JSON even though it asks with `Accept: text/html,*/*`.

### One release of the old addresses

[aliases.go](aliases.go) is the whole migration: `/admin` → `/app` and the
four pages the admin module owns there (`/admin/login` → `/app/admin/login`,
and the same for `/health`, `/assets` and `/_gallery`), `/api/v1/admin` →
`/api/v1/app`, and the public doors of `auth`, `content`, `file` and `site`
under their own prefix. A row is a redirect — 302 for a safe
method, 307 for the rest, never a cached 301 — and never a second mount, so the
mount gate refuses anything that answers at a redirected address. Every row is
deleted in v1.3.0, one release after they shipped. What is deliberately absent is
`/api/v1/tenant/tenants`: a control plane does not announce itself by leaving the
old door open.

Pages are operations too: `HTML` mounts one, `Page` is its response shape,
`SeeOther` and `Redirect` end a write, `LocalPath` is the one rule for where a
browser may be sent, and `SignIn` names the form an anonymous visitor is sent
to. Rendering markup into a `Page` belongs to [ui/page](../../ui/page/README.md)
(`Render`, `RenderFragment`, `InlineScript`); this package imports no markup
library. `RegisterResource` and `Resources` are how [kit/rest](../rest/README.md)
publishes an entity so the admin generates its screens and catalog.

`Static` mounts an asset tree at the address the surface composes for it — inside
the surface's chain, so a workspace tree is `no-store` and `noindex` and a public
one is cacheable — and records it in `Mounted` beside every route, because a
composition's audit of what it serves has to include the files it serves. It is
refused at mount on `Ops`, which has no document address (the tree would compose
a public-looking prefix and answer at every customer's host), and refused at a
namespace's root, where the routes of that namespace answer: `Static("/assets",
tree)`, never `Static("/", tree)`.

## The database boundary

This package imports `kit/db`, and the import is the contract rather than an
accident of layering. Three signatures name a transaction on purpose:
`TenantLoader.ByHost` takes the `db.Tx[db.System]` the kernel opens for the one
cross-tenant read a request makes, `Options.Authenticate` takes the request's own
`db.Tx[db.Tenant]` so a session lookup runs under row-level security, and
`TxFrom` hands a handler that same transaction — the only door to the database
a route has. `ConnFrom` and `WithConn` carry the `db.Conn` a control-plane route
opens a second transaction on, and the middleware chain owns the lazy
`db.Pending`: it opens on the first query, commits below 400 and rolls back
above, and closes early for the one route that streams its body.

Moving the pipeline into a `kit/db`-owned package would leave every one of
those signatures here, so the closure would not change; only the reader's path
would grow. The boundary is therefore drawn at the signatures: `kit/httpx`
imports `kit/db` for the transaction a request is, and imports nothing that
executes SQL of its own — resource registration reads `entity.Schema` and
`entity.Field`, and the single `kit/crud` value it names is `Query`, the page a
`Resource.List` is asked for. Presentation that needs neither lives in
[ui/document](../../ui/document/document.go) and
[ui/resource](../../ui/resource/resource.go); this package renders nothing.

Prerequisites: a `db.Conn`, a `TenantLoader`, an `Authorizer` and the
authenticate hook; [apps/platformkit](../../apps/platformkit/modules.go) is the
composition to copy. Tests run against the development database: `make up`,
then `make test TEST_PACKAGES=./kit/httpx`.
