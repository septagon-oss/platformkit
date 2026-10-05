# Tenant component galleries

## Composition (T-0196)

**Reused** — `rest.Spec`, `rest.Singleton`, `httpx.Resource`, `ui/screens`, `ui/resource`, and the composed x/text catalogue provide the named resource screens and dashboard counts.
**Added** — declared singular and plural resource names and a source plural check because the old machine entity names could not express “Settings” or Portuguese plurals.
**Made reusable** — the `rest.Name` declaration, `resource.<module>.<entity>` catalogue pattern, and `tools/checkplurals` check are available to the next module.

The dashboard takes the request's catalogue selection, as do generated resource
names. A module owns its English and Portuguese name forms; applications may
compose further locale files through the existing x/text source list.

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

## The shell's own copy, and the two language controls

Everything the shell itself writes — sidebar, header, theme toggle, the confirm
dialog, the account form — goes through `chrome(r, key, fallback)` in
`modules/admin/internal/mount.go` and `account.go`, with the keys in
`modules/admin/messages/en.json` and `pt-PT.json`. The hand-written pages take the
same route: `roles.go` says `admin.roles.*`, the tenant switcher in `pages.go` says
`admin.tenants.*`, and the gallery's own heading says `admin.gallery.title`. A
literal that skips it is a sentence an implementation of a different language can
never say, on a page that will still declare the language it was asked in. Where a
nav entry names one of these two pages this shell wrote, the entry's own module is
asked first and this shell's title for the page answers when nobody did — see
`navLabel`.

Two things the shell draws but does not write are asked of that catalogue too. The
count on a dashboard card goes through `admin.dashboard.count_number` rather than
`strconv.FormatInt`, so the reader's own formatter groups the number — twelve thousand
three hundred and forty-five is `12,345` in English and `12\u00a0345` in Portuguese, and
a card that concatenated `12345` onto a translated noun said the noun in one language
and the number in none. And every word of a generated command control — the button,
the form's accessible name, each argument's label — is asked of the page's catalogue by
`ui/resource` under `command.<module>.<entity>.<verb>`; the module's own `Summary` and
`Description` are the fallbacks, which is what keeps an untranslated composition saying
something rather than nothing. See `ui/page/README.md` for both prefixes and the
refusal heading, `fault.title.<status>`, that goes with them.

### Interface

- `admin.Deps.Language` (`internal.AccountLanguage`) is the account command:
  `Preferred` reads the person's own choice and `Choose` writes it, both on the
  request's transaction. The header renders a `components.Select` named `locale`
  posting to `/app/admin/language` — the shell's own path, so no module owns a route
  on this page — offering the languages the request's tenant declared, its default
  first, and an empty option that takes the choice back. `Deps.Language` is nil
  draws no control: a composition with nobody to ask renders no promise.
- `admin.Deps.LanguageVersion` (`internal.AccountLanguageVersion`) is the same door
  counted. The control's select box is a picture of what the row said when the page
  was drawn, so the form carries a hidden `expectedRevision` beside it and the route
  writes through `ChooseAt`: a page left open while the person's other tab chose
  answers 409 in their own language and changes nothing, where an uncounted form
  would have written the older choice back over the newer one. A composition that
  wires no version here keeps the intent door it always had — the form is drawn
  without a revision — and the route refuses any form that names one, rather than
  guessing the revision a door that counts nothing was meant to compare.
- A body that is not a form at all — `locale=%ZZ` is the case the browser sends when
  it sends anything — is the shell's own refusal, drawn by the shell rather than
  handed to `page.Serve` as a problem. The reason is a language, not a status code: a
  problem carries its sentence as a Go string, and the shell that renders it on this
  route gave up the negotiation with it — the case review 8 caught was a `422` whose
  `lang`, heading, sidebar and way back were all English in front of a browser that had
  asked for Portuguese. The refusal now declares the language it actually speaks and
  says, in that language, that the form could not be read; `admin.account.language.unreadable`
  is the key, `INVALID_INPUT` the code, 422 the status. The tenant's language form makes
  the same refusal of its own body — an unreadable pair, or one carrying no revision —
  and answers 422 with its own page and `admin.languages.refused_form`, for the same
  reason and with the same effect.
- The tenant's own languages are `/app/tenant/languages` (GET and POST), offered in
  the sidebar to whoever `Authorizer` says holds `tenant:locale_manage` — the same
  question `Service.SetOwnLocale` asks inside its transaction, so the link and the
  door cannot disagree. The page offers the languages `admin.Deps.Installed` names,
  which is the set this installation's catalogues carry: a page offering a language
  no catalogue carries is a page that lies about its own copy.
- `<html>` carries `lang` **and** `dir`, both from the language the page is written
  in. The declaration is what a screen reader and a translation tool read.

