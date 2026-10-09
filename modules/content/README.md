# Content module

`modules/content` is the pages and posts a tenant's public site is made of: a
`rest.Spec` at `/api/v1/content/contents` with the draft, published and
archived lifecycle and its events, a public read at
`/api/v1/public/content/contents` (one published row per `{slug}`) that answers only for published rows, and the
Markdown renderer `contracts.Render` the [web module](../web/README.md) uses.
`content:read` and `content:manage` guard the routes and the admin entry at
`/app/content/contents`. There are no versions by design; see
[module.go](module.go).

## Interface

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

The admin create and edit pages are derived from the `rest.Spec` body field, not
handwritten pages. `ui/forms` renders a textarea and localized Markdown help;
`ui/resource` renders the saved value through `ui/components.Prose`. The public
slug route uses the same rich-text renderer with public file visibility. A
consumer can use `kit/richtext.Normalise`, `SourceHash`, `PlainText`, and
`Render` for other Markdown fields without importing this module's internals.
The native catalog publishes the field as `text/markdown`; native editing and
rendering require a separate client implementation.

## Verification

Run `make test TEST_PACKAGES='./kit/richtext/... ./modules/content/...'` with the
development database to exercise the parser, file boundary and lifecycle.
`make check` covers the generated screen, catalog and transaction integration.
`make e2e` runs the browser journeys in
[richtext.spec.ts](../../e2e/richtext.spec.ts), using reusable
[content steps](../../e2e/steps/content.ts): saving Markdown, inspecting the
prose view, refusing unsupported syntax, and reading a published page. The
browser gate needs the services and browser dependencies described in
[CONTRIBUTING.md](../../CONTRIBUTING.md#verify-at-the-relevant-boundary).

## Limits

The server accepts the subset described by `kit/richtext`; unsupported syntax
is refused on new writes with line and remedy details. Existing stored Markdown
is rendered through the legacy safe-read path. The server does not supply a
JavaScript editor or native rich-text controls. Products choose which other
fields use `widget:richtext`, their own copy and rules, and which audiences may
see a published item. File variants and record-bound file visibility are a
separate capability. The normalizer standardizes line endings, block spacing,
Setext headings and bullet markers, and stores as a code fence only the indented
runs the parser reads as code — a fence one backtick longer than the longest
backtick run inside it, so a line of ``` held in the code stays data.
Indentation a list item or a blockquote owns is left as the author wrote it: a
step's second paragraph and a nested list keep their four spaces, and a bullet
marker is rewritten only inside the single list the parser sees, so `* a` then
`- b` stay two lists. What `Normalise` returns is checked against the document it
came from before anything is written: it must parse, it must pass the validation
the submitted text passed, it must keep the words the plain-text projection reads
and it must be its own canonical form. Cutting a line short can change how the
next parse reads it — `<p\t` is a paragraph of text, the `<p` left after its tab
is trimmed opens an HTML block — and a source whose stored form fails that check
is refused on the write that found it, because the alternative is a committed
body the next write refuses. A body that passes is stored once: a second
`Normalise` over it changes nothing. An unterminated fence gains its closing
line. Other equivalent Markdown spellings —
an ordered list written `1)` rather than `1.`, or `__bold__` rather than
`**bold**` — can still yield distinct source hashes, so complete AST
serialization remains open.

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

The author of a page cannot publish it. `Content.PublishBy` in `modules/content/contracts/content.go` compares the row's `author_id` with `tenancy.ActorFrom(ctx)` and refuses with `crud.ErrConflict` naming authorship; `Service.Publish` asks it inside its own transaction, before it reads the status, so a refusal moves no column, emits no event and returns no row. `Content.Validate` stamps `AuthorID` from `tenancy.ActorFrom` when it is unset, and `spec.Immutable` (`status`, `publishedAt`, `author`) stops a patch from rewriting either. Content written where no person is acting — a seed, a job — has an unset author and publishes: there is no authorship to separate.
Unpublish and Archive are the withdrawals, so anyone holding `content:manage` may take a page back down; the duty stands on the way up only.
`contenttest.Fake` calls the same `PublishBy`, so the fake refuses the author too.

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
