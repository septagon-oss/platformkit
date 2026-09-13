# Administration

## Event delivery

`/admin/delivery` shows up to 50 oldest pending publications and 50 latest retained
terminal subscription failures for this installation. The `delivery:read` operator
permission protects both the direct route and its navigation entry. Customer
administrators cannot inspect another tenant's delivery records through this page.

The page reads the existing outbox and failure ledger through `events.InspectDelivery`
in a bounded system transaction. It shows event/tenant IDs, event and durable names,
recorded times and the observation time. Truncated lists say that more records exist;
there is no total-count scan. Payloads and raw failure causes remain outside the view.
Refresh reads the records again and changes no delivery state. Use the event ID and
durable name to investigate through your installation's existing operational tools.

Publication is transport acceptance, not proof that every subscriber completed.
Retained failures are historical records, not a replay queue. This page does not
establish broker connectivity, mail receipt, policy qualification or service health.

The reference application grants `delivery:read` when provisioning a new operator
role. Existing roles are preserved by `auth.SeedRoles`: an authorized operator must
add this grant through the existing Auth role-management path before the new link
appears. Do not rerun bootstrap or overwrite the existing role's other grants.
Compositions using another authorizer must explicitly grant this operator permission.

Run `go test ./kit/events ./modules/admin` with the contribution guide's disposable
database setup for metadata, truncation, cancellation and access tests. The existing
`e2e/admin-tasks.spec.ts` checks the freshly bootstrapped operator's keyboard journey.

## Tenant component galleries

The admin module serves the interactive gallery at `/admin/_gallery`. Sign in
with `gallery:read` (included by the administrator's wildcard). The default
composition shows Core examples only on the operator tenant. Customer tenants
receive 403 until the application supplies `admin.Deps.Storybook`.
The Go explorer works without a JavaScript build. To run actual Storybook.js,
follow the [Storybook adapter instructions](../../ui/storybook/README.md).

The application composition owns this selector. Read the tenant and principal
already established in `context.Context`; select examples from the same resolved
product/module composition used by the application. Return `ui.Storybook` with
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