### Limits

The pseudo-locale is `kit/locale/providers/xtext`'s `Pseudo`, not this module's: a
composition opts into `en-XA`, and the answer is these same keys with every letter
accented and every sentence bracketed. It transforms copy, so it cannot tell a word
the code supplied from one a person did — a tenant's own name, a role name and a
permission identifier arrive accented too, and those are data rather than copy. The
module writes no page of its own about content translation and knows nothing about
which content exists in which language.

The account control's empty option — the one that takes the choice back — carries this
module's words, `admin.account.unset`, and is selectable: `ui/components` disables an
empty option only when the field is `Required`, which is what "a prompt, not a value"
always meant, and no language chosen *is* a value here. The component still falls back
to its own "Choose…" for a caller that passes no `Placeholder`; no caller in this
repository does. `e2e/account-language.spec.ts` selects the empty option by value.

Because the account form is a `Form` with its own submit control, every signed-in page
carries two submit buttons: the page's own and `Save language`. They are distinct
accessible names — which is why the shell puts the noun in the label rather than
depending on where the button sits — and `e2e/admin-tasks.spec.ts` and
`e2e/design-audit.spec.ts` match the page's own `Save` with `exact: true` for the same
reason: a substring match would find the shell's control and fail strict mode.

### Verification

`modules/admin/internal/mount_test.go` and `modules/admin/module_test.go` cover the
shell copy and the routes. The composed cases are in `apps/platformkit`:
`admin_shell_chrome_language_test.go` refuses any English chrome on a `pt-PT` page,
`account_menu_language_test.go` that the account control offers only the tenant's
languages, `page_direction_test.go` that `<html>` declares the direction with the
language, `account_language_test.go` that a chosen language renders the admin and
survives signing out and back in, and `tenant_languages_page_test.go` that the
tenant's administrator reaches their own languages from the dashboard.

The three cases that came out of a reviewer's own probe sit beside the code they hold:
`modules/admin/generated_command_language_test.go`
(`TestGeneratedCommandsUseThePagesCatalogue`) walks a saved row's page under `en-XA` and
refuses any control word that is not the marker a catalogue carries turns it into;
`modules/admin/account_form_error_language_test.go`
(`TestAccountFormErrorsKeepPortugueseThroughoutThePage`) posts an unsupported language
and an unreadable body and reads the whole page back; and
`modules/admin/internal/dashboard_count_language_test.go`
(`TestDashboardNumbersUseTheReadersLocale`) counts 12345 in both languages, which the
0/1/2/5 counts this module had been checking could not tell apart.

`e2e/account-language.spec.ts` is the browser journey: an English browser, the account
control, `pt-PT`, and the dashboard that answers in Portuguese; the same browser signs
out, signs in and is still Portuguese; then the empty option takes the choice back. It
is the only case here that holds a real form, a real redirect and a real session, and
it runs inside `make e2e`.

The journey types none of that itself: it imports [../e2e/steps/admin.ts](../../e2e/steps/admin.ts),
which is this module's browser steps exported — `signIn`, `signOut`,
`chooseLanguage`, `clearLanguageChoice`, and the account control's locators. A spec
that retypes the sign-in fields re-decides, every time, what the email box is called,
and admin labels are catalogue copy now, so a rewording would break ten specs at once
and say nothing about the change behind it. A step takes the label it must click as an
argument rather than assuming English, because a step that clicks `Save language`
cannot be used by the Portuguese half of the journey, and the Portuguese half is the
point of the journeys that exist. `modules/admin/journey_steps_test.go` refuses the
journey that re-invents them.

The same rule runs the other way inside a page: when the Languages screen's save is
refused, the line above the form is this module's own sentence for the refusal —
`admin.languages.stale`, `.refused_unusable`, `.refused_tag`, `.refused` — named by
the refusal's stable code (`LANGUAGE_UNUSABLE`, `LANGUAGE_TAG`, `AUTH_DENIED`) and
the two revisions the request itself brought and re-read. What the command refused
with is still an English Go error string, and it still reaches the log, which is
where an English developer sentence belongs; a page that declares `pt-PT` and quotes
`crud: conflict: this declaration was read at revision 2` over its form has told the
reader one language and answered in another.

## Generated screens and a module's own pages

The shell is composed last. It generates a register (list, new, row, commands) for every resource a module
registered, at the address the kernel composes for it (`/app/<module>/<entity path>`). A module can write
its own workspace pages for a resource instead, such as a working desk with panels the generated form does
not have. It mounts at least one GET at that address or under it, and the shell then mounts none of the
generated screens for that resource and logs one line at boot saying so. The resource keeps its API routes
and its catalog entry. Every other resource keeps its register.

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
