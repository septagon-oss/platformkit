# Components

`ui/components` renders the shared components as typed Go functions: Props in,
a gomponents `Node` out, no templates and no hidden state. The files partition
the library by role — [atoms.go](atoms.go) (buttons, inputs, badges, icons,
text), [molecules.go](molecules.go) (tables, cards, navigation, modals, tabs),
[sections.go](sections.go) (section headers, sections, heroes),
[layouts.go](layouts.go) (stacks, flex, grids, containers), [shell.go](shell.go)
(the application frame and the confirm dialog), [skeleton.go](skeleton.go)
(loading states) and [video.go](video.go). [props.go](props.go) holds every
Props contract, [classlists.go](classlists.go) every class list a renderer may
emit, and [layout_description.go](layout_description.go) what a layout
declares about its root.

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
