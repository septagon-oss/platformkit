# 0023: One Page contract for every shell

Status: accepted for the value contract in `kit/entity/view`. No renderer paints from a Page
yet: the delegation belongs to the extraction that replaces the markup decided inside
`ui/resource` and `ui/screens`, and until then this package is linked by no app.

## Problem

The page a person sees is decided twice today. `ui/resource` and `ui/screens` compute the
columns, the word for a status, the set of doors and the reading of a value while emitting
markup, and every other shell — a phone's native views, a terminal — computes them again from
the same description: two painters, the same inputs, no shared decision, both able to be green.
Nothing in `kit/` or `ui/` holds that decision today: the three `type Page struct` are an HTML
envelope and two products' records.

## Decision

`kit/entity/view` is a pure value package — the third in the line `kit/entity` →
`kit/entity/display` → `kit/entity/present` — that decides a screen once. `Build` takes a
`Description` (what the resource is, adapted by whoever holds the registration), the
presentation policy `present.Resolve` already resolved for one language, the authorised values
and the fixed presentation state, and returns one `Page`.

**Every key is written.** A Page never omits a member and never sends two shapes for one fact,
and an aggregate is never null.

**One version is read.** `PageVersion` moves when an existing member changes meaning, never for
an added one, and `SupportedVersion` says which revision this build reads. A shell finding a
`pageVersion` it does not support paints nothing and keeps the entry it already reads, and
`Build` refuses a resolved policy it cannot read the same way.

**The value domain is closed and named.** Every value carries its kind (`ValueKind`) beside its
spelling, so a builder switches on the Page and never on its own guess:

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

An integer that arrived as a `float64` is one some caller already decoded as a float, so it is
written `decimal`: the digits are the ones that survived that decode. A non-finite number has
no honest spelling at all, so it is `raw`: a read is never lost over what JSON cannot hold.

**Refusals name a member.** A build that cannot make a page returns the zero Page and a
`Refusal{Field, Want, Got}` naming `policyVersion`, `kind`, `language`, `builtAt`, `resource`,
`total`, `window.limit`, `operations`, `record` or `writable`. How a caller branches on `Field`,
that every refusal is correctable, and what is never checked here are `Refusal`'s own comment.

**The address stays with the painter.** No route, screen, write path or href reaches a Page: a
Page that repeated the catalogue entry would disagree with it. A `Ref` names a record
semantically — resource and id — so a shell can name a control after it and survive a row
reorder and a translation.

**Two differences from the renderers, stated rather than folded in.** A renderer humanises what
nobody declared: `display.EnumWord` answers "In progress" for an enum value no author named, and
a date is cut nowhere at all. This package does neither — an undeclared enum value keeps its own
text and a `neutral` tone, and a day is cut in the caller's zone.

**The contract is published for the shells that cannot read Go.**
`kit/entity/view/page-v1.schema.json` is the Page's JSON Schema, written by `tools/pageschema`
from these types through `kit/events`' own projection and owned by `make generate`.

## Consequences

- The resource adapter that fills `Description` from the HTTP resource description is another
  module's to write, as is the adapter that fills the rendered resource today: a pure contract
  cannot name a transport.
- The package introduces no copy. A CRUD door carries no `Label`, and "New %s" is the shell's
  phrase composed from `Kind` and the policy's own singular name.
- Byte-identity between a Page and the markup a renderer produces today is not claimed here; it
  is the claim of the extraction that hands the renderers over to this contract.
