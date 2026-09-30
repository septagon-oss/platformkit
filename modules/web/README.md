# Web module

`modules/web` is the public site: what an anonymous visitor sees at the root
of a tenant's host. It renders the [site](../site/README.md) settings and the
[content](../content/README.md) module's published pages through `ui/page` and
`ui/components`, with a bar where the admin has a sidebar, no controller of its
own and no data of its own — two public routes, `/` and `/{slug}`, and nothing
to declare. A tenant's theme and primary colour are pinned on the document, the
accent as an unlayered inline declaration — the one style written outside
`ui.Compose`, guarded to `#rrggbb`, and unlayered because a tenant's palette is
meant to outrank the layers — and the prose sheet in
[internal/style.go](internal/style.go) styles rendered Markdown in the same
`ui/style` steps the components use.

Compose it in the reference application with `web.Deps{Site, Content, Theme}`
and leave it out of an application that has a storefront of its own: the root
belongs to one module, and two claiming it fail at boot. It is the smallest
complete shell to copy when writing your own.
`make test TEST_PACKAGES=./modules/web/...` needs the development database.

## Authorization

### Permissions

The module declares none: `Permissions: nil` in `modules/web/module.go`. It has no nav entry and no generated screen. Its two routes are public and are not guarded by a permission.

### Object scope

None. `git grep` finds no `tenancy.Policy` use in `modules/web`. The pages come from the content module through `Site.Content.Public` (`modules/web/internal/mount.go`), which answers only published content.

### Duties the module enforces itself

- `Site.settings` in `modules/web/internal/mount.go` returns a 404 when the host resolves to no tenant (`tenancy.FromContext`), and a 503 when there is no database transaction.
- `Site.page` returns the same 404 for a slug that fails the `slug` pattern and for a slug the content module does not publish. Its comment says a draft, an archived page and an unused slug look the same.

The module writes no data, so it has no ownership rules.

### Public faces

Two routes, both declared with `httpx.Public()` in `Mount`: `GET /` (`web-home`) and `GET /{slug}` (`web-page`). They render the tenant's site settings (title, theme, primary colour, home slug), and the title and body of one published content page. They also link `SignInPath` and show the logo through `PublicFileURL`. There are no public writes, and the module uses no `kit/limit`.

### The operator boundary

None. The module declares no permission, so none is marked `Operator: true`, and it has no `OperatorRead` or `OperatorWrite` route.

### Provisioning

Nothing to grant, because the module holds no permission. A composition supplies `Deps.Site`, `Deps.Content`, `Deps.SignInPath` and `Deps.PublicFileURL`, and `Module` panics if any is missing. `Deps.Messages` is the one optional input, and like `admin.Deps.Messages` it opts the shell into translated copy: what arrives through it is the refusal a visitor meets (`ui/page/messages/pt-PT.json` answers the 404 of a slug nobody published), because the bar, the footer and the empty states are Go strings here and `Site.view` declares `en` over them whatever the negotiation picked. What visitors can see is decided by the content module's publishing state and the tenant's site settings, not by roles.
