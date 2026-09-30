# 18. Cascade layers decide precedence, and only the client layer is a consumer's

Status: accepted, 2026-09-29. Answers the evaluation the task's brief asked for and
that this repository had not written down: the three candidates the design floor was
chosen between, each on the four questions the brief names. The requirement the
program's decision record 0044 puts on any such choice is that it support `@layer` —
"a requirement of the choice, not a nice-to-have". The runtime contract is
[ARCHITECTURE.md](../../ARCHITECTURE.md)'s Web forms and pages row; the code is
[ui/ui.go](../../ui/ui.go), [ui/css](../../ui/css/) and
[ui/hooks.go](../../ui/hooks.go).

## Context

`ui.Compose` returns one stylesheet, and until this change every rule in it was
unlayered, so a rule's rank was its position in the file. A consumer that arrived
later — a module's `Extra.Sheets`, a client's own rules — could restyle a kernel
component with a one-class selector, and no ordering fixes that: the composition is
the list of modules an application selects, so the same sheet in another order is a
different file. Cascade layers say the ranking in the sheet instead of leaving it to
be observed. Cascade layers had been removed from the package `ui/css` derives from;
this change puts them back, which is why [ui/css](../../ui/css/) says so in its
package comment rather than leaving the omission to be rediscovered.

## The four questions, answered for each candidate

The brief's questions: are the typed gomponents components kept; does a client get one
layer with no way to override a kernel role; what does the application's shipped CSS
weigh; what does a client's `homepage.css` become.

**(a) Cascade layers in `ui/css`, emitted by `ui.Compose`.**

- *Typed components:* kept unchanged. `ui/style` still resolves each component's class
  list once and `ui.Compose` still assembles exactly one sheet, so a component's
  classes remain Go values checked at build time, with no string that only a browser
  understands.
- *One layer a client cannot override a kernel role from:* yes, and by the gate
  rather than by the ranking. The sheet opens `@layer tokens, base, components,
  client;` and emits nothing outside those four blocks, which makes the ranking a
  statement in the artifact instead of a file position — but for normal declarations
  a *later* layer wins (CSS Cascade Layers §6), so the layer a client writes is the
  strongest of the four and not the weakest, and a layer's rank outranks every
  selector. What keeps a client off a kernel role is `refuseClientSheet`, which
  refuses a client rule that names the kernel's own vocabulary — an attribute it
  renders, a class its markup carries, a comparison of the contents of the
  `class` attribute (the same namespace reached by value: `[class~="sr-only"]`
  matches what `.sr-only` matches), the root element, a `--pk-` property, a raw
  colour — read as a name and not as bytes, in the spelling a browser resolves to
  that name, a colour in its hex form or in any functional notation it computes
  one from (`rgb()`/`hsl()`, `lab()`/`lch()`, `oklab()`/`oklch()`, `hwb()`,
  `color()`, `color-mix()`; a named colour such as `red` is a word, and the gate
  names no word list; the colour read is made over the value a browser computes,
  with a `url()` argument and a quoted string stepped over because neither
  computes one), and over the whole sheet the composition emits: a class a
  module hands `Extra.Lists` resolves into `components` too, so the gate reads the
  composition's classes rather than one package's declaration list. `!important` is the one declaration spelling that crosses a layer the
  other way, which is where the kernel's own overrides sit, and the one style meant
  to outrank the layers is unlayered on purpose (the tenant accent `modules/web`
  pins to `#rrggbb`).
- *Shipped CSS:* measured at this revision, `ui.Compose(design.Default()).Body` — the
  kernel's own sheet, before any module's `Extra` — is 31,866 bytes, `sha256` prefix
  `83431909d6a6332f`. The five lines that state the ranking — the order statement and
  the four `@layer … {` openings, newlines included — are 107 of them, 0.34%, and take
  no account of the indentation the blocks add to the rules inside them. The delivery
  those lines came with is larger than they are: the stylesheet this application's own
  page links is 30,369 bytes on `origin/main` and 33,407 at this head — 3,038 bytes,
  10% — and that is `componentState`'s rules as much as the layering.
