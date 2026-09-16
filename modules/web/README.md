# Web module

`modules/web` is the public site: what an anonymous visitor sees at the root
of a tenant's host. It renders the [site](../site/README.md) settings and the
[content](../content/README.md) module's published pages through `ui/page` and
`ui/components`, with a bar where the admin has a sidebar, no controller of its
own and no data of its own — two public routes, `/` and `/{slug}`, and nothing
to declare. A tenant's theme and primary colour are pinned on the document,
and the prose sheet in [internal/style.go](internal/style.go) styles rendered
Markdown in the same `ui/style` steps the components use.

Compose it in the reference application with `web.Deps{Site, Content, Theme}`
and leave it out of an application that has a storefront of its own: the root
belongs to one module, and two claiming it fail at boot. It is the smallest
complete shell to copy when writing your own.
`make test TEST_PACKAGES=./modules/web/...` needs the development database.
