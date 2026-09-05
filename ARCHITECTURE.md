# Architecture

PlatformKit is a public foundation for composing multi-tenant SaaS products.
Its boundaries should make products easy to assemble, understand and upgrade.
The ideas below describe the current implementation; CONTRIBUTING.md defines
how changes are reviewed for readability, ownership and demonstrated behavior.

## The ten ideas

1. **Composition is a list.** An application is a slice of modules constructed in
   `apps/platformkit/modules.go` with typed dependency structs in dependency
   order. A module is `Module(Deps{...})`: one function, one struct.
   Compilation checks dependency types; composition tests check that required
   dependencies are supplied and the intended modules are selected.
2. **A module is three things.** `contracts/` (interfaces, DTOs, events,
   permission tokens, and a conformance suite), `internal/` (every
   implementation), and `module.go` (the manifest). `modules/task` is the
   exemplar every later module copies.
3. **Cross-module dependencies are Go interfaces.** A consumer takes an
   interface declared in the provider's `contracts/`; `internal/` makes any
   other coupling a compile error. A `contracts/` package is the entity, the
   events and the interfaces, so it imports `kit/crud` and never `kit/rest`:
   naming another module's Task links no web server. Gate 6 is that rule.
4. **Tenant isolation belongs to the database, and to the type system.** Tenant
   tables carry `FORCE ROW LEVEL SECURITY` and the tenant is set per
   transaction, so a forgotten `WHERE tenant_id` returns nothing rather than
   another tenant's rows. `db.Tx[db.Tenant]` and `db.Tx[db.System]` are distinct
   types, so crossing the tenant by accident does not compile and crossing it on
   purpose is one grep. The settings themselves are `USERSET` and no privilege
   can withhold them, so the grep gate is what closes that door; the re-read
   before commit is a backstop that catches the careless escape and not the
   deliberate one. See `docs/adr/0003`.
5. **Authorization is declared, not remembered.** Every operation registers an
   `Auth` value alongside its route; the app validates the recorded operation set
   at boot and refuses to start when one is undeclared.
6. **Events leave through one door.** A module writes an event in the same
   transaction as its state change; one outbox relay publishes to JetStream.
   Delivery is at-least-once and handling is exactly-once: `Consume` claims each
   event for each subscription in the handler's own transaction.
7. **Capabilities own their migration histories.** One runner applies the
   foundation and selected modules in composition order. Each owner numbers its
   own files. SQL and its checksum/history row commit together; failed files
   roll back. Applied files are immutable. See docs/adr/0010-migration-ownership.md.
8. **Screens derive from schemas.** `rest.Spec.Mount` registers the entity
   beside its routes, and `modules/admin` generates seven pages from each one:
   list, detail, two forms, two writes and delete. A `select` exists because the
   struct says `enum`. Five screens in the application are written by hand and
   each says why. The components are typed Go functions and the stylesheet is a
   Go value, so there is no CSS build and no framework. See `docs/adr/0007`.
  
9. **A client is configuration.** One image, one binary, `--role web|worker|all`;
   clients differ by configuration and assets, never by build.
10. **Claims need evidence.** The gates below check the implemented boundaries.
    A declaration, a passing compile and a product journey establish different
    things. Report executed results and timings; keep untested claims explicit.

## Layout

| Directory | Holds |
|---|---|
| `kit/` | the kernel: db, tenancy, config, problem, httpx, module, crud, rest, events, jobs, health, limit |
| `modules/` | business modules, each `contracts/` + `internal/` + `module.go` |
| `ui/` | typed components, the class builder and the CSS emitter, the browser controllers; `page` (documents as values) and `screens` (screens generated from schemas, and the resources catalog) |
| `design/` | design tokens and the two themes |
| `apps/` | `platformkit`, the reference binary |
| `tools/` | `locbudget`, the line-budget ratchet |
| `migrations/` | the foundation schema; modules may carry their own SQL |
| `deploy/` | Dockerfile, Postgres bootstrap |
| `docs/adr/` | the decisions, ten files at most |

