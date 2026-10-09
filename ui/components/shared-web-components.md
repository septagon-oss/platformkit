# Shared web components specification

Status: specified on 2026-09-29; all twelve families have executable typed
constructors and Gallery examples as of 2026-09-30. This document retains the
intended acceptance contract. The [component guide](README.md) describes the
implemented API and its Limits; source Props are authoritative for field names.
The phase report records actual checks, failures and unverified product/manual
acceptance. A named case below is not itself evidence that the case ran.

## Owner and consumer

Extend [components](README.md), its [Props](props.go), [class lists](classlists.go),
[shared controllers](../assets/js/components.js) and explicit
[Gallery](examples/gallery.go). Atomic categories describe compositions within
that owner; they do not create four new Go package trees. Themes remain
[design.Pair](../../design/design.go), projected by [ui/style](../style/theme.go)
and [ui.Compose](../ui.go). No new module, registry, router, database, event bus,
reflection discovery or `init` hook is part of this delivery.

The traced consumer at `ba21b010357a4a07898bcd175bae116e96378f74` is
[Task's rest.Spec](../../modules/task/module.go) →
[guarded resource reads](../../kit/rest/screens.go) →
[generated screens](../screens/screens.go) →
[resource.List](../resource/resource.go) → [TableWithSlots](table.go).
Admin's [authorized gallery](../../modules/admin/internal/gallery.go) is the
existing catalogue consumer. The trace's focused tests passed before this phase.
Table already owns rich cells and sorting links, but its selection hooks lack a
controller; Modal already owns overlays, focus and bottom sheets, but not a side
panel. These are extensions, not reasons to replace either owner.

Presentation receives already authorized values. Generated entity screens still
come from `rest.Spec`. A capability renders its own custom page only within its
`internal/ui` layer. Components import no module, SDK, HTTP server or database;
the import closures in [ui/README.md](../README.md#dependency-rules) still apply.
There is no task-named `contracts/` package to populate with HTML types.

## Landing order and reuse ledger

Each numbered row is a separately reviewable slice. Finish its applicable state,
locale, theme, accessibility and verification matrix before starting the next.
Every deliverable has its decision-0022 composition line here.

| Slice / deliverable | Atomic category | Reuse decision |
| --- | --- | --- |
| 1. EmptyState, Notice, Skeleton and recovery compositions | atoms/molecules | **Composed from** EmptyStateWithSlots, AlertWithSlots, Skeleton/TableSkeleton, MediaStatus and document.RequestNoticeExamples; Notice delegates to Alert. |
| 2. DataList, filter/sort chips, saved-view navigation, bulk selection | organism | **Composed from** TableWithSlots, Checkbox/Input/Select, Badge, Tabs, Pagination and Section; extend Table selection instead of adding another table. |
| 3. DetailSheet and SidePanel with sticky actions | organisms | **Composed from** ModalWithSlots/ModalPanelWithSlots, their shared controller, DetailList and Card slots. |
| 4. Timeline, including audit display | organism | **Composed from** Avatar, Badge, DetailList, Section and the DataList/Table composition from slice 2. |
| 5. Stepper | organism | **Composed from** Button/Link, Section, Stack/Flex and the existing forms composition; the trace found no stepper. |
| 6. SlotPicker and DateStrip | molecules/organism | **Composed from** native Input/Select/Button/Badge and layout; new availability presentation because the existing datetime control does not express slots or capacity. |
| 7. Calendar (day, week, agenda) | organism | **Composed from** DateStrip, DataList and controls; **new**, because the inventory found no calendar engine, using FullCalendar for the enhanced time grid. |
| 8. ProductCard, OptionChips, QuantityInput, BuyBar, Cart, OrderSummary | molecules/organisms | **Composed from** Card, Media, Button, Input/Select, Badge, Table/DetailList and layout; no new checkout capability. |
| 9. PricingTiers and PlanComparison | organisms | **Composed from** slice 8 price values, Card, Badge, Button, Table and layout. |
| 10. MapView with selection and list/map modes | organism | **Composed from** DataList, DetailSheet and controls; **new**, because the inventory found no map engine, using Leaflet. |
| 11. Hero full-bleed variant, PhotoGallery/viewer, Masonry | molecules/organisms | **Composed from** Media, Hero, Grid, Card and Modal; retain one image renderer and one overlay controller. |
| 12. Sparkline, AreaChart, BarChart, StatTile | atoms/molecules/organisms | **Composed from** text, layout, Table, Badge, controls and theme tokens; **new** bounded SVG geometry because no chart unit was found. |
| Generated-screen adoption | templates | **Composed from** resource.List/Detail/Form, screens.Mount and page.Shell/document.Document; no alternate screen generator. |
| Gallery and visual fixtures | examples | **Composed from** ExampleOf/ExampleWithSlots/ExampleWithChildren, Gallery and mediaSpecimen; no second catalogue or screenshot-derived markup. |
| Browser conformance | journeys | **Composed from** e2e/ui-components.spec.ts's specimen() and e2e/design-audit.spec.ts; use actual Go exports, not a private rendering harness. |
| Domain fixture, only if needed by a consumer test | test double/data | **Composed from** tasktest.NewFake, tasktest.RunService/Fixture and dbtest.Schema; pure presentation cases use values and the real renderer. |
| Localization | copy | **Composed from** locale.Messages/Formatter/Locale, page.Shell and xtext.FromCatalog; document recovery copy keeps its existing owner. |
| Specification and verification report | documentation | **Composed from** the presentation map, component guide and CONTRIBUTING consumer-trace/check conventions. |

Timeline follows the first three essentials because lists and details consume
it. Pricing comparison follows commerce because it shares price display. Neither
of those two brief rows is dropped. No product-specific data enters the kernel.

## Common API and refusal contract

Names below are proposed exported constructor/Props names. Constructors keep the
existing `Foo(FooProps) → gomponents.Node` shape. Named `FooSlots` are trusted Go
nodes/callbacks; portable Props contain values, never executable HTML or JavaScript.
Lower-camel names are the corresponding portable/mobile names. `?` means optional;
all other fields are required when their enclosing state is rendered. Slices and
their elements are borrowed immutable inputs; rendering never sorts them in place.
An unqualified aggregate `state` below is ContentState; child enums such as
Step.state retain their explicitly listed values. Disabled/Hidden come from
ComponentProps; pending means a supplied request is outstanding, not a timer.

Every new component embeds ComponentProps. ID identifies an instance in one
document; item `key`/`id` identifies supplied data within that instance. These are
not credentials, tenant selectors or database relationships. Empty or duplicate
item identities, missing required labels, unknown behavioral enums and invalid
numeric/range inputs are **correctable** composition refusals (C1 below).
Unknown style-only size/variant values retain existing default behavior. Existing
constructors' zero values and legacy default labels remain compatible.

New aggregate Props expose `Validate() error`. The constructor uses that same
validation decision before writing any component bytes; `Node.Render` returns the
error on malformed composition. [document.Render/RenderFragment](../document/render.go)
already discard buffered bytes on error. Validation returns developer errors,
not English UI copy or an invented HTTP status. The caller translates expected
domain refusals into the states below before invoking a renderer. A composition
error must not silently make missing data look empty or make a bad total buyable.

Shared value contracts, owned beside Props and used only where listed:

| Value | Fields and decisions |
| --- | --- |
| ContentState | `status: MediaStatus` (empty string means ready), `title`, `text`, `loadingLabel`; `offline: bool`, `offlineText`. Title/text label the currently absent state; loadingLabel is required for loading. Offline is additional information for ready or failed only, never authorization. |
| ContentState slots | `EmptyAction`, `RetryAction: []Node`. EmptyAction appears only for empty; RetryAction only for failed. Refused never offers an automatic retry. Ready/offline may offer an explicit reload through the component's ordinary action slot. |
| ChoiceLink | `key`, `label`, `href`, `selected`, `disabled`; `removeHref?`, `removeLabel?` together for a removable filter. It is a real navigation link; no route is synthesized from a key. |
| Money | `minor: int64`, `currency: Currency` (named uppercase three-letter code string). Amounts never use floats. Formatting is supplied separately as `text`; a zero amount is a known amount, absent money is unknown. No default currency, scale or conversion. |
| MoneyText | `money: Money`, `text: string`, `accessibleText: string`; formatted from that exact amount/currency by the caller. Only `money` participates in arithmetic. |
| TimeText | `atUTC: time.Time`, `text: string`; UTC is machine time, localized text is display. Date-only values are `YYYY-MM-DD` civil dates with a separate IANA zone and are not midnight UTC timestamps. |

Disabled navigation choices render labeled non-links with an unavailable reason
when supplied; they cannot navigate through mouse, keyboard or enhancement.
An enabled ChoiceLink or DateChoice requires its supplied href. No URL is built
from an opaque ID. Selection/current semantics use the native link's aria-current.

Ready renders supplied data; loading renders matching skeleton geometry and
`aria-busy`; empty renders EmptyState; failed renders Notice with retry only when
supplied; refused renders a non-urgent Notice without previously loaded content.
Empty is a successful read of no items. Failed/refused must remove stale rows,
selected detail, images, totals, point coordinates and interactive hidden payloads.
Failed/refused Props therefore contain no previous data payload; Validate returns
C1 **correctable** if it is retained, including in a captured example. The normal
refused notice is built from cleared values; an error returns no exported capture.
An explicit ready/offline state may retain a caller-approved snapshot and says
when it was read; it cannot silently queue a write or declare it successful.

There is no general action bus. In the component sections, **events** mean native
input/change/submit, ordinary navigation, or existing dialog close/cancel events.
Shared controllers consume them. No unused custom events are published. Web
actions compose ButtonProps and trusted form/action slots; HTMXProps enhance the
same URLs and forms. Every write retains its native POST form path; GET is read
only. Components neither invent routes nor replay requests after a reconnect.

| Refusal | Classification | Exact response/recovery |
| --- | --- | --- |
| C1 malformed Props, duplicate/empty key, unsafe behavioral combination, missing accessible copy | **Correctable** by the composing engineer | Validate and Render return an error; no component markup or partial computed result. Fix input and rerender. |
| C2 invalid choice/quantity/date input from the user | **Correctable** | Native constraint plus caller's field error on submit; retain entered values, focus the first invalid control; no command success state. |
| C3 unavailable/full/sold-out item, stale quote/revision or selection absent from refreshed results | **Correctable** by refreshing or choosing again | Show supplied reason; remove invalid selection; disable the unavailable choice; keep available alternatives. A server conflict returns no stale command row. |
| C4 provider/network/read failure, offline, canceled request, expired signed asset | **Correctable** when service/access can be restored | Failed state or explicit offline snapshot; manual read retry. An uncertain write retains its caller-owned command identity and cannot be declared failed or repeated with a new identity by UI. |
| I1 actor lacks current grant, cross-tenant identifier, concealed or deleted object | **Immutable** for this actor/tenant/object request | Do not render protected values or controls; use the existing handler's refusal/not-found policy. Reauthenticate/change tenant/access through a separate authorized flow, never retry the same denied mutation in a loop. |
| I2 caller reports a terminal business fact, such as an already finalized order that cannot be edited | **Immutable** for that command/object version | Read-only terminal presentation with reason. Components do not offer an edit merely because the row is visible. |

No controller gives C1/C2/C3 validity or disabled appearance authority over a
command. At the resource boundary, the service must recheck grants, tenant scope,
current state and expected revision in `db.Tx[db.Tenant]`, and atomically commit
state, audit and outbox. Generic CRUD currently locks live rows but does **not**
reject every stale form; this specification does not claim otherwise or enable
new bulk writes through that path. Products use a command that actually supplies
the required revision/idempotency contract.

## Component contracts

### 1. Rich states

`EmptyStateProps` retains Title/Description/Compact/Bordered and its IconStart and
Actions slots. Add canonical `Text` and `Action?: ButtonProps` for mobile parity:
Text takes precedence over legacy Description; Action delegates to Button. A new
Action and nonempty Actions slot together are C1 **correctable**, so an invocation
does not show duplicate primary actions. Old calls remain valid.

`Notice(NoticeProps)` and `NoticeWithSlots` are thin compositions of AlertWithSlots.
Props: `title?`, `text`, `tone: danger|warning|info|ok` (default danger, matching
the existing mobile Notice), `action?: ButtonProps`, `dismissible`, `dismissLabel`
when dismissible, `live: polite|assertive|off` (default polite). Slots: the existing
Alert IconStart/Actions seam with the same single-action rule. Map ok to Alert's
success tone. Add an optional Live override to Alert; absent preserves its current
danger/warning assertive semantics. Assertive is for an urgent, newly raised error,
not a whole initial page. No timer dismisses action or error notices.
Explicit polite/assertive/off map to status/alert/note respectively, with matching
aria-live; off is non-live information rather than an alert with contradictory ARIA.

Skeleton and TableSkeleton keep their APIs. ContentState owns their single loading
announcement; individual skeleton cells are decorative. Shapes match the eventual
row, image or form geometry, including dimensions supplied for media.

Events: action activation follows its link/form; dismiss hides only that notice,
and returns focus to the associated control if dismissal removed the focused
button. No persisted dismissal, connectivity detector, auto-retry or success event.
States: empty with/without action; skeleton; failed with retry/pending retry;
offline with/without safe snapshot; refused; success persistent/dismissible. Hover,
pressed, focus and disabled apply to the composed actions, not to inert copy.
References: R01 empty region; R03 success notice; R04 skeleton-shaped rows
(the source blurs data, so it is a geometry reference, not proof of loading).

### 2. DataList

`DataListProps`: `label`, `state: ContentState`, `columns: []TableColumn`,
`groups: []DataGroup`, `filters`, `sortChoices`, `views: []ChoiceLink`,
`resultKey`, `selectedIDs: []string`, `selectionName`, `formID`,
`selectAllLabel`, `clearSelectionLabel`, `selectionCountLabels`, `resultCountText`,
`compact`, `pagination: PaginationProps?`. Selection fields are required only
when row selection is enabled. `DataGroup`: `key`, `title`, `countText`, `rows`,
`collapsible`, `collapsed`. CountText is the server's localized count (possibly
including unloaded rows), not an inference from this page's length.
A single non-collapsible group with an empty title is the ungrouped form; multiple
groups or a collapsible group require titles and countText.
SelectionCountLabels is a localized string slice indexed by selected count, with
exactly one entry for each count from zero through this page's eligible-row count.
The composing locale formatter supplies plural forms; the controller selects an
entry without inventing a second interpolation engine. Other result sizes arrive
in a new server rendering with a new resultKey.
`DataRow`: existing `TableRow` plus `href?`, `selectable`, `selectionLabel`,
`revision?`, `statusLabel?`, `tone?`. SelectedIDs initializes checked state;
native checkboxes are thereafter the browser selection source. On a new resultKey,
the controller clears even IDs present in both snapshots.
IDs must be in the current rendered/selectable set (otherwise C3 **correctable**:
intersect with that set and announce cleared selection).

Slots: existing Table rich Cell/CellAttrs/RowAttrs/SortURL/SortState, plus
`Toolbar`, `RowActions(row)`, `BulkActions`, `EmptyAction`, `RetryAction`.
The implementation also supplies `Footer` for generated collection command forms
before the pager; these retain their own authority and are not bulk-selection actions.
An empty successful result retains supplied column headings and sort links; the
Table `Empty` slot composes its localized state without invoking row callbacks.
Use one Table owner per group, with a heading and count; no manually repeated
table HTML. Extend Table's selection slot contract to supply input name, form
association, value and disabled state. Native checkboxes submit selected IDs to
the composing form even without JavaScript. The caller's command adapter may
include hidden revision fields for every rendered row, but consumes revisions
only for submitted selected IDs; unchecked rows never become command targets.
Revisions are opaque caller tokens, never parsed or incremented by components.

Events: filter/sort/view/pager links navigate to supplied URLs; group disclosures
toggle locally; row links navigate to their detail page and may enhance to a
sheet; row action forms submit independently. Shared selection controller scopes
select-all to this instance and current page's eligible rows, including collapsed
groups, never unloaded results. It sets indeterminate for a proper subset,
updates the translated count, and clears selection on a new resultKey (query,
filter, sort, page, tenant or refreshed snapshot). It does not build query-wide
bulk commands. Without JS, individual checkboxes and POST work; the select-all
enhancement is hidden, so there is no inert affordance.

States: grouped/ungrouped, collapsed, filtered/no matches, sorting, saved view
selected, selection none/some/all, row disabled/overdue (caller label), pending
action, C3 stale selection, and every ContentState. Use inline actions on web;
swipe-only controls are not added (the brief permits swipe **or** inline).
No click handler makes an entire row swallow its links, checkboxes or buttons.
References: R02 groups/counts and lower bulk-action strip, R03 views/filter row,
R05 list-to-detail selection. Saved-view creation/deletion are caller-owned form
slots; the renderer never writes localStorage or invents a saved-view store.

### 3. DetailSheet and SidePanel

`DetailPanelProps`: `itemID`, `title`, `description?`, `label`, `state: ContentState`,
`closeLabel`, `returnHref`, `size: small|medium|large|full` (default medium),
`busy`. `DetailSheetProps` additionally has `open`, `placement: bottom|end|auto`
(default auto: bottom below 48rem, end otherwise), `closeOnOverlay` and
`closeOnEscape` (both default true). Slots: `Header`, `Body`, `Actions`,
`EmptyAction`, `RetryAction`; reuse Modal's header/body/footer frame and controls.
`SidePanel` uses the same content frame in a named `aside`, never `aria-modal` or
a focus trap. Its closed state uses ComponentProps.Hidden and contains no active
focus targets. Sheet is modal at every breakpoint; placement does not change
interaction semantics. Existing Modal centered/bottom behavior remains compatible.

Events: native dialog close/cancel, the return link, and caller action forms.
The shared Modal controller traps focus in the sheet and restores the opener;
if it vanished, focus the invoking list heading. SidePanel leaves the list usable,
moves focus to its heading only after explicit selection, and returns to the row
on Close. Only the newest request for itemID may populate an opened panel;
closing invalidates outstanding swaps. Reopening another item cannot apply the
old response. Reuse and extend the existing modal request/opening tracking.

Header and actions remain visible while the body scrolls; long actions wrap.
At 200% text the footer must not cover content; it becomes normal flow if needed.
Without JS, the opener follows its real detail URL and the same content renders
as a full page with returnHref. Do not render a hidden dialog as the only route
to detail. Busy is presentation only and cannot prevent Escape from working.
Dirty-form confirmation belongs to the caller through ConfirmDialog; a sheet
does not promise autosave. States: closed/open, bottom/end, loading, empty,
failed/retry, refused, long content, pending actions, conflict/read-only.
References: R05 right pane, R06 selected item sheet with lower actions.

### 4. Timeline and audit list

`TimelineProps`: `label`, `state`, `items: []TimelineItem`,
`layout: timeline|audit` (default timeline), `beforeLabel`, `afterLabel`,
`actorLabel`, `timeLabel`, `detailsLabel`, `more?: ChoiceLink`.
`TimelineItem`: `id`, `actorText`, `actorAvatar?: AvatarProps`, `time: TimeText`,
`summary`, `statusLabel?`, `tone?`, `changes: []Change`, `detailsHref?`.
`Change`: `label`, `beforeText`, `afterText`, `redacted: bool`, `redactedText` when
redacted. Redacted changes contain no before/after values in HTML or Props exported
to the browser. Missing actor is caller-supplied localized text, not a lookup.
Supplying beforeText/afterText on a redacted change is C1 **correctable** before
render/export; the component does not merely hide a secret with CSS.

Order is caller-owned and stable, including equal timestamps; the renderer does
not reverse pages or resort relative times. Timeline is an ordered list; audit
layout composes TableWithSlots and native disclosure of a DetailList per row.
Events: disclosure, details navigation, load-more navigation; no edit/delete or
synthetic audit emission. States: empty, loading, failed/refused, one/many events,
redacted values, absent actor, expanded changes, caller-described terminal event.
Reference: R04 actor/date/action/details columns. Before/after disclosure extends
that reference; it is not visible in the screenshot.

### 5. Stepper

`StepperProps`: `label`, `steps: []Step`, `currentKey`, `progressText`,
`state`, `busy`, `formID`, `saveStatusText?`. `Step`: `key`, `title`,
`description?`, `state: upcoming|complete|error|disabled` (default upcoming),
`href?`; currentKey marks exactly one existing, non-disabled step. Completed
count drives a native progress value against total steps; progressText supplies
its localized name/value explanation. No hidden/conditional steps are counted:
the caller supplies the effective sequence. Empty steps or unknown currentKey
are C1 **correctable**, not a flow that claims to be finished.

Slots: `Body`, `Back`, `Continue`, `SaveExit`, `Cancel`, `ErrorSummary`;
all actions compose existing controls/forms. Events: step link navigation or
native submit with caller adapter intents `back`, `continue`, `save-exit`.
Only a successful server response may change currentKey, mark complete or follow
the exit redirect. Save-exit waits for persisted success; failure keeps the
current inputs and reports the error. A lost response is C4 **correctable** with
unknown outcome, not permission to allocate a new command identity. Switching
steps never stores drafts in component state or localStorage.

Use an ordered navigation list with `aria-current="step"`; disabled/future steps
are plain labeled content unless the caller supplies an allowed link. At narrow
width the current step and progress remain visible with a disclosure for the
whole sequence. Native forms complete the flow without JS. States: first/middle/
last, completed steps, invalid current step, saving, save failed, save succeeded,
resumed draft, disabled step, read-only/refused. Reference: R07 left step rail,
top save/exit and bottom Back/Continue. Actual fields and validators are product
composition, not a reusable legal/configuration workflow.

### 6. SlotPicker and DateStrip

`DateStripProps`: `label`, `days: []DateChoice`, `selectedDate`, `previous?`,
`next?: ChoiceLink`. `DateChoice`: `date: YYYY-MM-DD`, `label`, `href`,
`today`, `disabled`, `reason?`. Days stay in caller order; dates are unique.
Selection is a date navigation link, not a booking; the selected link has
`aria-current="date"`. Dates wrap or use an explicitly named keyboard-scrollable
region. There is no custom ARIA calendar grid or implicit locale/week start.

`SlotPickerProps`: `label`, `state`, `dateStrip: DateStripProps`, `timeZone`,
`timeZoneLabel`, `nowUTC`, `slots: []Slot`, `selectedID?`, `quantity: int64`,
`name`, `formID`, `required`, `errorText?`, `snapshotText`, `stale`, `statusLabels`.
StatusLabels supplies localized available/booked/past/unavailable/full/stale and
capacity-unknown labels; the eligibility decision selects the matching label.
`Slot`: `id`, `startUTC`, `endUTC`, `timeText`,
`availability: available|unavailable|booked`, `capacity?: int64`,
`remaining?: int64`, `capacityText?`, `reason?`.
Slots refer to the selected local date in timeZone; end is strictly after start;
an overnight end is allowed. Capacity/remaining are both absent (unknown) or
both present with `0 ≤ remaining ≤ capacity`. Quantity is positive. Invalid
intervals/capacity/zone are C1 **correctable**; no negative capacity means unlimited.

The pure eligibility decision, also used by the renderer, uses this precedence:
stale snapshot → no choices; booked → booked; startUTC ≤ nowUTC → past;
unavailable → unavailable; known remaining < quantity → full; otherwise available.
Selected is decoration over an available slot only. A selected choice that
becomes ineligible is C3 **correctable**: clear it and show caller errorText.
Unknown capacity is labeled unknown and never a claim that seats remain.
Bookability of that snapshot still depends on availability from the authority.

Events: native radio `change` for the slot ID, date link navigation, and caller
POST confirmation. One fieldset/legend with one radio group; each radio's label
includes time, zone/offset where ambiguous, capacity and reason. Booked, full,
past and unavailable entries remain readable with disabled radios. Selection
does not reserve, decrement capacity, create an event or promise success.
The component does not compute recurring offers or parse an ambiguous wall time.
Duplicate wall-clock labels at a DST fold have distinct UTC instants/IDs and
visible offsets; nonexistent wall times have no slot supplied. States: default,
selected, booked, full, capacity unknown, unavailable, past, stale, validation
error, loading/empty/failed/refused/offline and pending confirmation.
References: R08 date strip/time chips; R09 date/quantity and sold-out treatment.

### 7. Calendar

`CalendarProps`: `label`, `state`, `view: day|week|agenda` (default agenda),
`dateStrip`, `rangeStartDate`, `rangeEndDate` (exclusive), `timeZone`,
`timeZoneLabel`, `language`, `firstWeekday: 0..6`, `nowUTC`, `events`,
`views`, `previous`, `next`, `today: ChoiceLink`, `allDayLabel`,
`agendaLabel`, `gridLabel`, `fallbackText`, `selectedID?`.
`CalendarEvent`: `id`, `title`, `description?`, `timeText`, `statusLabel`,
`tone`, `href?`, `allDay`, plus **either** `startUTC/endUTC` for a timed interval
**or** `startDate/endDate` (exclusive) for all-day civil dates. Mixing the two,
zero/negative duration, duplicate IDs or an invalid range is C1 **correctable**.
Slots: state actions and `EventDetails(event)` trusted content; no HTML in event
titles. Input is already expanded occurrences, not recurrence expressions.

Server output always contains DateStrip, navigation and an agenda from the same
events. Day uses the selected day; agenda groups the bounded range by local date.
Order is start instant then ID; all-day entries precede timed entries. A spanning
event appears on each overlapping local day; `[start,end)` never leaks into a day
starting at its exact end. Zero events is empty, not failed. The display includes
overlapping events without interpreting them as booking conflicts.

The shared FullCalendar adapter enhances week/day time grids with fixed input
events, explicit zone/locale/now, `editable=false`, no recurrence, background fetch,
drag/reschedule or browser-clock sampling. Keyboard-accessible event links point
to the same detail URLs as the agenda. Keep an explicit agenda toggle and fallback
if enhancement cannot load. SSR reserves the selected view's box before loading;
at widths below 48rem agenda is the default, with an explicit grid choice in a
named scroll region. The adapter must display 23/25-hour local days honestly and
distinguish folded hours; it cannot assume every day is 24 hours.

Events: date/view/range navigation and event detail navigation. No persistence
occurs when switching view. SelectedID is cleared with C3 **correctable** if the
new authorized event set omits it. States: day/week/agenda, today/selected day,
overlap, all-day, overnight, DST fold/gap, canceled/overdue (caller labels), and
the ContentState family. References: R10 week grid; R11 day strip; R12 dated agenda.

### 8. Product and price

The components below use MoneyText and plain supplied product/variant identities.
No catalog, tax, discount, fulfillment, stock or payment rule lives here.

| Constructor | Props beyond ComponentProps and ContentState | Slots / events / own states |
| --- | --- | --- |
| ProductCard | `id`, `title`, `description?`, `href`, `media: MediaProps`, `price?: MoneyText`, `priceText` when price unknown, `availability: available\|sold-out\|unavailable`, `availabilityText`, `badges: []BadgeProps` | `Actions` uses Button/form slots; title/media navigate to href. Ready, no image, unknown price, sold out, unavailable, caller-supplied promotion. Reference R13 product/media/price region. |
| OptionChips | `label`, `name`, `formID`, `options: []Option`, `value?`, `required`, `errorText?`; Option has `key`, `label`, `disabled`, `reason?` | Native radio change submits the opaque value; no computed variant graph. Selected, disabled/sold out, missing required selection. Reference R14 choice and disabled-chip regions. |
| QuantityInput | `label`, `name`, `formID`, `value: int64`, `min: int64`, `max: int64`, `step: int64`, `decreaseLabel`, `increaseLabel`, `errorText?`, `pending` | Native integer input, input/change events; buttons enhance by one step and dispatch change once. Minimum, maximum, invalid typed input, disabled, pending. Reference R15 quantity area. |
| BuyBar | `label`, `total?: MoneyText`, `totalText` when unknown, `summaryText`, `pending`, `unavailableText?` | `PrimaryAction`, `SecondaryAction`; submits caller's existing form. In-flow before sticky, pending, unavailable, stale total. Reference R14 bottom quantity/action area. |
| Cart | `label`, `lines: []CartLine`, `summary: OrderSummaryProps`, `quoteState: current\|stale\|incomplete`, `quoteText`, `resultKey`, `pending` | `LineActions(line)`, `Checkout`, state actions. Line quantity changes submit a caller form, removal is a separate POST action, checkout is explicit. Empty, updating, removed-last-line, sold-out line, changed quote, uncertain write. References R13 selected product and R16 editable line rows. |
| OrderSummary | `label`, `lines: []SummaryLine`, `subtotal: MoneyText?`, `total: MoneyText?`, `complete`, `incompleteText?`, `totalLabel`, `subtotalLabel` | `Actions`, `Footnote`. Read-only amounts; no hidden checkout operation. Current/incomplete/stale, zero price, included tax, discount, fee, and state family. References R16 subtotal/total and R17 order-summary column. |

OptionChips is single selection per named group; multiple independent attributes
are several fieldsets. No nesting of a card link around a form. The component
never selects the first variant or silently switches a now unavailable one.
Unknown value/disabled selected option is C3 **correctable**: clear, explain,
and require a new choice. Use Input's native radio semantics with shared styles,
not clickable divs. An OptionChip's visible label is its accessible name.

Quantity's valid set is `min + n*step`, n a nonnegative integer, through max.
Require `0 ≤ min ≤ max`, `step > 0`, and checked integer arithmetic; these are
C1 **correctable** for malformed Props. User entry outside that set is C2
**correctable**; never clamp silently. Entering zero does not imply deletion.
The caller uses min=1 for ordinary cart rows and supplies a Remove action when
allowed. Increment/decrement controls are hidden until enhancement is active;
native typing and submission always work. At min/max only the impossible direction
is disabled; max need not be a reachable step. Both directions are disabled while
the current input is invalid or pending. No floating-point input is accepted.
Use Input's text field with numeric inputmode and a digits-only pattern, preserving
the decimal string. The server validates int64 range/min/max/step; the shared
incrementer uses checked BigInt arithmetic and never valueAsNumber. This keeps
large quantities exact and retains typed errors until the caller corrects them.

`CartLine`: `id`, `productID`, `variantID?`, `title`, `description?`,
`media?: MediaProps`, `unitPrice: MoneyText`, `quantity: int64`,
`quantityInput: QuantityInputProps`, `availability`, `availabilityText`,
`lineTotal: MoneyText`, `revision`. QuantityInput.value must equal quantity.
All cart line amounts share the summary's currency. Positive quantity times
nonnegative unit minor units gives line total; check multiplication and addition
before every operation. Subtotal is the sum of line totals. This is deterministic
display validation, not the authoritative pricing decision.

`SummaryLine`: `key`, `label`, `amount?: MoneyText`, `text?`,
`effect: add|included|informational`. Add lines contribute signed minor units
(discounts negative); included lines show amounts already in subtotal, contributing
zero; informational lines have text only. Total is subtotal plus add lines.
The complete state requires every participating amount and exact equality with
supplied subtotal/total. Unknown amounts never become zero. Mixed currency,
overflow, mismatched supplied totals, negative final total, or a numeric amount
on an informational line is C1 **correctable** and returns no computed result.
An incomplete summary shows its known lines with incompleteText and no total.
SummaryLine entries are adjustments/inclusions/information, not a second list of
cart items to add again. Standalone OrderSummary checks the supplied subtotal plus
adjustments; Cart additionally checks its subtotal against its actual line items.
When complete=false, total must be absent (otherwise C1 **correctable**).

The cart's checkout control is unavailable for stale/incomplete quotes, no lines,
unavailable lines or pending submission; keep the reason visible. This is C3
**correctable** through a fresh server quote or choice. The server still checks
stock, price, actor, revision and command identity atomically. An empty cart is a
valid read and removing its last ordinary item is not universally forbidden.
BuyBar uses one actual action/form association; it does not clone a second submit
button on scroll. Sticky space is reserved, safe-area insets respected, actions
wrap, and focus/body content must stay visible with a keyboard or 200% text.

### 9. PricingTiers and PlanComparison

`PricingTiersProps`: `label`, `state`, `plans: []Plan`, `periods: []ChoiceLink`,
`selectedPeriod`, `currentPlanID?`. `Plan`: `id`, `name`, `description?`,
`price?: MoneyText`, `priceText` when amount absent, `periodText`,
`badgeText?`, `recommended`, `features: []PlanFeature`, `action: ButtonProps`,
`unavailable`, `unavailableText?`. `PlanFeature`: `key`, `label`,
`state: included|excluded|value|unknown`, `text` (mandatory localized meaning,
including for included/excluded). Price is the exact billed/displayed amount the
caller supplies; periodText distinguishes an annual bill from a monthly equivalent.

`PlanComparisonProps`: `label`, `state`, `plans`, `features: []FeatureHeading`,
`currentPlanID?`, `caption`; FeatureHeading has `key`, `label`, `description?`.
The matrix order follows those arrays. Every plan has exactly one cell for every
feature key; omitted cells are C1 **correctable**, never implicitly excluded.
Included/excluded use text plus icon. Current plan is identified independently
of recommended; recommending never means selected or authorized.

Events: period links navigate to server-priced alternatives; plan actions submit
or navigate as supplied. No client-side annual/monthly division, invented savings,
trial duration, upgrade permission or automatic purchase. Plan comparison composes
Table with column/row headers and a named scroll region; narrow layouts also show
one plan's full labeled feature list. States: default, current, recommended,
unavailable, unknown price/feature, period selected, loading, empty, failed/refused,
pending plan action. Reference R18 plan columns, billing-period switch and features.

### 10. MapView

`MapViewProps`: `label`, `state`, `points: []MapPoint`, `selectedID?`,
`view: list|map` (default list), `views: []ChoiceLink`, `mapLabel`, `listLabel`,
`legend: []MapLegend`, `viewport: MapViewport`, `tiles?: MapTiles`,
`mapUnavailableText`, `zoomInLabel`, `zoomOutLabel`, `snapshotText`.
`MapPoint`: `id`, `latitude`, `longitude` (finite floats), `title`,
`description?`, `statusKey`, `statusText`, `href`. `MapLegend`: `key`, `label`,
`tone`, `symbol: circle|square|triangle`; statusKey must name a legend entry.
`MapViewport`: finite `latitude`, `longitude`, `zoom: int` within the tile range.
`MapTiles`: `urlTemplate`, `minZoom`, `maxZoom`, `attributions: []Attribution`;
Attribution is plain `text` and `href?`, not provider HTML. URLTemplate is an
explicit same-origin absolute path with `{z}`, `{x}`, `{y}` placeholders, resolved
by the composition's tile service. No private token, geocoder or default public
tile service is embedded. The kernel does not create that proxy/service here.

Slots: `ListItem(point)` through DataList, `SelectedContent(point)` through
DetailSheet, state actions. Both modes contain the same authorized point set and
share selectedID; no cluster can hide a list item. Duplicate IDs/nonfinite/out-of-
range coordinates (latitude outside ±90, longitude outside ±180) or legend mismatch
are C1 **correctable**. Leaflet's Web Mercator cannot represent polar latitudes
beyond its projection range; retain those points in the list and label map
unavailability, rather than falsifying their location. No coordinate clamping.

Events: list/map navigation, point or row activation following the same href,
local map pan/zoom, and detail close. Leaflet's map is a shared enhancement;
the list remains the no-JS, keyboard and assistive alternative. Use token-colored
marker elements with text/symbol cues, never raw product colors. Coincident pins
offer access to each point through the list; clustering/routing are out of scope.
Local movement never fetches new tenant data or writes a location. An absent tile
configuration or tile failure is C4 **correctable**: show mapUnavailableText and
the intact list; distinguish it from a refused data read, which clears both modes.
Drop late selection responses using the DetailSheet opening/item identity rule.

States: list/map, selected/unselected, each supplied status, no points, one point,
coincident points, unavailable tiles, polar point, failed/refused/offline data,
loading, stale snapshot text. Do not ask for geolocation, track movement or store
positions. References: R19 status points/legend/list switch; R06 selection sheet.

### 11. Media

Keep Media's ready/loading/empty/failed/refused vocabulary and intrinsic dimensions.
Extend HeroProps with `layout: split|full-bleed` (default split); retain
SectionHeaderProps and HeroSlots.Actions/Media. Full-bleed means media spans its
container with the caption/title/action region following on a token surface;
unreadable text over an arbitrary photograph is not the default. No client-local
image CSS. Reference R20 full-width photograph and following identity region.

`PhotoGalleryProps`: `label`, `state`, `items: []Photo`, `selectedID?`,
`viewerOpen`, `previousLabel`, `nextLabel`, `closeLabel`, `returnHref`.
`Photo`: `id`, `media: MediaProps`, `full: MediaProps`, `href`, `positionText`.
Both media values require intrinsic width/height; informative images require alt,
decorative images require Decorative=true and empty alt. Text-only caption is
optional. Reuse Media to display both the thumbnail and full image; do not write
a second image renderer. `positionText` supplies each localized “image n of N”.

Events: thumbnail link opens its ordinary image detail URL without JS; the kernel
may enhance it with the existing Modal viewer. Previous/next buttons or Left/Right
keys change the selected image while the viewer is focused, without wrapping at
ends. Home/End select first/last; Escape closes and returns focus to its thumbnail.
Arrow navigation never intercepts keys inside a nested text input. Disabled ends
have labels and native disabled state. A removed/refused selected image closes
the viewer or renders a refusal without keeping its old src. No auto-advance,
autoplay, download grant, transform, upload or background prefetch of unseen photos.
An individual failed/refused Photo retains only its public fallback label/identity;
its thumbnail and full MediaProps clear their src values. The parent may remain
ready with other authorized photos. A failed/refused parent clears the whole set.
States: grid/one image, viewer open/closed, first/middle/last, caption, missing
image, loading, failed/retry, refused. Reference R13 multi-image/thumbnail region;
the full-screen viewer interaction is a specified extension of that region.

`MasonryProps`: `label`, `state`, `items: []Photo`, `columns: 1|2|3|4`
(default 2), `smColumns` (default 3 at 40rem), `lgColumns` (default 4 at 64rem),
`gap: shared spacing token`, `more?: ChoiceLink`. Slots: `ItemActions(photo)`.
Use native CSS multi-column flow and break-inside avoidance, with Media's reserved
aspect ratios. DOM and keyboard order follow columns top-to-bottom, then the next
column; do not use dense-grid reordering or a second masonry engine. At large text
use a minimum column width of 10rem and the requested column count as a maximum,
so captions/actions do not clip. No infinite-scroll-only path:
more is a real link. Each item activates its link or the same PhotoGallery viewer.
States: mixed aspect ratios, missing/loading/failed item, selected item action,
empty, failed/refused data. Reference R21 borderless mixed-ratio image columns.

### 12. Charts

The chart model describes numeric geometry, not financial, statistical or domain
truth. Formats, aggregation, sampling windows, comparisons and beneficial/unfavorable
meaning are supplied. SVG and the tabular alternative render on the server.

| Constructor | Props beyond ComponentProps and ContentState | Events / states / reference |
| --- | --- | --- |
| Sparkline | `label`, `summary`, `points: []ChartPoint`, `tone`, `decorative` | No interactive plot; decorative only with an equivalent surrounding value/summary. Rising/falling/flat, one point, gaps, empty. R22 sparkline column. |
| AreaChart | `label`, `description`, `series: []ChartSeries`, `xLabel`, `yLabel`, `xTicks`, `yTicks`, `ranges: []ChoiceLink`, `tableLabel`, `showDataLabel` | Range links navigate to supplied server snapshots; native details shows data. Positive/negative/zero/flat, gaps, single point, selected range. R23 plot frames/range selector; filling lines extends its sparse sample. |
| BarChart | `label`, `description`, `bars: []Bar`, `axisLabel`, `ticks`, `tableLabel`, `showDataLabel` | Data disclosure; no hover-only values. Positive/negative/mixed, zero, long categories, empty. R23 labeled plot frame is the reference; no actual bar sample occurs there. |
| StatTile | `label`, `valueText`, `description?`, `deltaText?`, `deltaDirection: up\|down\|flat\|unknown`, `deltaTone`, `comparisonText?`, `href?` | Optional real link. Positive/negative/zero/unknown delta, loading, missing value, failed/refused. R23 KPI row and lower retention delta. |

`ChartPoint`: `key`, `x: float64`, `y?: float64`, `xText`, `yText`.
`ChartSeries`: `key`, `label`, `tone`, `pattern: solid|dash|dot`, `points`.
`Bar`: `key`, `label`, `value: float64`, `valueText`, `tone`.
`Tick`: `value: float64`, `text`. Missing y is a gap with localized yText,
not zero; nonfinite numbers, duplicate point/bar/series keys or non-increasing
x within a series are C1 **correctable**. Do not silently sort, drop or connect
across gaps. All-gap input is empty. One point draws a point, not an invented trend.

Geometry uses a linear mapping `(v-min)/(max-min)` into a normalized viewBox;
vertical coordinates are inverted. Line x/y bounds use supplied finite points;
area and bar y bounds additionally include zero. If a bound is constant, expand
it on each side by `max(abs(v)*0.05, 1)`; all-zero area/bar uses [0,1]. Reject a
nonfinite derived span (C1 **correctable**). Area fill closes each contiguous
segment to zero; bar height is the signed distance from zero. Ticks are supplied
inside the computed range, in increasing order; there is no hidden tick rounding,
smoothing, interpolation, log axis, stacking or downsampling algorithm.
These narrow pure calculations have independent expected-coordinate tests.

The table includes every supplied value, missing marker and unit; large exact
amounts use their authoritative formatted int64 text, never a float reconstructed
from SVG coordinates. A chart summary names the measure and range. Multiple series
have labels and dash patterns as well as token colors. Empty, failed and refused
do not emit meaningless axes or sensitive hidden data. Hover can highlight a value
already in the table; it is never the only way to learn it. Money totals and a
StatTile's favorable/unfavorable tone are never inferred from slope or sign.

## Gallery, accessibility and localization acceptance

Use existing ExampleInfo IDs and explicit Gallery entries; retain every existing
public ID. New component IDs are `pk-ui.component.<kebab-name>` and new story IDs
append `/<case>` from this table. These are planned entries, not a new runtime
registry. Each entry captures the real constructor and Props/slots; browser tests
consume those same exports. Interaction tests drive a story to hover, pressed and
focus rather than adding fake Props that claim a CSS pseudo-state is active.

`S` below means the explicit cases `default`, `loading`, `empty`, `failed`,
`refused`, `offline` (ready snapshot) and `offline-failed`. `A` means the same
entry's interactive controls must additionally be tested hovered, pressed,
keyboard-focused, disabled and pending where that control submits a request.
Do not pretend a static chart or skeleton has pressed/disabled behavior: its A
cases apply only to its actual links, toggles or retry actions. State primitives
and small controls use their own cases below rather than an invalid S wrapper.

| Component / story prefix | Required cases in addition to A where applicable |
| --- | --- |
| empty-state | default, with-action, without-action, long-copy, legacy-description, canonical-text |
| notice | failed-retry, retry-pending, offline, refused, success, dismissible, urgent, passive, long-copy |
| skeleton; table-skeleton | default, geometry, reduced-motion; no inert-element hover/pressed/focus/disabled/empty/error cases |
| data-list | S; grouped, ungrouped, collapsed, no-matches, filter-chips, sort-ascending, sort-descending, saved-view, selection-none, selection-some, selection-all, selection-stale, row-disabled, overdue, pagination |
| detail-sheet | S; closed, bottom, end, long-body, wrapped-actions, pending, read-only, conflict, late-response |
| side-panel | S; open, closed, long-body, wrapped-actions, read-only, focus-return |
| timeline | S; audit, actor-missing, changes-expanded, redacted, equal-times, load-more |
| stepper | S except empty (empty sequence is C1); first, middle, last, step-error, disabled-step, save-pending, save-failed, save-succeeded, resumed, read-only |
| date-strip | default, today, selected, disabled-date, long-label, month-boundary, keyboard-scroll; parent SlotPicker/Calendar owns asynchronous S states |
| slot-picker | S; selected, booked, full, capacity-unknown, unavailable, past, overnight, dst-fold, stale, invalid-selection, confirmation-pending |
| calendar | S; day, week, agenda, all-day, overlap, overnight, dst-fold, dst-gap, selected, canceled, overdue, enhancement-failed |
| product-card | S; no-image, unknown-price, sold-out, unavailable, promotion |
| option-chips | default, selected, disabled-option, sold-out, required-error, stale-choice, long-label; parent owns S states |
| quantity-input | default, minimum, maximum, invalid, overflow-boundary, disabled, pending; no empty/error content substitution that discards typed input |
| buy-bar | S; sticky, wrapped, pending, unavailable, stale, keyboard-open |
| cart | S; updating, removed-last-line, sold-out-line, stale-quote, incomplete-quote, uncertain-write |
| order-summary | S; zero, included-tax, discount, fee, incomplete, stale; malformed totals are validation cases, not a buyable story |
| pricing-tiers | S; current, recommended, period-selected, unavailable, unknown-price, pending |
| plan-comparison | S; current, recommended, unknown-feature, long-features, narrow |
| map-view | S; list, map, selected, legend-statuses, one-point, coincident, polar, no-tiles, tile-failed, late-response |
| hero | default (existing split), full-bleed, loading-media, empty-media, failed-media, refused-media, long-title |
| photo-gallery | S; single, viewer-first, viewer-middle, viewer-last, caption, image-failed, image-refused, selected-removed |
| masonry | S; mixed-ratios, image-loading, image-failed, item-action, long-caption, load-more |
| sparkline | S; rising, falling, flat, single-point, gaps, decorative |
| area-chart | S; positive, negative, zero, flat, gaps, single-point, multiple-series, selected-range, data-expanded |
| bar-chart | S; positive, negative, mixed, zero, long-categories, data-expanded |
| stat-tile | S; delta-up, delta-down, delta-flat, delta-unknown, linked |

Every applicable story is rendered with English (`en`) and European Portuguese
(`pt-PT`), light and dark, and each explicitly supplied client theme Pair. Theme
fixtures come through the existing authorized storybook composition; there is no
client-name switch in a component. Kernel validation covers design.Default and
generic stress palettes. Client-theme coverage requires the adoption tasks to
supply their actual pinned pairs; absence is recorded unverified, not silently
replaced with Default. The matrix records theme/locale/story IDs and passed,
failed or skipped outcomes. Runtime code must not discover story files by name.

Test at 360, 768, 1024 and 1440 CSS px, plus the 320px reflow boundary, 200% text,
reduced motion and forced colors. The brief's hit target is at least 44×44 CSS px
for every interactive control, including chips, checkbox labels, incrementers,
dialog close and chart/map controls, without overlapping targets. Visual density
may shrink decoration but cannot shrink that target. Sticky footers must not
obscure the last field, focus ring or error at any tested size. Every media/skeleton
swap reserves dimensions: record zero layout shift attributable to late content
within that specimen, not an unmeasured claim about a whole product.

Keyboard and screen-reader walks must complete every native fallback and enhanced
flow, including tab/shift-tab, Enter/Space, Escape, relevant native arrow handling,
focus restoration, errors and busy announcements. Use labels/legends, heading
hierarchy, native tables/lists and named scroll regions. A modal contains focus
and makes the background inert; a side panel does neither. This follows the
[WAI dialog pattern](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/).
Measured text contrast must be 4.5:1 (3:1 for qualifying large text); functional
non-text boundaries/cues 3:1. Check disabled and focus states separately against
the applicable criteria. These targets use
[WCAG text contrast](https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html),
[non-text contrast](https://www.w3.org/WAI/WCAG22/Understanding/non-text-contrast.html)
and [reflow](https://www.w3.org/WAI/WCAG22/Understanding/reflow).

Use a persistent empty polite region before updating routine status/count text;
urgent untied failures alone use assertive. Field errors use aria-invalid and
aria-describedby. Visible labels occur in accessible names. Images convey meaning
through alt or are explicitly decorative. A color never supplies the only status.
No autoplay; reduced motion removes skeleton shimmer and animated sheet/map travel.
No positive tabindex, hover-only action, swipe-only task or raw inline event handler.

No new renderer authors user-visible English or Portuguese. Callers supply all
visible/accessible messages through Props/slots, selected from their request's
[locale seam](../page/README.md). Existing legacy default labels stay compatible;
the new compositions must pass every label explicitly, so they never exercise an
English fallback in the Portuguese stories. Kernel gallery/recovery copy stays
with its existing owner; no parallel translation table is installed in renderers.
Missing required copy is C1 **correctable**; the story matrix checks completeness.

TimeText and MoneyText are formatted at composition, outside the renderer import
closure. The current locale.Formatter has Text, not date/time/currency methods;
this task does not claim or introduce a second locale engine. The caller supplies
localized date/time/number/currency projections; the gallery supplies explicit
en/pt-PT fixtures for identical raw values. New components must not reinterpret
their symbols, decimal separators, time zones or week starts. Calendar's engine
formatting uses the explicit language/zone and caller labels, never browser defaults.
Stable form values remain UTC instants, IDs and canonical integer quantities.

Money's currency is a named Currency code value with uppercase three-letter
syntax; actual supported currency and minor-unit exponent belong to the caller's
formatting/commerce capability. Portable captures must encode int64 minor values
as decimal strings and describe that in their schema; native Go remains int64.
The examples/export schema test must enforce that representation before declaring
these Props portable. Controllers never parse a large minor value as a JS Number
or recompute money; submitted totals are never authoritative. Fixtures cover zero,
two and three decimal places and values above JavaScript's safe integer range.

## Mobile names and compatibility

T-0180 is the named mobile counterpart. Its trace revision is
`2d56f1a36486fa4353dbc7afdfa64707073e47af`; the observed existing concepts are
EmptyState, Notice, Skeleton and ListScreen/ResourceList. New component names and
lower-camel semantic Props in this specification are the proposed shared contract
for both kits. Actual parity must be checked against T-0180's landed API before
publishing either new public family; an in-progress mobile report is not agreement.

| Concept | Shared name/semantic Props | Existing web/native compatibility |
| --- | --- | --- |
| Empty result | EmptyState: title, text, action | Web Description remains a fallback alias, and Actions/IconStart remain trusted slots. Mobile already has title/text/action. |
| Notice | Notice: tone, title, text, action | Web delegates to Alert, mapping ok→success; Alert and its Message remain supported. Native Notice currently has ok/text; no second web alert implementation. |
| Loading | Skeleton: shape/lines through web Props; loading belongs to the containing state | Native Skeleton/ListScreen continue to compose the same concept; native refreshing is not a new web loading lifecycle. |
| Lists/details/flows | DataList, DetailSheet, SidePanel, Timeline, Stepper | New semantic data fields and states use the same names; native callbacks replace web link/form transport. ListScreen stays the native template, not a renamed web Table. |
| Dates/time | DateStrip, SlotPicker, Calendar | Same IDs, UTC/civil-date distinctions, timeZone, capacity and status semantics; each platform supplies its renderer. |
| Commerce | ProductCard, OptionChips, QuantityInput, BuyBar, Cart, OrderSummary, PricingTiers, PlanComparison | Same Money/minor/currency, quantities and quoted state; neither platform acquires checkout authority. |
| Maps/media/charts | MapView, Hero, PhotoGallery, Masonry, Sparkline, AreaChart, BarChart, StatTile | Same point/photo/series identities, status and label data; mobile does not embed Leaflet/FullCalendar or HTML. |

The platform-specific action binding is deliberate: web ButtonProps/form slots
provide href, form association and HTMX enhancement; native action.onPress invokes
the application callback. Only label/disabled/pending semantics are shared, never
callbacks serialized as data. Compatibility mappings and golden value/state cases
belong to the two kits, not per-product wrappers. No source file in the mobile
checkout is changed by this task.

## Standards and provider decisions

The pillar register retains gomponents and HTMX; current source pins gomponents
v1.3.0 and [NOTICE](../../NOTICE) records HTMX 2.0.8. Native HTML forms, inputs,
links, dialog, tables and SVG remain the baseline. Rich states, lists, panels,
stepper, timeline, commerce and plan comparison compose these existing OSS units.
Adding a second UI framework would duplicate their renderer, style and focus
owners without providing a missing domain decision. This paragraph is the
open-source comparison for those groups, not a claim that each needs a library.

Calendar's graphical week/day layout uses **FullCalendar Standard 7.1.0**, its
time-grid plugin and required runtime dependencies bundled as kernel assets.
It supplies the missing overlap/time-grid engine while server DateStrip/agenda
retains useful no-JS behavior. Select only the Standard distribution and pin the
exact transitive artifacts/licenses in the implementation slice; no Premium
resource scheduler, React wrapper, CDN script or product-local bundle. The
[time-grid API](https://fullcalendar.io/docs/timegrid-view) covers day/week and
[Standard is MIT licensed](https://fullcalendar.io/license). The inspected
[v7 zone API](https://fullcalendar.io/docs/timeZone) supports named zones; use that
API rather than the older Luxon-plugin assumptions. Its
[styling hooks](https://fullcalendar.io/docs/upgrading-from-v6-css) permit owned
classes. A browser spike must prove token styling, named-zone fold/gap behavior,
CSP and 44px accessible alternatives before the slice can pass. If it fails,
report the failure and revise this choice; do not quietly build a rival engine.

MapView uses **Leaflet 1.9.4**, the API version inspected in its
[official reference](https://leafletjs.com/reference.html), to own projection,
tiles and keyboard pan/zoom. Token marker controls, list and selected sheet remain
PlatformKit compositions. Bundle the pinned upstream runtime and required license
notice through ui.Assets/Controllers; all optional loading stays in kernel scripts.
The caller supplies tile URL/attribution per request; there is no default service
or process-global provider selection. Leaflet instances are destroyed on removal
or HTMX replacement. Tile/engine failure leaves the existing list path usable.
No network SDK appears in Go Props or domain contracts.

Media uses native images, CSS columns and the existing Modal.
[PhotoSwipe](https://photoswipe.com/) was considered for a larger gallery engine.
This brief requires image selection, previous/next viewing and close, which the
existing focus/overlay owner can carry. Pinch-zoom animation, slide transitions
and preloading are not required; installing a second modal/viewer mechanism for
them would replace a traced reusable unit. This choice does not authorize a new
gesture or photo-processing engine. Masonry is the browser's native column layout.

Charts use server-rendered [SVG](https://www.w3.org/TR/SVG2/).
[D3 linear scales](https://d3js.org/d3-scale/linear) were considered. D3 is useful
for broader browser visualization, but these four bounded components require only
affine geometry, gaps and plain tables that must exist before scripts run. The
small pure calculations defined above avoid a second browser/server scale and
formatting implementation. There is no general chart engine, automatic statistics
or custom scale library; expansion into those features requires another OSS
comparison. This is the explicit reason for not adding a chart dependency here.

Upstream assets' implementation must be versioned with provenance and pass the
existing asset/CSP/import/style gates. The specification does not install them.
Library mechanical positioning is contained within the owned adapter; all visual
colors, typography, spacing, borders and states use design tokens. A library's
defaults are not a waiver for the theme or accessibility matrix.

## Ten questions and Limits

These answers describe the presentation layer's share; this is not a domain module.

| Question | Resolved kernel share | Limits: capability/product share |
| --- | --- | --- |
| 1. Entity fields | No entities/tables. Only the consumed Props/slots above; hidden DOM state is limited to form association, selection and current opening. Nothing is stored for a future hypothetical command. | Products own actual customer, booking, stock, order, event and saved-view fields. |
| 2. Identity and relationships | Instance IDs are document-local; item IDs are opaque, unique within the supplied set; selection must refer to that set. No global ID-to-object lookup, foreign key or tenant field pretending to authorize HTML. | Identity resolution and relationships are read through the request's explicit tenant transaction before projection. |
| 3. States and transitions | ContentState has the existing five statuses; offline and success are explicit notice compositions. Native input changes update temporary selection only. Server responses supply authoritative domain state; late responses cannot overwrite a newer selection. | Booking/canceling, workflow completion, stock reservations, saved drafts and order lifecycle remain domain transitions. |
| 4. Authorization | Components perform no authorization lookup and emit no hidden protected payload in refused state. Missing caller-supplied action means no affordance. I1 **immutable** for the denied request. | Every read and command authenticates actor/tenant; command services recheck grants and resource scope inside db.Tx[db.Tenant]. Hidden/disabled buttons are never the check. |
| 5. Transaction and concurrency | Renderer is pure, has no Tx/Clock dependency and never calls time.Now. Shared controllers scope ephemeral state to an instance/request generation; they do not claim locks. C3 **correctable** requires reload/new choice, not an optimistic stale row. | Command service owns expected revision, lock/compare, all-or-none bulk semantics, audit and outbox in one transaction. Refused writes return no stale row and write/emit nothing. Do not bind controls to commands lacking that guarantee. |
| 6. Events and idempotency | No domain events or jobs. Input/submit/navigation events are consumed by existing controls and caller routes. Rendering, retrying a GET or opening a sheet produces no domain mutation. | The caller generates and retains command identity for uncertain POSTs; the service binds it to actor/tenant/operation/input and rechecks access on retries. UI must not invent once-only semantics. |
| 7. Retention and export | No UI retention database, cookie, localStorage draft, service-worker write queue or shared HTML cache. Remove node-local state/listeners on teardown. Existing Example.Describe/export carries only explicit synthetic gallery data and authorized captures. | PII minimization, media lifetime/signed URLs, audit retention and authorized CSV/tenant export are capability policy; export links recheck scope server-side. |
| 8. Provider integration | FullCalendar/Leaflet are isolated kernel browser adapters over supplied values. Data reads stay with guarded resource/capability adapters; images/tiles use caller-supplied authorized URLs. C4 **correctable** has an explicit fallback. | Tile/media origins, hosting, credentials, data feeds, commerce/payment/booking providers and lawful use of content are product composition. No provider string is a client selector. |
| 9. Locale/time/money | Request-local strings; UTC instants plus explicit IANA zone/civil dates; explicit nowUTC for eligibility/current-time display. Money uses int64 minor units and Currency, checked arithmetic, no FX and no renderer-authored words. | Preferred locale/time zone, currency/exponent, translations, tax, rounding of domain calculations and what counts as overdue belong to the capability/product. A service samples its injected Clock in Deps. |
| 10. Specialist validation | Only structural presentation validation, interval ordering, finite geometry, choice eligibility and amount consistency as specified. C1/C2/C3 are **correctable**; terminal I2 is **immutable**. Empty collections are valid reads. | Sector rules, jurisdiction, booking windows, capacity policy, qualification, real prices, cancellation/stock and statistical/financial meanings are outside UI. No branch on composed modules or client name. |

The “last one” invariant lives at the authoritative mutation, never a renderer's
count. A service that preserves at least one qualifying member refuses the write
that removes the last one; reading or repairing an already empty collection is
not refused. DataList/Cart empty states therefore work even when there are no rows.
The UI cannot infer that invariant from a paged count or globally disallow Remove.

No migrations apply. If later domain work adds persistence, it is a separate
capability delivery with transactional `<version>_<name>.up.sql`, tenant column
and forced RLS on every tenant table and no down migration. No SQL service/fake,
`module.go`, permission or event declaration is created just to fit the task name.
Any future module contracts obey the kernel/lower-tier contracts-only dependency
boundary and contain no gomponents, HTML, driver, SDK or HTTP framework.

## Independent conformance cases

These are the cases to write before implementing each slice. Expected results are
specified independently of markup helpers. New-case outcomes below are **required**,
not executed/passing results. Direct renderer tests use values and the same pure
decision used in production; browser cases use the real Gallery export and existing
specimen helper. A shadow renderer, fake DOM implementation or imitation SQL
service would violate the reuse inventory.

There is no new SQL service to fake. For the traced Task consumer, use the existing
[tasktest fake/conformance suite](../../modules/task/contracts/tasktest/conformance.go).
Both [fake.Resolve](../../modules/task/contracts/tasktest/fake.go) and
[SQL Service.Resolve](../../modules/task/internal/service.go) already call
[domain.Resolve](../../modules/task/domain/resolve.go). The existing executable
`TestFakeConforms` exercises the baseline decisions in F1–F3 below. F1's retry
silence/time and F2/F3's error identities are already asserted; the explicit nil
result and no-state/no-event assertions on F2/F3 are additional acceptance checks,
not evidence supplied by the current suite's error-only assertions. Its fake does
not enforce tenant RLS, transaction rollback, actor grants or SQL locking; only
real PostgreSQL/service cases can establish those claims. Do not extend it into
a booking/cart fake for this presentation delivery.

| ID / boundary | Given / action | Required outcome and reason |
| --- | --- | --- |
| F1 existing Task fake + SQL RunService | Open task, Resolve with surrounding spaces, then exact/empty retry | One resolved transition and event; original resolution time preserved on retry. Existing shared domain decision, not a UI-derived success. |
| F2 existing Task fake + SQL RunService | Resolved task, Resolve with different text | Conflict (**immutable** for that resolved task's account), nil result, no extra state/event; covers the read-only/terminal presentation. |
| F3 existing Task fake + SQL RunService | Unknown ID, then Assign/Resolve/CheckSLA | Not found (**immutable** for that tenant/object request); no row, no event. UI renders refusal according to the guarded caller, never a stale detail. |
| P1 all new aggregate Props | Duplicate item IDs or missing required label; call Validate then Render through document.Render | Same C1 **correctable** error, nil document bytes; no partially emitted sensitive values or computed totals. |
| P2 rich states | An empty authorized result; then a failed read; then refused with old rows still supplied | Empty shows next action; cleared failed state shows retry. Retained data on failed/refused is C1 **correctable** and returns no bytes/capture; after clearing it, I1 **immutable** shows refusal without old text/URLs/IDs or a retry loop. |
| P3 notices | Successful retry or changed selection count announced twice | One stable polite region updates once per outcome; no duplicate alert caused by wrapping it in another live region. Dismissal does not erase a persisted result. |
| P4 list selection | Rows A/B eligible, C disabled, page two has D; select-all, then deselect B | Submit A/B only, then A only; header checked then indeterminate. Never C or D. Native no-JS form submits individually checked rows. |
| P5 list scoping | Two lists with identical item IDs; select in the first; replace its resultKey | Second list unchanged; first selection clears and count label is index zero. C3 **correctable** stale selection cannot survive filter/tenant change. |
| P6 grouping/sorting | Two server pages, grouped counts larger than rendered rows; follow descending link | Request supplied URL and preserve server order/count text. No client-only reorder that makes page two inconsistent. |
| P7 panel concurrency | Open A, then B; A response arrives last; close B before B response | A never replaces B; no response reopens a closed panel; no old protected body remains. Focus returns to the latest valid opener or list heading. |
| P8 panel accessibility | Sheet with long content/actions; SidePanel beside a usable list | Sheet traps/restores focus and Escape closes; SidePanel does not trap. Footer never covers last field at 360px or 200% text; no-JS follows detail/return links. |
| P9 audit privacy | Change has redacted=true with accidentally supplied old/new secret text | C1 **correctable** composition refusal before output; the fixture cannot serialize redacted values or pretend the component is the audit store. |
| P10 stepper save | Step 2 of 4, save-exit response fails or is lost | Remain on step 2 with entered values; failed/unknown outcome shown. No progress advance or exit redirect until confirmed success. C4 **correctable** preserves caller command identity. |
| P11 stepper validation | Current key missing, duplicate key, or current step disabled | C1 **correctable**; do not pick another step or show a completed flow. Native form field errors focus the first invalid field. |
| P12 slot eligibility | Future available slot capacity=3/remaining=1, quantity=1 then 2 | First enabled; second full/disabled with reason. One selected radio does not reserve a seat. C3 **correctable** on second choice. |
| P13 slot boundaries | start equals now, then future booked with remaining=3, then unknown capacity | First past; second booked by precedence; third eligible only if supplied available, labeled capacity unknown. Negative/inconsistent capacity is C1 **correctable**. |
| P14 date/time | Europe/Lisbon: 2026-10-25T00:30Z and 01:30Z; then 2026-03-29 clock gap | Distinct IDs/offsets for the repeated local 01:30; no invented nonexistent local hour. Calendar spans intersect civil-day boundaries using [start,end). Server nowUTC, not browser clock, controls today/past. |
| P15 calendar | Timed event ends exactly at next day's midnight; one multi-day all-day item; overlap | Timed event absent from next day; all-day uses exclusive end; both overlapping events reachable by agenda/link. No booking refusal inferred from overlap. |
| P16 options/quantity | min=2, max=9, step=2; enter 3, then 8 and increment; selected option becomes disabled | 3 is C2 **correctable**; 8 is valid and increment disabled, not clamped to 9. Disabled option selection clears with C3 **correctable**. |
| P17 totals | Unit minors 125×2 and 200×1; add discount −50, shipping +25; included tax 75 | Line totals 250/200, subtotal 450, total 425; included tax contributes zero. Caller labels do not change arithmetic; source slices unchanged. |
| P18 total refusals | Currency mismatch; MaxInt64×2; inconsistent supplied total; missing amount | First three C1 **correctable**, no result; missing amount is incomplete, no zero substitution or buyable total. An empty cart remains a valid empty state. |
| P19 money serialization | Minor 9007199254740993 and negative adjustment −50 | Go arithmetic stays exact; capture/schema use decimal strings; round trip does not become 9007199254740992. No JS Number arithmetic. |
| P20 plan comparison | Recommended plan differs from current; cell unknown; change billing period | Recommendation never checks/selects it; unknown is labeled unknown; navigation uses supplied server price, no divided annual price or invented saving. |
| P21 map | Same ID selected by list and pin; two colocated points; tile failure | Same selected content; both points reachable via list; tile failure retains list and explicit C4 **correctable** fallback. Cross-instance selections do not leak. |
| P22 map refusals | NaN longitude; valid polar latitude; refusal after previous ready map | NaN is C1 **correctable**; polar point stays in list without relocated pin; refused data removes all points/selection/coordinate payload (I1 **immutable**). |
| P23 media/viewer | Slow portrait image and wide landscape with declared dimensions; viewer last photo, then removal | No image-decode layout shift; Next disabled at end; Escape restores thumbnail focus; removed/refused image's old src is absent. |
| P24 masonry | Mixed ratios and action labels at 360px/200% text; keyboard across all items | Reserved geometry, no clipped/overlapping targets, DOM order follows visual column order. More link and each photo usable without JS. |
| P25 affine chart scale | Points (0,0),(10,10) in a 100×100 plot; bar values −5,+5 | Line maps (0,100),(100,0). Bars use zero at y=50 and reach 100/0 respectively. Tests compare coordinates, not copied implementation branches. |
| P26 chart edge cases | Sparkline y=7 flat series; one point; a missing middle y; NaN/infinite input | Sparkline flat range [6,8]; one point has no false trend; gap is disconnected and explicit in table; invalid input is C1 **correctable**, never malformed SVG. |
| P27 accessibility/state matrix | Each Gallery story in both modes/locales and supplied theme pairs | Declared classes, escaped content, names/roles, keyboard paths, measured contrast/targets/zoom and no inline handlers all pass. A missing theme/run is skipped/unverified, never green. |
| P28 request isolation | Two concurrent authorized stories with different tenant themes/messages but identical local item IDs | No other tenant's copy, examples, styles or data; run the existing authorized gallery/RLS tests as well as the new pure parallel-render cases. The fake cannot prove this. |
| P29 library lifecycle | Repeat an HTMX replacement of calendar/map/viewer three times, then interact once | Exactly one instance/listener action; destroyed roots receive no updates; no additional network event source or leaked map. Reuse the real upstream libraries, not a private mock engine. |
| P30 command authority (consumer integration) | Two actors submit same slot/quote revision; access is revoked before one commit; retry unknown outcome | Domain command, not UI, proves at most one permitted transition, no unauthorized/stale row, and atomic audit/outbox. Without such a producer fixture this scenario remains unverified; do not claim it from a radio or cart unit test. |

The fake-side cases reuse the producer's existing decisions and suite, with the
stronger refusal assertions identified above to add at that same boundary.
UI-only logic has value conformance cases rather than an invented Service. SQL
concurrency, rollback and audit checks remain at the capability that performs the
write; no placeholder SQL is proposed here. New test names/file placement can
follow the neighboring component tests, but each case ID remains in its assertion
description so the implementation report can link a result to this contract.

## Visual reference register

These are the review's Mobbin screenshots, visually inspected for this
specification. The private phase report maps each ID to the exact existing
`refs/apps/<app>/<file>`; images are not copied into the public repository.
Product names in this table identify the reference, not specialized component
behavior. Each component section names the region it answers. A screenshot is
neither a license to redistribute the asset nor proof of interaction/accessibility.

| ID | Reference | Region used |
| --- | --- | --- |
| R01 | [Linear empty projects](https://mobbin.com/screens/5273571d-4f58-410a-97a0-f6018dc8b80f) | Centered empty introduction and next action, with surrounding shell preserved. |
| R02 | [Linear grouped issue list](https://mobbin.com/screens/be6c4ee4-aa93-42b4-89b3-dcfc8386f022) | Group headers/counts, aligned rows and lower selected-items action strip. |
| R03 | [Relevance AI saved views](https://mobbin.com/screens/c9df4ae4-6265-48ac-8e16-95220d542fce) | Views above filters/table and separate success notice. |
| R04 | [Vanta event log](https://mobbin.com/screens/41659ab7-5710-4abf-91ca-c6bf482c9b0b) | Actor/time/action/details columns and repeated row geometry. Blurred data is not labeled a real skeleton state. |
| R05 | [Asana list/detail pane](https://mobbin.com/screens/4b448c3a-ea3c-441c-8af6-abde7ad992dd) | Selected row with adjacent named detail and activity region. |
| R06 | [Freenow trip sheet](https://mobbin.com/screens/b1ac38f3-23c1-470f-aac9-ccb002514a1e) | Selected map item in a bottom sheet with readable facts and lower actions. |
| R07 | [ElevenLabs stepped form](https://mobbin.com/screens/142853bc-e171-4128-8386-6e288b5ad49d) | Left progress rail, explicit save/exit, Back/Continue below form. |
| R08 | [Airbnb time-slot sheet](https://mobbin.com/screens/cc30131f-ba65-4613-b8e2-304eba91f5b1) | Date strip, time choices and selected time; no inference of booking authority. |
| R09 | [Klook date and quantity](https://mobbin.com/screens/75c64351-7fef-4198-ac3d-72ec0b2432a4) | Date/time choices, quantity bounds and sold-out explanation. |
| R10 | [Fresha calendar week](https://mobbin.com/screens/14d65e9e-4469-46a0-a483-8e95091c397b) | Day columns, time axis, overlaps and status-coded events. |
| R11 | [Equinox+ day schedule](https://mobbin.com/screens/f469c43a-f58d-4bb4-aa10-7b3cbe284e6c) | Current/selected day strip above day's event content. |
| R12 | [Time2book upcoming schedule](https://mobbin.com/screens/515b06ca-69c6-475b-97ee-510f26b203dd) | Dated agenda groups with time, place and capacity metadata. |
| R13 | [Faire product detail](https://mobbin.com/screens/21bc409a-1d01-4003-8709-2bcb845a8199) | Image gallery beside product identity, exact price and quantity choice. |
| R14 | [Yami option chips](https://mobbin.com/screens/5e7aabb9-eef8-4a90-bacc-eb15c521a417) | Named selected/disabled options and bottom purchase controls. |
| R15 | [Shop product detail](https://mobbin.com/screens/a0ffb7b8-9dc1-485c-acd2-9951dee69fe9) | Product/price hierarchy, option group and bounded quantity control. |
| R16 | [Contra invoice line items](https://mobbin.com/screens/e03dde45-0cec-4e7b-991e-22157d904aa9) | Editable line quantities/amounts and explicit included-tax/subtotal/total rows; adapted to cart display only. |
| R17 | [Walmart order detail](https://mobbin.com/screens/c75ab520-4707-4e7b-9d65-7e5d7c888111) | Separate item, progress and monetary-summary areas. |
| R18 | [Dribbble plans/features](https://mobbin.com/screens/73f76d09-9508-49ba-810e-8b2d13ff74e6) | Billing-period choice, plan prices, recommendation and feature comparison. |
| R19 | [Felt operations map](https://mobbin.com/screens/69edfc38-9b7d-4d32-8319-87ebd3cb3829) | Status points, legend/list tabs and map controls. |
| R20 | [Fi pet profile](https://mobbin.com/screens/3f271e88-4cc1-4c53-8172-602069a0fe78) | Full-width photo followed by identity/facts on an opaque surface. |
| R21 | [Savee masonry board](https://mobbin.com/screens/ae31b243-afc4-4f4c-925f-88d342dc3701) | Mixed-ratio, borderless photo columns with controlled gutters. |
| R22 | [Uniswap tokens table](https://mobbin.com/screens/e39f0d0f-bed1-49ff-92e0-c53d36d34de6) | Compact sparkline column beside exact values and directional change. |
| R23 | [Sweatpals overview](https://mobbin.com/screens/50fbe150-69d2-422f-8a9c-c2ce539afc4f) | KPI row, comparison selector, labeled graph frames and delta badge. It does not supply a populated area/bar example. |

Offline, focus, error, full-screen viewer, before/after audit disclosure, populated
area and bar charts, and dark/Portuguese variants are explicit extensions where
the selected image lacks them. Acceptance compares structure to the named region
and checks the specified behavior separately; it never asserts pixel equality to
a state the reference does not contain.

## Adoption and implementation gates

Slice 1 migrates the traced generated list's empty state to EmptyState with its
authorized New action, and localizes the existing document recovery compositions
through their existing owner. Preserve their controller IDs and fallback behavior.
Slice 2 moves list layout/count/pager composition into DataList while resource.List
continues to project entity fields, rows, sorting URLs and authorized command forms.
Keep a single ungrouped group for the generated baseline; remove the replaced
layout, never duplicate Table's cells or sorting. Collection commands stay below
rows and above the pager. They do not become bulk-selected commands by accident.
Slice 3 shares the detail content frame with resource.Detail's ordinary page;
custom callers may additionally open it through an authorized sheet route.

Measure the traced consumer's call sites, source lines and repeated markup before
and after each change; this specification claims no reduction yet. The externally
named app needs are met only after T-0126 updates the consumed kernel pin and each
app task replaces its old composition. Gallery samples alone do not prove adoption.
Do not create a client-named renderer package or leave an old implementation beside
the adopted unit. A compatible alias/delegate may keep a published constructor.

Before each implementation commit run the repository's `make check`; before any
push the integrating session runs `make e2e`, with the development services in
[CONTRIBUTING](../../CONTRIBUTING.md#verify-at-the-relevant-boundary). This worker
does not push or start services in the specification phase. Never use `make down`
as a test command. Focused feedback uses existing boundaries:

```sh
go test ./ui/components/... ./ui/resource ./ui/screens -count=1
go test ./modules/task/contracts/... -count=1
./scripts/check_ui_layers.sh
./scripts/check_imports.sh
./scripts/check_packages.sh
```

The first command exercises actual renderers/exports and generated consumers;
the second only the existing Task contract/fake. Neither proves new browser
interaction, PostgreSQL isolation, real provider integration or all client themes.
For tenant and gallery integration retain the actual tests
`TestTenantIsolationIsEnforcedByPostgres` in kit/db and
`TestStorybooksUseOnlyTheAuthorizedTenantComposition` in modules/admin.
Run them with the repository's real test database, not a new private fake.

Each slice's IMPLEMENT report records paths, exact commands, results and all
unverified matrix cells, including new upstream assets' pinned versions and byte
cost. No test is weakened to pass. Price necessary budget growth separately before
landing the feature; the specify phase changes neither loc-budget.json nor
packages-budget.json. The program's current atomic-components indicator counts
downstream `css.Literal(` lines, so an unadopted kernel addition or specification
cannot lower it. Record the measured before/after values and the T-0126/app adoption
dependency rather than inventing a gain from the number of new constructors.