- *A client's `homepage.css`:* its rules reach the browser as a value the kernel built
  — `ui.Extra`, which `Compose` places in `client`. A sheet served as a static file
  beside the application's is outside the contract whatever this ADR says, because
  nothing reads it; that conversion is the cost of this option, and it is real. The
  products checkout's exported sheet builders were swept at the previous round, in
  that checkout and by that round: some of them are refused by the new gate, on raw
  colours and on selectors naming a kernel-rendered attribute. What a name covers has
  since widened to the classes the kernel's own markup carries, so a sheet that
  restates one of the kernel's utilities refuses now where it composed before. The count and the per-client breakdown belong to the sweep's own record and
  to a checkout this repository cannot read, which is why neither is quoted here.
  That sweep is a floor, not an inventory, and it is not this repository's to fix.

**(b) Tailwind v4 as a standalone build, with the design tokens declared as `@theme`.**

- *Typed components:* kept as Go, but their `class` attributes become utility strings
  `ui/style` never sees, so the build no longer checks that a class exists; the
  resolution that makes a class list a Go value is the thing this option gives up.
- *One layer a client cannot override from:* available — Tailwind v4 emits layers of
  its own — but the layer set is the toolchain's, not the kernel's, and the kernel's
  components would be placed inside it by a build step outside this repository.
- *Shipped CSS:* not measured. Nothing in this repository builds a Tailwind sheet, and
  a size for it would have to come from a prototype nobody wrote. Say plainly: this
  evaluation compares (a) measured against (b) argued.
- *A client's `homepage.css`:* becomes utility classes in the markup, which moves the
  client's brand out of a stylesheet and into its components — a second migration, in
  the direction away from the product's own templates.

**(c) Open Props.**

- *Typed components:* kept; Open Props is a published set of custom properties, not a
  renderer, so it changes nothing about the components and nothing about where a rule
  ranks.
- *One layer a client cannot override from:* no. Custom properties decide values, not
  precedence, so the bug this task is about survives option (c) intact.
- *Shipped CSS:* not measured; the same answer as (b), for the same reason.
- *A client's `homepage.css`:* unchanged in shape, with a second property vocabulary
  beside the `--pk-` one that the role variables and every `ui/style` rule read.

## Decision

(a), in the shape [ARCHITECTURE.md](../../ARCHITECTURE.md) describes. It is the only
one of the three that answers the second question, it is the only one this repository
can measure, and it costs no toolchain: `make build` remains
`go build`, and no package directory appears in the branch's diff, with
`./scripts/check_packages.sh` answering 75 of 76. (c) is
additive to any of the three and remains open as a token source for the `tokens`
layer; (b) was refused for what it gives up rather than for what it costs, and if a
future brief wants the utility engine it wants a prototype's measurement first.

## What this decision does not settle

Decision record 0044's second amendment asks for more than this: "The stylesheet stops
being a Go value", with Tailwind's standalone binary and Open Props named as the
candidates and `@layer` support as the requirement any answer must meet. This branch
discharges the `@layer` half — the at-rule family is back in the package that is
remembered for removing it — and leaves the other half open. Recording that in the same
place as the comparison is the point: (a) was chosen because it answers the second
question at a cost this repository can measure, and it is not the same choice as
adopting a stylesheet solution, whatever the shared `@layer` letter suggests.

## Consequences

A consumer sheet is refused at composition rather than ranked by accident, which
breaks consumer sheets that relied on source order — the sheets the sweep above
counts are those, and each refusal names the rule and what to write instead. The
layer order is what makes the ranking readable; it is not what protects a kernel
role from a client rule, because the client layer ranks last and therefore wins,
and the protection is the gate's read of the kernel's two name vocabularies. The
read cannot reach a bare type selector: `dialog { display: block }` names neither
an attribute nor a class and reaches the modal's own rule from the layer that
outranks it, which is stated as the limit rather than closed, because refusing
every element selector would shut the client layer for the `a { color:
var(--pk-color-accent-default) }` rule that is a client's own business. `ui/css` is
now the owner of the at-rule families the design system uses (`@layer`, `@media`,
`@keyframes`) and still emits no parser, no minifier and no diagnostics.

## What this record does not measure

No Tailwind or Open Props artifact was built, so their shipped sizes, their layer
behaviour in a browser and the effort of the utility migration are unevaluated here;
the sweep of client sheet builders belongs to the previous round's report and to the
products repository, and this repository holds 69 `css.Literal(` call sites before this
change and the same 69 after it, all inside `ui/`, none in a consumer sheet.
