# Tenant component galleries

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
sidebar entry, because `kit/module.Validate` refuses a nav entry that names no permission.

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

The module does not grant anything itself. `gallery:read` is held by any role that names it, or by the `*` wildcard that `SeedRoles` in `modules/auth/internal/seed.go` gives the built-in `admin` role. A composition grants it by editing a role on the roles page or through `PUT /api/v1/auth/roles/{name}`, or by passing default roles to `auth.SeedRoles` (called from `seedRoles` in `apps/platformkit/modules.go`). A role in a client's `client.yaml` is not shown by the code read for this section. The operator tenant needs `Deps.Storybook` or `tenant.Operator` to serve any gallery content.
