# Components

`ui/components` renders the shared components as typed Go functions: Props in,
a gomponents `Node` out, no templates and no hidden state. One file per
component, named after it, so a reader looking for `Card` opens
[card.go](card.go) — [alert.go](alert.go), [avatar.go](avatar.go),
[badge.go](badge.go), [breadcrumb.go](breadcrumb.go), [button.go](button.go),
[card.go](card.go), [checkbox.go](checkbox.go),
[detail_list.go](detail_list.go), [divider.go](divider.go),
[empty_state.go](empty_state.go), [heading.go](heading.go), [icon.go](icon.go),
[input.go](input.go), [label.go](label.go), [link.go](link.go),
[media.go](media.go), [modal.go](modal.go), [pagination.go](pagination.go),
[select.go](select.go), [sidebar.go](sidebar.go), [spinner.go](spinner.go),
[table.go](table.go), [tabs.go](tabs.go), [text.go](text.go),
[textarea.go](textarea.go) and [helpers.go](helpers.go), which holds the two
helpers more than one family needs. The files that hold a group rather than one
component say so — [sections.go](sections.go) (section headers, sections, heroes),
[layouts.go](layouts.go) (stacks, flex, grids, containers), [shell.go](shell.go)
(the application frame and the confirm dialog), [skeleton.go](skeleton.go)
(loading states) and [video.go](video.go). [props.go](props.go) holds the original Props contracts; the larger shared
families keep their contracts beside their renderers. [classlists.go](classlists.go) holds the class lists a renderer may
emit, and [layout_description.go](layout_description.go) what a layout
declares about its root.

Every renderer takes its Props struct and returns a gomponents `Node`, and an
unknown variant or size string falls back to the documented default rather than
failing — the contracts' "data schema, not behavior" stance. Interaction
contracts stay in the shared runtime controllers, so a downstream product needs
no private script and no duplicate markup.

The rule that makes the stylesheet a Go value: a class no list declares gets no
rule, so styling lives in class lists and `ui.Compose` resolves exactly those.
Labels a screen reader hears — `Spinner.Label`, `Pagination.PreviousLabel`,
`ConfirmDialog.AcceptLabel`, `Alert.DismissLabel` and their siblings — are Props
with the authored English as defaults, so a localized shell supplies its own
words through the same typed contract.

One label deliberately has no default. `Table.Label` names the scroll wrapper so
that a table wider than its box can be reached with a keyboard alone; a name
invented by the renderer would be the renderer claiming to know what its
caller's table is about. A table nobody names therefore stays an ordinary scroll
box, exactly as it was, and [ui/resource](../resource/resource.go) names the
generated list after its own heading. [e2e/design-audit.spec.ts](../../e2e/design-audit.spec.ts)
refuses a scroll box that cannot take focus, so the affordance cannot quietly go
missing again.

The package imports [ui/style](../style/README.md) and [ui/icon](../icon/icon.go)
and nothing that reflects. Capturing an invocation, describing its Props as
JSON Schema and the `Gallery` of one example per component live in
[ui/components/examples](examples/example.go), which imports this package.
Adding a component means a renderer, its class lists and a Gallery entry;
`go test ./ui/components/...` proves every emitted class resolves to a rule,
and the [admin gallery](../../modules/admin/README.md) shows the result.

[Shared web components](shared-web-components.md) specifies the intended T-0181
additions, reuse, contracts and acceptance cases. Rich states, DataList, detail panels and Timeline are
implemented. The remaining families now have typed constructors and explicit
English/Portuguese Gallery states; their intended acceptance cases remain in
the specification and their executed checks in the phase report.

## Rich states

[EmptyState](empty_state.go) accepts `Text` and an optional `Action` using
`ButtonProps`. `Text` takes precedence over the compatible `Description` field.
Use either `Action` or the existing `Actions` slot: supplying both returns a
render error before any component markup. Existing zero-value calls still work.

[Notice](notice.go) reuses Alert for failures, offline information, refusals and
success. Supply localized `Text`, an optional `Title`, and `Tone` (`danger`,
`warning`, `info`, `ok`). `ok` uses Alert's success styling. The default live
announcement is polite; `Live: "assertive"` is for urgent errors and `"off"`
is a non-live note. Existing Alert calls retain their tone-based defaults.

