# 0023: One Page contract for every shell

Status: accepted for the value contract in `kit/entity/view`. No renderer paints from a
Page yet: the delegation belongs to the extraction that replaces the markup decided
inside `ui/resource` and `ui/screens`, and until then this package is linked by no app.

## Problem

The page a person sees is decided twice today. `ui/resource` and `ui/screens` compute the
columns, the word for a status, the set of doors and the reading of a value while emitting
markup, and every other shell — a phone's native views, a terminal — must compute them
again from the same resource description. Two painters with the same inputs and no shared
decision can disagree, and both can be green. No value in `kit/` or `ui/` holds that
decision: the three existing `type Page struct` are an HTML envelope and two products'
records.

## Decision

`kit/entity/view` is a pure value package — the third in the line `kit/entity` →
`kit/entity/display` → `kit/entity/present` — that decides a screen once. `Build` takes a
`Description` (what the resource is, adapted by whoever holds the registration), the
presentation policy `present.Resolve` already resolved for one language, the authorised
values and the fixed presentation state, and returns one `Page`. It reads its arguments and
writes none of them, performs no I/O, reads no clock but the one it is given, and is safe
for concurrent use.

**Every key is written.** A Page never omits a member and never sends two shapes for one
fact: the only nulls in it are field values, `""` and `[]` mean none, and an aggregate is
never null. A shell that has to guess what an absent key meant is deriving again, which is
what this type exists to end.

**One version is read.** `PageVersion` is the revision of a Page's own meaning and moves
when an existing member changes meaning, never for an added one. `SupportedVersion` says
which revision this build reads; a shell finding a `pageVersion` it does not support paints
nothing from the Page and keeps the entry it already reads. `Build` refuses a resolved
policy it cannot read the same way: a page assembled from a policy this build does not
understand is not a page.

**The value domain is closed and named.** Every value carries its kind (`ValueKind`) beside
its spelling, so a builder switches on the Page and never on its own guess about what a
JSON member was:

| Kind | Spelling |
|---|---|
| `absent` | `null`, with the policy's own missing word in `text` |
| `string`, `boolean`, `picture` | as stored, through `display.Text` |
| `integer` | a JSON number, only while `|v| ≤ 2^53-1` |
| `bigInteger` | a canonical decimal string of the same digits, past that bound in either sign |
| `decimal` | a canonical decimal string, never an exponent |
| `money` | `minorUnits` as such a string with `currency` and `scale`; never a double |
| `instant` | RFC 3339 UTC with exactly six fractional digits |
| `date` | that calendar day cut in the zone the caller loaded, which is printed on the Page |
| `list`, `map` | `[]` and `{}` for none; a map's keys in the order `encoding/json` writes |
| `raw` | a value JSON cannot spell: `value` null, `text` what is stored |

An integer that arrived as a `float64` is one some caller already decoded as a float, so it
is written `decimal`: the shape stays safe and the digits are the ones that survived that
decode. A non-finite number has no honest spelling at all, so it is `raw` — a read is never
lost over a value JSON cannot hold.

**Refusals name a member.** A build that cannot make a page returns the zero Page and a
`Refusal{Field, Want, Got}`: `policyVersion`, `kind`, `language`, `builtAt`, `resource`,
`total`, `window.limit`, `operations`, `record`, `writable`. A caller branches on `Field`
with `errors.As` or `errors.Is` and never parses a sentence, because a page that is not
built is answered by the shell's own state, not by this package's prose. Each is
correctable: the caller reaches a page by changing what it passed. No permission grammar,
business rule or schema declaration is checked here; each is refused by whoever owns it, and
a page grants nothing either way.

**The address stays with the painter.** No route, screen, write path or href reaches a Page:
a Page that repeated the catalogue entry would be disagreeable with it. A `Ref` names a
record semantically — resource and id — so a shell can name a control after it and survive
a row reorder and a translation.

**Two differences from the renderers, stated rather than folded in.** A renderer humanises
what nobody declared: `display.EnumWord` answers "In progress" for an enum value no author
named, and a date is cut nowhere at all. This package does neither — an undeclared enum
value keeps its own text and a `neutral` tone, and a day is cut in the caller's zone — so a
shell's change is reviewed as the change it is.

**The contract is published for the shells that cannot read Go.**
`kit/entity/view/page-v1.schema.json` is the Page's JSON Schema, written by
`tools/pageschema` from these types through `kit/events`' own projection and owned by
`make generate` and `check-generated`; the phone's builder validates against the same file.

## Consequences

- The resource adapter that fills `Description` from the HTTP resource description is the
  other module's to write, beside the adapter that fills the rendered resource today: a pure
  contract cannot name a transport, and the seam is not this package's to own.
- The package introduces no copy. A CRUD door carries no `Label`, and "New %s" is the
  shell's phrase composed from `Kind` and the policy's own singular name.
- Byte-identity between a Page and the markup a renderer produces today is not claimed here;
  it is the claim of the extraction that hands the renderers over to this contract.
