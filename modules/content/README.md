# Content module

`modules/content` is the pages and posts a tenant's public site is made of: a
`rest.Spec` at `/api/v1/content/contents` with the draft, published and
archived lifecycle and its events, a public read at
`/api/v1/public/content/contents` (one published row per `{slug}`) that answers only for published rows, and the
Markdown renderer `contracts.Render` the [web module](../web/README.md) uses.
`content:read` and `content:manage` guard the routes and the admin entry at
`/app/content/contents`. There are no versions by design; see
[module.go](module.go).

Compose it with `content.Deps{}`; it needs nothing from other modules.
Consumers import [contracts/](contracts/) and its [fake](contracts/contenttest/),
never `internal/`. `make test TEST_PACKAGES=./modules/content/...` needs the
development database.
