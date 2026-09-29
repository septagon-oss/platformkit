# Style engine

`ui/style` turns a component's declared classes into CSS rules. `New()` starts
a `ClassList`; its methods name utilities by role — `Padding(S4)`,
`Bg(SurfaceBrand)`, `FontSize(TextXL)` — over typed scales (`Spacing`,
`FontSize`, `Radius`, `BorderWidth`, `Color` and the rest), so a class that does
not exist cannot be written. `For(lists...)` resolves lists to a `css.Sheet`
with one rule per class; `Rules(classes...)` does the same for class names.
Read [classes.go](classes.go) for the builder, [compile.go](compile.go) for the
class names and [emission_resolve.go](emission_resolve.go) for their declarations.

Two sheets sit beneath the utilities: `ThemeVars(light, dark)` renders a
`design.Pair`'s tokens as custom properties with the attribute-over-preference
cascade, and `RoleVars()` defines the `--pk-role-*` properties the utilities
are written in terms of — declarations owned by
[`design/roles.go`](../../design/roles.go) and rendered here, so the list that
gates a client's `design.yaml` is the list this block is built from. A utility
never references a theme token directly, so
a role is the layer a browser paints and the layer a reader reads: the muted
text roles mix `text-primary` toward `text-muted`, two colours the contrast gate
certifies on every surface, rather than toward the surface one of them was
authored against — `BodyRolePairs()` names the pairs the components compose and
`design.Theme.CheckRoles` measures them, which [`ui/export`](../export/README.md)
runs before it emits anything and `design.Client.Resolve` runs on a client's own
file. `TintedRolePairs()` names the pairs whose
background this layer derives rather than takes from a theme — the brand tint is
`design.SoftTintPercent` of the accent mixed into the card surface, and the
accent's own colour is painted on it — and the export gates those too, because a
background mixed toward a foreground always reads worse than the surface it came
from. An element rule no class can express reads the
same scales back with `Value()` — `S8.Value()` is `"2rem"` — so an article and
the page around it share one scale (see [modules/web](../../modules/web/README.md)).
`Measurements`, `ScaleValues`, `ShadowValues`, `EasingValues` and
`TransitionValues` project the scales for design export.

It depends on [design](../../design/README.md) and [ui/css](../css/css.go)
only. `go test ./ui/style` checks that every enumerable class emits a rule,
that role variables reference declared tokens, and that values agree with rules.
