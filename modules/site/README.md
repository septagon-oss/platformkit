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

## The accent is measured, not typed-and-hoped

`Validate` refuses a `primaryColor` that reads under `MinAccentRatio` (3:1) against
*both* kit canvases — `CanvasLight` and `CanvasDark`, which are `design`'s
`SurfaceCanvas` values, pinned to it by `TestTheCanvasesAreTheKitsOwn` rather than
imported, because `contracts/` names no theme. The ratios are the WCAG 2.x ones, and
3:1 is this repository's published target for non-text contrast: 4.5 would refuse
every colour installed before the band existed, and so every tenant that never chose
one. It rejects and never adjusts, so the stored row, the served value and the
trail's diff stay one value; an older out-of-band row still reads and still renders
(`Settings` answers what is stored, and `AccentRatios` answers how it reads). One
rule, in `Validate`, so the SQL service, the change apply and `sitetest`'s fake
cannot disagree.

The rule measures a colour a person typed. `DefaultPrimaryColor` is the empty colour
— not a colour but the absence of one — and nothing fills a value in on write, so the
band never measures the installation's own accent, which is not typed and is already
what the linked stylesheet paints per mode.

## A site that names no accent gets the installation's

`Palette` is the port for that: one method, `Accent(mode)`, answering `#rrggbb` for
`light` or `dark`. The module asks and the composition answers — `modules/site/provider.go`
reads the pair the application named with `pkit.App.Theme(pair)` off `pkit.Wiring.Skin()`,
the seam `modules/web/provider.go` already reads for the stylesheet it composes — for
the same reason `WriteGate` is asked here and answered there: which colours this
installation's chrome is drawn with is a fact about the deployment, and a module that
knew would be a module with a customer in it.

Resolution happens on the way out and never on the way in:

- `Settings` and `SettingsForUpdate` answer `SiteSettings.Resolve` — the stored colour
  when there is one, the palette's for the row's mode when there is not, and the empty
  colour when the installation named no palette either. A `system` row resolves against
  the light accent, the pair's first; a `system` *page* is answered by the browser's own
  preference, because the shell carries both.
- `Save` clears a colour equal to either of the installation's two accents back to the
  empty one (`ClearInstallationAccent`) before `Validate` and before the diff, so the
  form that submits what the read gave it writes nothing, publishes nothing and leaves
  the revision where it was. Compared against both modes, or a form could not change
  its theme: the accent it echoes is the one for the mode it is leaving.
- The event and the trail speak stored values. `"" → "#b45309"` is a change somebody
  made; `"" → "#0f5d4e"` is nobody's edit, and a trail row saying so would be a fact
  that did not happen.

So the row follows the palette rather than recording it: an installation that moves its
accent on a Tuesday is drawing its tenants' unchosen sites in the new colour on that
same Tuesday. `TestSavingTheReadsAnswerStoresTheAbsenceOfAColour` pins what the column
holds; `char(7)` is blank-padded, which is why `Service.stored` trims what it reads.

## Composition

**Reused** — `SiteSettings`, its `Validate`, `hexColor` and `DefaultPrimaryColor`;
`rest.Singleton`'s public door; `design.Light()/Dark().SurfaceCanvas`; `sitetest`'s
conformance harness, whose fake shares the rule by calling `Validate`; the `WriteGate`
shape for a question the module asks and the installation answers; `pkit.Wiring.Skin()`,
which `modules/web/provider.go` already read for the same fact.
**Added** — `CanvasLight`, `CanvasDark`, `MinAccentRatio`, `ContrastRatio`,
`AccentRatios` and the clause in `Validate`: no package here computed a contrast
ratio, so nothing existing carried the judgement. And `Palette`, `PaletteFunc`,
`Accent`, `Resolve`, `ClearInstallationAccent` and migration `000054`, new because the
row could not hold "nobody chose": `Validate` filled a constant in on every write
door, so no read could distinguish an unchosen accent from the colour installed in
2024, and the module had no line through which the application's palette could reach
it.
**Made reusable** — `ContrastRatio(a, b)` and `AccentRatios`, exported from
`contracts/`, which any future theme check can call instead of re-deriving
luminance a second time; `Palette` itself, the second port this module asks and its
composition answers, so a third (the palette's hover colour, say) is a method and not
a new module; `contracts.Accent`, which is the whole resolution rule in one function
the fake, the SQL service and the paint's tests all read the same way; and the pattern
of `TestTheCanvasesAreTheKitsOwn`, which pins a duplicated theme value to its owner
rather than to a comment. The test itself lives in the module's external test package
and nothing can import it — the shape is what is reusable, not the function.
