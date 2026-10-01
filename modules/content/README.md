# Content module

`modules/content` is the pages and posts a tenant's public site is made of: a
`rest.Spec` at `/api/v1/content/contents` with the draft, published and
archived lifecycle and its events, a public read at
`/api/v1/public/content/contents` (one published row per `{slug}`) that answers only for published rows, and the
Markdown renderer `contracts.Render` the [web module](../web/README.md) uses.
`content:read` and `content:manage` guard the routes and the admin entry at
`/app/content/contents`. There are no versions by design; see
[module.go](module.go).

## Composition

**Reused** — `kit/rest`, `kit/entity`, `kit/richtext`, `ui/forms`,
`ui/components.Prose`, and `modules/file.RichTextFiles` compose the rich text
field, with the file resolver passed as `content.Deps.Files` per request.
**Added** — `kit/richtext` owns the validated Markdown subset and tenant-scoped
image resolution because no earlier kernel package could validate, normalize,
extract, and render one stored format for both generated screens and public pages.
**Made reusable** — the `widget:richtext` tag, `richtext.Files` port and fake,
and `Prose` component let another `rest.Spec` entity use the same field path.

When `Deps.Files` is absent, image references are refused on write; text-only
Markdown still works. `Body` is limited to 262,144 Markdown code points and
1,048,576 stored UTF-8 bytes. Public rendering uses the same rich-text renderer
as the admin view and resolves images for the public audience.
Consumers import [contracts/](contracts/) and its [fake](contracts/contenttest/),
never `internal/`. `make test TEST_PACKAGES=./modules/content/...` needs the
development database.

## Authorization

### Permissions

The manifest in `modules/content/module.go` (`permissions`) declares two keys, defined in `modules/content/contracts/permissions.go`.
`content:read` is the `Read` permission of the `rest.Spec` (list and read routes) and guards the "Content" nav entry and its generated screen `content/contents`.
`content:manage` is the `Write` permission of the same spec: create, update and delete routes, and the `publish`, `unpublish` and `archive` commands registered by `RegisterRoutes` in `modules/content/internal/handler.go`.
The public slug route is guarded by neither key (see Public faces).

### Object scope

None. A search of the module finds no call to `tenancy.Policy` and no `Resource.Kind`.
Scope is the tenant: rows carry the tenant id and row-level security matches on it (`Deps` in `modules/content/module.go`).
The code does not show any per-object attribute check.

### Duties the module enforces itself

The module has no ownership or separation-of-duties refusal.
`Content.Validate` in `modules/content/contracts/content.go` stamps `AuthorID` from `tenancy.ActorFrom` when it is unset, and `spec.Immutable` (`status`, `publishedAt`, `author`) stops a patch from rewriting them.
Status moves only through `Service.Publish`, `Unpublish` and `Archive` (`modules/content/internal/service.go`), which publish an event in the same transaction.
These are lifecycle rules, not checks on who the caller is.

### Public faces

One route is public: `GET /contents/{slug}` (`httpx.Public()` in `RegisterRoutes`, operation `content-content-public`).
It returns `Page` in `modules/content/internal/handler.go`: `slug`, `title`, `kind`, `html` (rendered and sanitized) and `publishedAt`.
A draft, an archived page and an unknown slug are the same 404, and a host that resolves to no tenant is also a 404.
It is a read. The module has no public write, so `kit/limit` (used by the public-write limit in `kit/httpx/surfaces.go`) does not apply to it.

### The operator boundary

None. No permission in `permissions` sets `Operator: true`, and the module registers no `OperatorRead` or `OperatorWrite` route.

### Provisioning

The code names no role. A composition grants `content:read` to people who may see the content list, and `content:manage` to people who may edit and publish.
Roles are granted by permission key through the roles API (`PUT /api/v1/auth/roles/{name}`, cited in `modules/user/contracts/administration.go`) or in the composition's own role definitions.
This repository does not show a client `client.yaml` for this module, so the exact file is not documented here.
