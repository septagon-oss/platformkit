# HTTP kernel

`kit/httpx` builds the one Huma API the application serves and enforces that
every operation declares its authorization. `New(Options)` returns the `*API`
and the router; `Register` mounts a handler with an `Auth` — `Public()`,
`SignedIn()`, `Permission(key)` or `OperatorPermission(key)` — and
`ValidateDeclarations` is the boot gate that refuses a route without one. Read
[httpx.go](httpx.go) for the middleware chain: host to tenant, the lazily
opened tenant transaction (`TxFrom`, `ConnFrom`), the request on the context
(`RequestFrom`), the security headers and the per-request nonce (`Nonce`).

Pages are operations too: `HTML` mounts one, `Page` is its response shape,
`SeeOther` and `Redirect` end a write, `LocalPath` is the one rule for where a
browser may be sent, and `SignIn` names the form an anonymous visitor is sent
to. Rendering markup into a `Page` belongs to [ui/page](../../ui/page/README.md)
(`Render`, `RenderFragment`, `InlineScript`); this package imports no markup
library. `Static` serves an asset tree beside the API. `RegisterResource` and
`Resources` are how [kit/rest](../rest/README.md) publishes an entity so the
admin generates its screens and catalog.

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