```go
components.Notice(components.NoticeProps{
    Text: "Não foi possível carregar os itens.",
    Action: new(components.ButtonProps{
        Label: "Tentar novamente", Href: "/items",
    }),
})
```

Translations belong to the composing request. `NoticeProps.Validate` rejects
missing copy, unknown behavioral values and unnamed actions. `Node.Render`
uses the same validation; `document.Render` discards the document on error.
Actions use native links or buttons; a submit button can name its form through
`ComponentProps.Attrs["form"]`. A pending action is disabled without changing
the caller's props. Trusted `NoticeSlots.Actions` can supply a full native form.

A dismissible Notice requires `DismissLabel`. The shared controller reveals its
dismiss button, removes only that notice and restores focus to the next usable
control. Set `ComponentProps.Attrs["data-alert-return-focus"]` to a control ID
to restore a specific associated control instead. Without JavaScript the notice
persists and dismissal is hidden; its native recovery links/forms still work.
No notice has a timer, automatic retry or persisted dismissal.

`Skeleton` and `TableSkeleton` remain decorative geometry. Give the containing
loading region its localized name and `aria-busy`; do not announce every cell.
The existing Gallery includes the rich-state cases in English and Portuguese,
including long copy, pending recovery, refusal, offline, success and loading.

## Composition

**Reused** — EmptyStateWithSlots, AlertWithSlots, Button, Skeleton, TableSkeleton,
Stack, TableWithSlots, Pagination, ModalWithSlots and its content frame, the shared
component controller and the
existing Gallery/export pipeline.
**Added** — Notice and canonical EmptyState Text/Action properties supply the
mobile naming and validated recovery composition the existing Alert API lacked;
DataList supplies grouped navigation and native selection that Table alone lacked.
DetailSheet and SidePanel add validated snapshot states and placement to the shared
Modal frame, with modal and nonmodal semantics respectively. Timeline composes
existing Table/DetailList for ordered event display; no existing event-list API
carried UTC instants and explicit redacted changes.
**Made reusable** — localized rich-state examples and browser checks for notices,
44px controls, no-JavaScript recovery, dismissal focus, page-scoped selection and
late-response refusal for a replaced or closed detail selection, and ordered
redaction-safe event display.

## Data lists

[DataList](data_list.go) composes one Table per group, server-supplied counts,
filter/sort/view links and an optional Pagination. `DataListSlots` retains Table's
rich cells and server sorting callbacks, adds row and bulk actions, and keeps
collection command forms in `Footer` before the pager. Generated
[resource lists](../resource/resource.go) use this composition while retaining
their schema projection, authorized native forms and server sorting. They do not
enable bulk mutation through generic CRUD.

Supply a unique instance `ID`, `ResultKey`, `SelectionName`, `FormID`, localized
selection labels and one `SelectionCountLabels` entry for each eligible count,
including zero. Row IDs are unique across groups. The supplied `CountText` can
include unloaded results; selection includes only this page's eligible rows,
including collapsed groups. Native checkboxes associate with the named form and
submit without JavaScript. The controller adds select-all, mixed state, clear
and a single translated count announcement. A new result key clears selection
even for IDs still present and prevents form reset from resurrecting it.

`ContentState` uses ready/loading/empty/failed/refused. Explicit ready/offline may
show a caller-approved snapshot and its `OfflineText`; no retry runs on its own.
Absent states must contain no groups or selected IDs, including in captures.
Failed may show `RetryAction`; refused never does. Empty remains a successful
read and may retain collection commands and pagination, including a bookmarked
page beyond a total reduced by deletion. Empty copy stays outside the table scroll
region. LoadingLayout supplies row/group/action geometry without retaining data. All state copy and
pagination labels belong to the caller. Validation errors return no component
bytes. A row's opaque Revision is only passed to its caller-owned action slot;
the component never generates revision fields or treats a checkbox as authority.

