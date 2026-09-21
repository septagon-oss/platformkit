# Site module

`modules/site` owns the data a tenant's public site is made of — title,
tagline, navigation, theme, primary colour, logo — and none of the rendering.
The settings are a singleton, so the routes at `/api/v1/site/settings` are a
read and a PUT guarded by `site:manage`, with the admin entry at
`/app/site/settings`; `contracts.Public` is the projection a theme reads.
There is no job and no HTML, which is what lets the [web module](../web/README.md)
or a product's own theme be replaced independently.

Compose it with `site.Deps{}`. Consumers import [contracts/](contracts/) and
its [fake](contracts/sitetest/), never `internal/`. Navigation paths must be
local (`httpx.LocalPath`), which the contract validates before a save.