## Limits

Ceilings, not baselines. The numbers live in `loc-budget.json` and
`packages-budget.json`, they are ratcheted at every tag, and the table below is
therefore a snapshot and not the source — a second copy goes stale the first
time somebody deletes something, which is what happened: this file published
15,000 / 150,000 / 40,000 while `loc-budget.json` shipped a fifth of that. Run
`make check-loc` for today's.

A ceiling is the count rounded up to the next hundred (`go run ./tools/locbudget
--write --round 100`). At exactly the count, the next one-line pull request is
over budget, which the release review found by writing one. Raising a ceiling is
an owner commit with a reason; a tag re-ratchets.

Two of the rows say something the file could not say by path alone. The
reference binary and the gates that guard it are budgeted apart, because a
ceiling shared between them is a ceiling neither owns. And a module's `*test`
package — its fake and its conformance suite, `tasktest` and the ones every
later module copies — is test support that happens to compile like production
code: no `_test.go` suffix, importable by any consumer. It is counted by the
package's own name (`dir_suffixes`) rather than by where it sits, so a fake is
never priced as if it were a feature of the module it stands in for.

At v1.0.0 they were:

| Bucket | Ceiling | Count at v1.0.0 |
|---|---|---|
| `kit/` (kernel) | 8,200 | 8,106 |
| `modules/` | 11,700 | 11,626 |
| `ui/` + `design/` | 9,600 | 9,514 |
| `apps/` | 500 | 492 |
| `tools/` | 400 | 303 |
| total production Go | 29,900 | 29,850 |
| test support Go (`*test` packages) | 5,600 | 5,530 |
| test Go | 18,200 | 18,167 |
| browser JavaScript | 300 | 218 |
| markdown | 1,600 | 1,525 |
| first-party packages linked into the app | 54 | 54 |

## The browser half

There is no CSS build, no node in the build, and no framework.

`ui/components` is the component library: `Button(ButtonProps)` and forty
others, each a Go function taking a props struct and returning HTML. Every one
of them declares the classes it can emit as a `ui/style.ClassList`, and
`ui/style` resolves exactly those classes to CSS rules against the custom
properties `design` renders. `ui.Stylesheet` composes the three once per
palette, at the first request, and serves the result from memory — so a
component that is deleted takes its CSS with it, and a class no component
declares has no rule. Both directions are tested: `ui/components` proves its
declarations resolve, `modules/admin` proves the shell declares nothing else.

`ui.Compose` is the whole stylesheet API: the kernel's lists plus a consumer's
own lists and rules, resolved together so a shared utility has one rule,
returned as a `Sheet` the consumer computes once at mount and carries. `ui/page`
is a document as a value — a `Chrome` every page of a shell shares, a `Request`
read once at the edge, a `View` a handler returns, a `Frame` that arranges them
— and `page.Serve` is the one adapter between a handler and the router.
`ui/screens` is the seven generated pages of a resource as pure renderers, for
any shell that mounts them, and `screens.Describe` is the same knowledge as JSON
at `/api/v1/admin/resources`, which is what a shell that is not a browser
generates its screens from. `modules/admin` and a client's storefront are
callers of these three and write no frame, head, stylesheet cache or class
string of their own.

There are two sheets, and the split is about what a page downloads rather than
about how the CSS is written. `ui.Stylesheet` is the tokens, the roles, the base
layer and the classes an application's own pages can emit;
`ui.GalleryStylesheet` is the difference — the rules for the components only
`/admin/_gallery` renders, which is the one page that links it.

A client's colours are one value. `design.Pair` is the two themes an
installation ships, `admin.Deps.Theme` carries it, and `design.Default()` is
what an application that says nothing gets. Nothing above the tokens changes
with it: a component names a role, a role is a custom property, and a palette
sets the property. There is no override layer, no second stylesheet and no
build step — which is what the role indirection was for.

