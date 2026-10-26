# Administration workspace

This module owns the shared web workspace, access pages and operator navigation.
Applications supply modules, permissions and resource presentation at composition;
the shell draws only destinations the current tenant and role may open.

## Find the right experience

| Experience | Starting point and next action |
|---|---|
| Sign in and recover access | The access page names the workspace and keeps its form and recovery links together. Password, passkey and email-link controllers retain their existing routes. |
| Tenant administration | Dashboard names the tenant and groups available tools. Counts describe that workspace; they are not installation-wide analytics. |
| Platform operations | Dashboard prioritizes the tenant directory. The operator label stays in the header; open a tenant's host to work in its separate scope. |
| People, work, content and billing | Resource lists use the owner's names, primary field, summaries, status and empty-state guidance. Open a named record before editing or running its authorized commands. |
| Roles | Read the access summary, expand the role, then save its permissions. The existing last-administrator refusal still protects access. |
| Personal sessions | Sessions appears when the composition supplies a session store. It lists the signed-in person's sessions, independently of administrative permissions. |
| Health and developer tools | Health reports observed checks. Component galleries remain subject to both permission and tenant composition. |

The same authorized navigation drives desktop and mobile. Platform operations,
workspace tools, system support, developer tools and personal access remain distinct.
Application-specific workspaces such as file management and review queues retain
their own routes and composition; this shell does not replace their workflows.

## Give a resource a useful reading

Declare `Present` on the existing resource specification. `Group` and `Icon` guide
workspace organization. `Singular`, `Plural` and `Description` name the experience;
`PrimaryField` names a record. A nonempty `SummaryFields` selects compact list
columns beside the primary, status and thumbnail fields. Hidden and detail-only fields stay
off the list. Without summaries the existing visible columns remain available.
`EmptyDescription` explains the next step without inventing a control or permission.
Declared strings use the existing `Words` translation seam, including form names.