A failed list clears the same fields — filter/sort/view choices, result and
selection metadata, pagination, HTMX controls and every slot but its own retry —
and a refused list additionally clears columns and that retry control. Retaining
any of these rejects both HTML rendering and typed example capture before export.

## Detail panels

[DetailSheet and SidePanel](detail_panel.go) share Modal's header/body/action frame.
Supply a unique ID, localized Label/CloseLabel, ReturnHref and ContentState. Ready
requires ItemID and Title. Loading/empty/failed/refused require cleared item ID,
title and description; retained Body/Header/Actions slots are refused before
rendering or typed export. RetryAction appears only for failed, EmptyAction only for empty.

DetailSheet is a native modal dialog at every width. Placement is bottom, end,
or auto (bottom below 48rem and end above). SidePanel is a named aside and never
traps focus or makes the list inert. Hidden side panels expose no active controls.
Body scroll and wrapping actions share the existing Modal classes. At large text
sizes the body keeps a minimum readable height and excessive chrome scrolls in
the frame instead of covering the last field. Busy sets aria-busy; caller actions
supply their own pending/disabled state, and Escape remains available by default.

An opener is a real detail-page link. Set its `data-detail-open` to the panel ID;
without JavaScript the href remains the complete route to detail. The return link
uses ReturnHref without JavaScript. Enhanced opening focuses the panel, and close
returns to the opener. Set panel `Attrs["data-detail-return-focus"]` to the invoking
list heading ID (with tabindex=-1) for fallback when that row disappears.

An HTMX GET targeting the panel can replace its content or its root. The shared
controller admits only the latest request in that opening and rejects a response
arriving after close or replacement. Detail responses apply synchronously with
that check; caller swap delays and view transitions are disabled for this target.
Modified link activation retains native browser navigation. Server responses still require authorization;
request tracking is a presentation guard. Dirty forms use the existing caller
confirmation contract. The component does not save drafts or own a detail route.

## Timeline and audit display

[Timeline](timeline.go) renders caller-ordered events as an ordered list or an
`audit` Table. Actor text, time display, summary, before/after labels and optional
continuation links are caller-localized. `TimeText.AtUTC` is a nonzero `time.Time`
in UTC; `Text` is its supplied display projection. Native time elements retain
RFC 3339 machine values. Equal timestamps never reorder the supplied events.

Changes expand with native details/summary and compose DetailList. An unset
before/after value needs localized display copy from the caller. Redacted changes
require RedactedText and reject any before/after payload before rendering or
capture. Failed/refused states reject retained items and continuation URLs.
The optional portrait is decorative beside ActorText and creates no extra link.
No event, identity, audit write or relative-time clock is invented here.

The example owner explicitly supports the standard `time.Time` RFC 3339 string
codec, date-time schema, typed edits and copyable `time.Date` expressions.
Arbitrary custom codecs remain refused. Expanded disclosure is reached through
native interaction in the Gallery/browser case; it is not persisted component
state. No Timeline operation writes an audit record.

## Workflow and availability

[Stepper](workflow.go) displays the supplied current/completed/error/disabled
steps and a native progress element. Its body and action slots compose a real
Form with explicit form associations; Back, Continue and SaveExit remain caller
commands. Busy disables fields, while SaveStatusText can report a failed save
without discarding entered fields. Stepper never advances on its own.

[DateStrip and SlotPicker](slots.go) distinguish civil dates from UTC intervals.
Supply the IANA zone, explicit NowUTC and localized status/offset/capacity labels.
SlotEligibility applies stale, booked, past, unavailable and full precedence;
unknown capacity stays unknown. Selecting a native radio does not reserve a seat.
An unavailable selection requires ErrorText and renders unchecked.

[Calendar](calendar.go) always renders a chronological agenda of native links;
the day/week grid draws the same ones, from the addresses the renderer hands the
engine beside each event. A disabled view hands it none, so nothing in the view is
followable. Timed intervals intersect local civil days with exclusive ends; all-day
events use exclusive civil dates. Day/week grids load the vendored FullCalendar
Standard 7.1.0 only when needed. SSR navigation supplies the range; the engine does
not fetch, drag, resize, book or run a browser-clock decision. A range is limited to
366 civil days, and the strip's selected day — the one it marks `aria-current`
and the grid opens on — must fall inside that half-open range, so a composition
whose selection belongs to another range renders nothing. The optional engine is
destroyed when its HTMX root is removed.