The only third-party byte the browser runs is htmx, vendored and minified.
Beside it are four controllers of a few dozen lines each, and they are the four
interactions a server-rendered application cannot express: a theme that must
survive a reload, a validation error that must not cost a page, a destructive
action that must be confirmed, and a sign-in form that posts to a JSON route.

The theme is the only state the browser keeps, and a default is not state. A
page carries no `data-theme` attribute until somebody uses the toggle: the dark
rules are behind `prefers-color-scheme`, qualified by `:root:not([data-theme])`,
so an untouched installation follows the operating system and the palette it
ships. `theme.js` writes to storage on a click and at no other time.

Screens come from schemas, and so does everything a screen says about a value.
`kit/rest` carries the helpers a second HTML consumer would otherwise write
again: `Values` reads a submitted form through the schema, `FieldErrors` puts a
refusal on the control it is about, and `Display`, `Text` and `Humanize` are the
one answer to what a boolean, an instant and an enum look like — in a cell, in a
description list and in a select's options alike. `httpx.Resource` carries its
own authorization: the five closures ask the same Authorizer the routes do, so a
hand-written page cannot read past a permission by forgetting to.

### What a browser is told it may do

Every response the application makes carries `X-Frame-Options: DENY`,
`Referrer-Policy: strict-origin-when-cross-origin` and
`X-Content-Type-Options: nosniff`, and every HTML one carries a content security
policy: `default-src 'self'`, scripts from this origin and the request's own
nonce, images from this origin and `data:`, and `frame-ancestors 'none'`. They
are set by `kit/httpx` on the router that carries the static tree as well as the
API, so a stylesheet, a 404 from the router and a panic that never reached a
handler are covered by the same three lines — a reverse proxy that added them
would be a deployment topology this repository does not get to assume, and a
header a proxy adds is a header a request that reaches the pod directly does not
have.

The nonce is why there is a nonce. One inline script exists — the four lines in
`modules/admin` that apply the saved theme before the first paint, which cannot
be deferred without the page flashing white — and it carries
`httpx.NonceFrom(ctx)`. `style-src` is the one concession: `ui/components` emits
`style` attributes for a column's width and for an element that is hidden, a CSP
nonce cannot cover a style *attribute*, so that directive keeps
`'unsafe-inline'`. The exposure is CSS and not script, and it is written down
here rather than left to be discovered.

Uploaded bytes are the other half. `modules/file` serves a download as
`Content-Disposition: attachment` unless its stored media type is in a closed
render-safe set — PNG, JPEG, GIF, WebP, AVIF, PDF and plain text — and never for
HTML, SVG, XHTML or any XML dialect, because an uploaded page served inline is
stored cross-site scripting on the tenant's own origin. Both download routes
also send `Content-Security-Policy: default-src 'none'; sandbox` and
`Cross-Origin-Resource-Policy: same-site`, and a declared type in the renderable
set is checked at upload against `http.DetectContentType`.

## Gates

`make check` runs gates 1 to 9; `make e2e` is gate 10, which needs a browser and
a minute and so is a target of its own. CI runs both. The list stops at ten.

| # | Gate | Proves |
|---|---|---|
| 1 | `make build` | constructors and code type-check |
| 2 | `make vet` + `make fmt-check` | no known-bad and no unformatted Go |
| 3 | `make test` | the suite passes against a real Postgres |
| 4 | `make check-loc` | no bucket exceeds its ceiling |
| 5 | `make check-packages` | the app links no more first-party packages than `packages-budget.json` allows |
| 6 | `scripts/check_imports.sh` | a module imports only another module's `contracts/`, and `apps/` reaches into no module's `internal/` |
| 7 | boot validation test | no operation ships without an `Auth` declaration, and no route requires a permission no module defines |
| 8 | tenant isolation test + `make check-gucs` | RLS blocks cross-tenant reads as the app role, and only `kit/db` writes the tenancy settings |
| 9 | empty-database boot test | the app migrates and serves from nothing |
| 10 | `make e2e` | the admin shell renders and a generated CRUD screen works |