The owners are [workspace composition](internal/workspace.go),
[resource rendering](../../ui/resource/reading.go) and the shared
[components](../../ui/components/README.md). To verify an integration, follow the
[browser setup](../../CONTRIBUTING.md#verify-at-the-relevant-boundary) and run the
admin workspace and role journeys against a disposable reference application.
These checks cover the shared flows, not every product's custom workspace.

## Tenant component galleries

The admin module serves the interactive gallery at `/app/admin/_gallery`. Sign in
with `gallery:read` (included by the administrator's wildcard). The default
composition shows Core examples only on the operator tenant. Customer tenants
receive 403 until the application supplies `admin.Deps.Storybook`.
The Go explorer works without a JavaScript build. To run actual Storybook.js,
follow the [Storybook adapter instructions](../../ui/storybook/README.md).

The application composition owns this selector. Read the tenant and principal
already established in `context.Context`; select examples from the same resolved
product/module composition used by the application. Return `export.Storybook` with
that composition's `Examples`, `Theme` and stylesheet `Extra` values. Reuse an
existing product `Design()` contribution rather than maintaining another registry.
Return a `problem.Problem` with status 403 for a tenant or caller without access.
An empty example collection remains empty, including for the operator.

Never choose a tenant from a query parameter, accept arbitrary example definitions
from the browser, or return the union of every tenant's examples and hide links.
The selector is authoritative for the index, `/preview`, `/export`, navigation
and every file under `/storybook/`. Its optional `Files` contains an immutable
Storybook.js build for exactly that composition; mismatched builds are refused.
Direct requests for unpublished example IDs return 404. The endpoints require
the gallery permission before selecting content; responses containing the selected
composition are not cached. Keep private assets in application routes with the
same authorization or embed them in the examples. The shared public asset tree
contains the installation stylesheet and framework scripts, so tenant-specific
assets and CSS must not be placed there.

Choose an example and change its typed controls. The preview updates automatically
after typing pauses or a selection changes, keeping focus in the controls. Invalid
input preserves the last valid preview; **Apply preview** retries an update. The
server patches the captured Go invocation; property types, declared allowed values
and documentation come from its Props. **Go properties** copies the current Go
declaration. Pass it to the component constructor, adding documented slots in
trusted Go composition. Slot callbacks and arbitrary markup are not browser inputs.

The isolated preview supports keyboard interaction, native modal focus, light/dark
themes and 320/768/1280px viewports. Its HTTP content security policy also sandboxes
the direct preview URL: forms, network requests, storage and access to the parent
document are blocked. Lazy-loaded examples therefore require application journey
tests for their network behavior. Narrow preview widths scroll inside the gallery.

In an application, load a deferred modal panel into its existing root with
`HTMXProps{Get: panelPath, Target: "#server-modal", Swap: "innerHTML"}` on the
trigger. Set the root's `ID` to `server-modal`, `Deferred: true` and
`OpenOnSwap: true`. The root opens after the swap settles and clears on close.
The explicit swap keeps the dialog root; the application otherwise defaults to
replacing the target element. Give multiple tabs or dialogs distinct IDs.
`ModalForm` closes its owning dialog after a successful 2xx response, including
when the response replaces the form. Validation and failed requests keep it open;
a delayed response from an earlier opening cannot close a reopened dialog.

`Section`, `SectionHeader` and `Hero` compose the existing components. `Heading`
separates semantic `Level` from visual `Size`; `Grid` accepts `SM`, `MD` and `LG`
column overrides. `design.Theme.Shape` controls button, card and modal radii beside
the existing colors and typography; the same values appear in CSS and design exports.

For local verification, follow [the contribution guide](../../CONTRIBUTING.md).
`make check` covers tenant isolation and exported contracts; `make e2e` covers
gallery controls, responsive layout, actual widget interactions and selected axe
checks in both themes. These checks do not establish native editor fidelity or
replace a screen-reader review. New product integrations must supply their selector
and repeat these checks against their own examples and private asset routes.

## Generated screens and a module's own pages

The shell is composed last. It generates a register (list, new, row, commands) for every resource a module
registered, at the address the kernel composes for it (`/app/<module>/<entity path>`). A module can write
its own workspace pages for a resource instead, such as a working desk with panels the generated form does
not have. It mounts at least one GET at that address or under it, and the shell then mounts none of the
generated screens for that resource and logs one line at boot saying so. The resource keeps its API routes
and its catalog entry. Every other resource keeps its register.

Beside the generated register this module draws seven pages itself: the dashboard and health page, the
tenant switcher, the roles workspace, the gallery and its storybook file route, and the sessions screen.
The last is `mountSessions` in `modules/admin/internal/sessions.go`: a table of the caller's own sessions
at `/app/auth/sessions` with two forms that end one and end every other, guarded by `httpx.SignedIn()`
with no permission, because these are the caller's own rows. It renders through modules/auth's commands
(`Deps.Sessions`, declared in `modules/admin/module.go`) and is mounted only when a composition supplies
them — a composition without a session store mounts no screen and answers 404, rather than a page with
nothing in it. The screen's own words and why a ref never appears as row text are modules/auth's
(`modules/auth/README.md`); this module owns only that it is drawn inside the shell's chrome. It carries no
module navigation declaration; the shell adds it under Your account when the session store is supplied.

## Authorization

### Permissions

The manifest in `modules/admin/module.go` declares one permission, `gallery:read` (`PermissionGalleryRead`). It guards the gallery page `admin-gallery`, the `admin-gallery-preview` and `admin-gallery-export` routes (`mountGallery` in `modules/admin/internal/gallery.go`) and the private storybook file route `admin-storybook-file` (`mountStorybook` in `modules/admin/internal/storybook.go`). The sidebar link to the gallery is shown only when the authorizer allows `gallery:read` and `Shell.storybook` succeeds (`frame` in `modules/admin/internal/mount.go`). The other pages use permissions declared by other modules: the roles page uses `role:manage` (`httpx.Permission(authcontracts.PermissionRoleManage)` in `modules/admin/internal/roles.go`), and the tenant switcher uses `tenant:manage` through `httpx.OperatorPermission` (`modules/admin/internal/pages.go`). The dashboard (`admin-dashboard`) and health page (`admin-health`) are `httpx.SignedIn()` and name no permission.

### Object scope

None. The code searched shows no call to `tenancy.Policy` in this module. Access to the gallery is decided by `Shell.storybook` in `modules/admin/internal/gallery.go`, which calls the composition's `Deps.Storybook` callback for the tenant and principal in the context. With no callback, only a tenant with `tenant.Operator` set gets the built-in `examples.Gallery()`. Any other tenant gets 403.

### Duties the module enforces itself

The module writes no rows, so it has no ownership or separation-of-duties refusals. The one rule it enforces is fail-closed storybook selection: `Shell.storybook` refuses when there is no tenant in the context, and `mountStorybook` refuses a build whose `platformkit.json` does not match the selected book. The last administrator rule for roles is enforced by the auth module (`SetRole` in `modules/auth/internal/roles.go`), not here.

### Public faces

One route is public: the sign-in page `admin-login` (`httpx.Public()` in `modules/admin/internal/pages.go`). It serves a form that posts to the auth module's login route. It exposes no tenant data. The page does no writes and calls no `kit/limit` limiter. Rate limiting of the login itself belongs to the auth module (`Limiter` in `modules/auth/contracts/limiter.go`).

### The operator boundary

The module declares no permission with `Operator: true`. Its one manifest permission is `gallery:read`. It does mount one operator-only page: `admin-tenants` uses `httpx.OperatorPermission(tenantcontracts.PermissionTenantManage)` in `modules/admin/internal/pages.go`, and the tenant module owns that `tenant:manage` permission. The roles page also hides operator permissions from non-operator tenants (`offeredPermissions` in `modules/admin/internal/roles.go`).

### Provisioning

The module does not grant anything itself. `gallery:read` is held by any role that names it, or by the `*` wildcard that `SeedRoles` in `modules/auth/internal/seed.go` gives the built-in `admin` role. A composition grants it by editing a role on the roles page or through `PUT /api/v1/auth/roles/{name}`, or by passing default roles to `auth.SeedRoles` (called from `seedRoles` in `apps/platformkit/roles.go`). A role in a client's `client.yaml` is not shown by the code read for this section. The operator tenant needs `Deps.Storybook` or `tenant.Operator` to serve any gallery content.

## Composition

The provider requests Auth's `RoleEditor` and `SessionAdministration`, the kernel's
`httpx.Authorizer`, Tenant's `Lister`, and optional User `Lookup`. `Deps.TenantList`
takes precedence over deprecated `Deps.Tenants`; missing Lookup retains the anonymous
header label used for a signed-in caller whose name is unavailable.

**Reused** — existing role/session screens, navigation authorization and caller header
continue to render through their current shell ports.
**Added** — the `TenantList` compatibility input lets the switcher consume only `List`
while retaining its legacy `Tenants` field for callers.
**Made reusable** — the provider can build without any identity aggregate key.

The shell keeps workspace, caller and build labels inline; destination groups share
one column, and purpose sentences use the shared bounded measure. Navigation cards
stay unfilled in both themes so they remain quiet destinations. Generated forms
use the existing `MaxWSM` control column, and write screens appear only for a
declared guard, offered verb and executable closure; missing new/edit screens
answer 404 while a mounted form still enforces its caller's grants.

**Reused** — `Text`, `ChromeContext`, `Sidebar`, `Stack`, `Grid`, `Card`,
`FormSentence`, `forms.MustExample`, `MaxWSM`, the guarded `httpx.Resource` and
`page.Serve` carry the presentation and route rules.
**Added** — one canonical test-only [floor instrument](../../ui/testdata/design_floor.mjs)
and a [served reference-app assertion](../../apps/platformkit/shell_design_floor_test.go),
because the existing browser helpers did not enforce all nine rules under `make check`.
**Made reusable** — the same floor oracle now serves the Go gate and existing
Playwright journeys, including stylesheet checks, chrome diagnostics and form measures.


**Reused** — the shell's `components.Shell`, `Sidebar`, `SectionHeader`, `FormSentence`
and `Badge`; the `Present` reading each resource already declares (`entity.EntryHints`)
and the `Words` translation seam; `ui/screens`' generated register; and this module's
existing dashboard, health, roles and sessions pages. **Added** — workspace sections
(`workspaceSections` and `workspacePurpose` in `internal/workspace.go`), which group
only what `Navigation.Visible` admitted and say what each destination is for; a
dashboard count that fails leaves its destination without a figure, and neither the
home nor the health page reports a check that never ran as passing; one disclosure
per role on the roles page; and the declared reading in `ui/resource/reading.go`,
which picks the field that names a row and the columns a list shows. Before this,
the admin's navigation ignored a resource's declared `Group`, and the web's resource
screens did not read `SummaryFields`. **Made reusable** — `components.Disclosure`,
`components.Standalone` for access pages, and the declared reading: a module that
declares `PrimaryField`, `SummaryFields`, `Description` and `EmptyDescription` gets
its list, record and empty state read that way without a page of its own.

The home's primary action and the footer's "About this workspace": **Reused** —
`components.Disclosure`, `Text`, `Button` and `Stack`; `page.NewNavigation`,
`Visible` and `Unserved`, so the action is filtered and reported exactly as a nav
entry is; `workspacePurpose` for its sentence; `module.Grants` and kit/module's
nav-entry refusal wording; `pkit.Optional`, `Provides` and `Put`. **Added** —
`admincontracts.PrimaryAction`, a defined type over `module.NavEntry`, because no
module or application could say what its people come to the workspace to do: a
flag on `NavEntry` would let many modules claim the home at once, and claiming
`Router.Home()` moves the dashboard to another route. **Made reusable** — the port
itself: an application that `Provides[admincontracts.PrimaryAction]` gets its action
first on the home, for whoever may follow it, with no page of its own.

## Limits

- The home's primary action is one per composition; its words are the product's,
  printed as written like a nav label. A product summary beyond the action's purpose
  sentence is not declared here: a product that wants a page of its own at `/app`
  claims `Router.Home()`.
- "About this workspace" holds the host's scope; the build revision stays in view beside it, which the
  shared design floor requires. The account
  id is not shown there (support reads it on the person's record), and whether the
  tenant's slug joins it is undecided.

- The dashboard intro is a `SectionHeader` title over a `FormSentence`, because a
  `SectionHeader` description has no reading measure. Giving it one is a change to
  the shared component and its design export.
- The roles page's summaries and the tenant list's subtitle are written in English
  and are not catalogued, so a Portuguese tenant reads them in English.
- On the web a row is named by its `ui:"display"` field when that field is shown and
  filled, then by `PrimaryField`, then by the first named field with a value. A
  catalogue reader names it by `PrimaryField` alone, which must therefore be a field
  every row has: people are named by their address.