## Commerce and plans

[ProductCard, BuyBar, Cart and OrderSummary](commerce.go) render caller-authorized
snapshots. Money is int64 minor units plus a Currency. MoneyText includes caller
formatted and accessible text. LineTotal and OrderTotal check overflow, currencies
and supplied totals. Included amounts do not get added twice; an incomplete
summary never invents a zero total. Stale, incomplete, pending or unavailable
carts suppress checkout. LineActions receives the line revision for a caller-owned
form; the renderer does not generate a command or claim a successful purchase.
BuyBar uses sticky positioning within its containing scroll area and safe-area
padding. It never overlays a separately positioned application keyboard/footer.

[OptionChips and QuantityInput](choice_controls.go) use native radios and a labeled
text input. QuantityInput accepts only the supplied nonnegative step lattice and
never clamps to an ineligible maximum. Enhancement uses BigInt, emits one change
per button action, and gives invalid entries the caller's ValidationText. Without
JavaScript the input remains a native submitted string; the authoritative command
must validate its range and revision. Money and int64 quantities serialize as
canonical decimal strings in typed captures, preserving values beyond 2^53.

[PricingTiers and PlanComparison](plans.go) share exact supplied prices. Billing
periods are navigation links, not client-side price arithmetic. Current and
recommended are independent; unknown feature cells have explicit text. Comparison
uses column and row headers plus native per-plan disclosures for narrow layouts.
No tax, discount, exchange-rate, rounding or jurisdiction policy lives here.

## Maps, media and charts

[MapView](map_view.go) retains its DataList in both modes. The optional Leaflet
1.9.4 adapter projects only supplied points and makes each address it is handed a
native marker link; handed none — as a disabled view hands it none — its pins are
named places that follow nothing, and coincident points stay reachable in the list.
Tiles require a caller-owned same-origin URL template and attribution; this package
provides no tile service or credential.
Failed tiles keep an explicit fallback. Polar coordinates remain exact in the list
without a relocated Web Mercator marker. DetailSheet composition is optional and
must match the selected authorized point. `SelectedID` itself must name one of the
supplied points; stale IDs are rejected before rendering. No selection is persisted globally.

[PhotoGallery and Masonry](photos.go) reuse Media and Modal. Declared image dimensions
reserve image geometry; full images remain in inert templates until selected.
Viewer arrows/Home/End stop at the supplied ends; Escape restores the thumbnail.
A removed selection closes the viewer. Masonry follows native column reading order
with bounded responsive columns and an ordinary More link. Hero's full-bleed layout
puts media across its containing section and bounds the copy independently.

[Charts](charts.go) render bounded SVG and accessible text. Area and bar charts
include native data-table disclosures; series have labels, tones and dash patterns.
Pure scale functions reject nonfinite values, retain signed zero baselines, expand
flat ranges, and disconnect gaps. StatTile's direction and tone are caller-supplied;
no business meaning, relative-time clock or trend conclusion is inferred.

## Limits

Calendar refuses a range whose local-day boundaries normalize to another civil
date, including dates skipped by a time-zone transition. It returns an error
before rendering or export; the caller supplies another range or recovery page.

These renderers receive already authorized data and supplied messages. They do
not fetch, authorize, save or retry writes, infer connectivity, keep snapshots,
or decide domain success. Request recovery remains owned by
[document.RequestNoticeExamples](../document/document.go). Client-theme acceptance and T-0180 parity require their owners' actual theme
pairs and landed mobile contracts. Browser proofs cover Chromium, native keyboard
behavior, automated accessibility and reflow; manual assistive-technology speech,
Firefox/WebKit and a numerical CLS score are not established. DataList has explicit
loading geometry; other aggregate loading states announce progress and reserve
skeleton space, without promising identical height for arbitrary caller content.
The booking/cart producer still owns actor grants, tenant transactions, expected
revisions, idempotency, uncertain writes and atomic outbox events. Gallery examples
are presentation fixtures, not production booking or checkout services.
