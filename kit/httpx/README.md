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

Prerequisites: a `db.Conn`, a `TenantLoader`, an `Authorizer` and the
authenticate hook; [apps/platformkit](../../apps/platformkit/modules.go) is the
composition to copy. Tests run against the development database: `make up`,
then `make test TEST_PACKAGES=./kit/httpx`.
