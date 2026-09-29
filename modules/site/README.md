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

## Authorization

### Permissions

The manifest in `modules/site/module.go` (`permissions`) declares one key, `site:manage`, defined in `modules/site/contracts/permissions.go`.
It is both the `Read` and the `Write` permission of the `rest.Singleton` in `Module`, so it guards the settings read and the `PUT` on `/settings`.
It also guards the "Site" nav entry and the generated screen `site/settings`.
The public face is not guarded by it.

### Object scope

None. A search of the module finds no call to `tenancy.Policy` and no `Resource.Kind`.
There is one settings row per tenant, and the tenant comes from the request's host and row-level security (`Deps` in `modules/site/module.go`).

### Duties the module enforces itself

None found beyond validation. `SiteSettings` validation in `modules/site/contracts/site.go` requires navigation paths to be local (`httpx.LocalPath`) and bounds their length.
The code shows no ownership or separation-of-duties refusal.

### Public faces

`Public: true` on the singleton in `Module` mounts `GET /settings` on the public surface (`httpx.Public()` in `kit/rest/singleton.go`).
Its `Face` returns `contracts.Public`: `title`, `nav` and `theme` only. The home slug, logo and timestamps are not exposed.
A host with no tenant is a 404. The route is a read; the module has no public write, so `kit/limit` is not applied.

### The operator boundary

None. `site:manage` does not set `Operator: true`, and the module registers no `OperatorRead` or `OperatorWrite` route.

### Provisioning

The code names no role. A composition grants `site:manage` to the people who configure the tenant's site.
Roles are granted by permission key through the roles API (`PUT /api/v1/auth/roles/{name}`, cited in `modules/user/contracts/administration.go`) or in the composition's own role definitions.
This repository does not show a client `client.yaml` for this module.
