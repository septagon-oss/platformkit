# Storybook for Go components

This adapter runs Storybook.js with its HTML renderer. Stories and controls come
from the selected `ui.Export` snapshot; the canvas loads the existing authorized
Go preview. It does not reimplement components in JavaScript. Node 22.12+ and npm
are build prerequisites; the running application serves the finished files.

From the repository root, build Core's catalog into a new directory outside the
checkout (the builder refuses an existing output directory):

```sh
npm --prefix ui/storybook ci
go run ./tools/designexport > /tmp/platformkit-core.json
npm --prefix ui/storybook run build -- /tmp/platformkit-storybook < /tmp/platformkit-core.json
```

Set `server.storybook_dir: /tmp/platformkit-storybook` in the reference app's
local configuration, then start or restart that app using that configuration.
Sign in on the operator tenant and open `/admin/_gallery/storybook/index.html`,
or follow **Open Storybook** in the gallery. Building files alone publishes nothing.
Keep build directories immutable; select a new directory after rebuilding.

For products, export exactly the `Theme`, `Examples` and `Extra` returned by the
existing `admin.Deps.Storybook` selector. Build that snapshot, then return its
filesystem in the same `ui.Storybook.Files` value (for example, `os.DirFS(path)`).
Select the filesystem from the authenticated tenant's product composition, never
from a browser path or tenant query parameter. Identical compositions can reuse
one immutable build. Different content or themes require separate builds.

The admin module authorizes every file, including `index.json`, `iframe.html`,
bundles and private assets. It verifies `platformkit.json` against a fresh export
of the selected composition and returns 503 for a stale or mismatched build.
This manifest detects configuration mistakes; it is not a signature for untrusted
builds. Supply trusted build output, never a directory customers can modify, and
never publish a union of tenant stories or mount these files as public static assets.

Controls patch only changed fields through Go's typed validation. Resetting a
control restores the captured source value. Integers outside JavaScript's safe
range use exact JSON text controls. The theme toolbar controls the Go preview;
Storybook starts at the desktop viewport; its [viewport tool](https://storybook.js.org/docs/essentials/viewport)
lets you switch to mobile, tablet or a responsive canvas. Admin Sidebar stories
explain when a narrow viewport hides them. The SkipLink story includes a focus
button, sample navigation and a working content destination for keyboard testing.
Forms and network examples
remain isolated by the preview's HTTP sandbox. Embed private specimen images/fonts
as data URLs; the sandbox must not depend on cookies for subresource requests.
Use application journeys for network behavior and accessibility checks inside
the specimen; addons that inspect only the outer iframe cannot certify it.

`make e2e` builds Storybook into its temporary fixture, tests real controls and
confirmation, and removes the fixture afterward. `make check` covers two-tenant
file isolation, permission denial, path traversal and mismatched build refusal.
