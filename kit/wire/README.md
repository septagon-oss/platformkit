# Wire compatibility

`kit/wire` gates a composition's published OpenAPI or AsyncAPI 3.x JSON without
linking its renderer, HTTP framework, database or any non-standard-library package.
Import `github.com/septagon-oss/platformkit/kit/wire` from the PlatformKit version
your application pins; [the executable example](wire_test.go) uses that public
import. Run `go test ./kit/wire` for the standalone contract cases.

```go
func TestDocument(t *testing.T) {
    wire.Golden(t, "testdata/openapi.json", func() []byte {
        return renderDocument(t) // your composition renders once and owns errors
    })
}
```

Check in an initial reviewed baseline manually. `Golden` reads it, calls
`Compare`, and reports every break before any write. A nonempty `UPDATE_GOLDEN`
rewrites a compatible document; a missing baseline or a break still fails.
Without the flag, even compatible byte differences fail with the first offset.
Use the same helper for an AsyncAPI document. The two reference composition tests
in [apps/platformkit](../../apps/platformkit/) use this lifecycle.

`Compare(golden, current []byte) []Break` is deterministic and does no I/O.
Every break carries `Rule`, `Path`, `Member`, and its existing diagnostic `Message`;
`String()` returns that message. Results sort by message, path, member, then rule.

| Rule | Refused change |
| --- | --- |
| B1 | Published operation identity removed |
| B2 | Published method/action and address removed or assigned another identity |
| B3 | Reachable member removed/retyped, structural link changed, or invalid JSON/profile |
| B4 | An existing request gains a required member |
| B5 | A request enum loses a value or a response enum gains one |
| B6 | Emitted declaration (`kind`, `permission`, `feature`, any other member) changes without an explicit allowance |
| B7 | The comparison did not finish reading one side: a schema past the traversal limit, a document past the node budget, or a constraint this profile does not read |

A comparison has three outcomes. The empty result states that both documents were read in full and
no rule fired. A B1–B6 row states that a published declaration moved. A B7 row states that this
comparison did not finish, so neither compatibility nor incompatibility was established. A B7 names
one side and one address, never a pair — there is no record on the other side to compare — and it
fires on two documents whose bytes are equal, because a diff of nothing is not a certification and a
golden gate that read one would rewrite the baseline from a document nobody had finished reading. It
arrives in that same list, in that same order, under the distinct prefix `B7 (incomplete):` so
nothing that reads `Bn (breaking):` reads it as a claim that a declaration changed. No allowance
covers it: an `AuthorizationAllowance` is a reviewed pair of declarations, and a B7 names no pair.

`CompareWithAllowances` and `GoldenWithAllowances` take an explicit
`[]AuthorizationAllowance` after their ordinary arguments. Each entry names exact
normalized `From`/`To` strings, a `ReviewedOn` date (`YYYY-MM-DD`) and a nonblank
`Reason`. Dates record review; they are not expiry checks. Invalid or duplicate
pairs refuse as B6 at `$allowances`. The package has no default policy. The
[reference caller](../../apps/platformkit/wire_compatibility_test.go) explicitly
allows `kind=signed_in` to `kind=any_credential`; reverse changes and permission
changes remain refused. Declare product allowances in the composition itself.

An operation's declaration is the whole object the server writes under
`x-platformkit-auth`: `kind`, the `permission`, the plan feature such a door names
with `.Needing`, and any other member it carries. The identity is all of it, spelled
`kind` then `permission` then `feature` then the rest by name, with an absent, null or
empty member contributing nothing. A name or a value that does not read as one member is
quoted, so one member cannot carry another's text: `"a=b": "c"` spells `"a=b"=c`, which is
not the `a=b=c` that `"a": "b=c"` spells. Adding, removing or renaming any member is
therefore a B6 change, and a permission that stayed put does not make a moved feature
not-a-change. A
pair is exact over that spelling: the reference allowance above covers a widening where
neither side names a feature, and a transition that also moves a feature needs its own
reviewed pair with the feature spelled on both sides — which the refusal prints, because
its message holds both whole identities. A pair written without a feature matches nothing
once its operation carries one, so the transition refuses as B6 naming the operation
rather than reading as the widening somebody reviewed.

AsyncAPI operations use their top-level key as identity and action plus resolved
channel address as path. Send payloads are responses; receive payloads are
requests. Channel/message existence, message name/effective content type, channel
links and individual message links are checked, including unreferenced channel
and message records. Adding a message link preserves existing links. Local JSON
pointers decode `~0` and `~1`; missing or external references refuse.

The shape of a schema node is one `key=value` text, and that text is what a B3
names. A number in it is spelled at its value rather than through a `float64`, so
equal values agree and different ones never do: `1`, `1.0` and `1e0` are one bound,
`0.1` and `1e-1` are one bound, and `9007199254740993` and `9007199254740992` are two.
Value equality is exact to decimal precision within a window of 512 significant digits
and an exponent within ten thousand; outside it a literal is compared as written behind a
leading `=`, which can refuse an equivalent re-spelling and cannot hide a change. A
keyword value keeps its JSON kind, so `maximum: 5` is not the `maximum: "5"` another
document spells.

An enum is a set: the order members are written in and any writing them twice say
nothing, and a member is quoted only when it cannot read back as exactly one member —
because it is empty, holds a space, a `|`, a quote or a backslash, opens an object or an
array, or would read back as a number, a bool or a null. So `["a|b"]` is one member and
spells `enum="a|b"`, which is not the `enum=a|b` that `["a","b"]` spells, and the empty
member of `["", "approved"]` is a member rather than nothing. A member or a declaration
value that is an object or an array keeps its JSON text; numbers inside one keep the literal
written, which can refuse a re-spelling and cannot hide a change.

A `type` is compared as the set it is. One name spells `type=string`; a set of names is sorted,
written once, and spelled `type=[array|null]`, each name spelled as one member; a set of exactly
one is that one name, so writing it as a set says nothing. So `["array","null"]` → `["array"]` and
`["string","null"]` → `["integer","null"]` refuse as a changed shape where they were silent, and
so does a set that gained a member: a set that grew is a different set here, and this gate takes
no position on whether a widened answer is safe for an old reader. `nullable` stays a separate
part with its own meaning. A `type` that names nothing — `[]`, an empty name, a value that is
neither a name nor a set of names — declares no type and refuses as an invalid profile rather than
reading as an open one. `minItems` and `maxItems` join the bounds the signature carries, spelled
by the same rule, so `minItems=1` is not the `minItems=2` another document states.

Whether anything a node does not name may answer is part of that node's shape. Only
`additionalProperties: false` closes the slot, and it spells `additionalProperties=false`, so
every move between closed and open — `true` → `false`, absent → `false`, `false` → `true`,
`false` → `{}` — changes the text and refuses as B3. An open slot is recorded as the empty
schema it is whether the document wrote `true` or wrote `{}`, and a written schema is
descended, so the two spellings of "anything may follow" are one contract and a change inside
a written open schema still refuses under its `{}` address. One asymmetry follows and is
stated rather than hidden: `absent` → `true` only adds that open-slot record, and a new record
that names no requirement produces nothing, so that direction is invisible, while
`true` → `absent` loses the record and refuses as B3 `… {} is gone`. One direction can refuse
an equivalent re-spelling and the other cannot hide a change, which is the direction every
other spelling rule in this package takes. An `additionalProperties` that is neither a boolean
nor a schema refuses as an invalid profile.

A request body the operation demands records `<operation> request body requires body` — the
same statement a required parameter makes with its own name — so the one rule that refuses a
request that gained a demand refuses both. A body that became required is B4; a body that
stopped being demanded is B3 `… requires body is gone`, the conservative relaxation, refused
for the reason a required parameter becoming optional is refused today. `required: false`,
`required: null` and an absent `required` are one state and record nothing, which is what
`omitempty` means on the way out. A schema-level `required` is compared as the list of names
it states; a list holding something that is not a name states a requirement no reader can
resolve and refuses as an invalid profile rather than comparing as the shorter list it would
silently become.

## Composition

**Reused** — `breakingWireChanges`, the one schema walk, `refuseWireBreak`, `app.AsyncAPI`,
`deviceComposition` and the reference application's reviewer regression cases; the plan
feature reaches the identity through the one `wireAuth` those two document kinds already
share, and the golden lifecycle, the `Break` shape and every diagnostic are unchanged. A
schema value is spelled by the same grammar that spelling already used for a declaration
member — quote a value only when it cannot read back as one member, in one helper both
kinds reach — so `wireSignature`, `wireAuth` and the enum segment share one rule rather
than agree on two. **Added** — structured breaks, explicit dated allowances and AsyncAPI
projection, because the previous test-private OpenAPI walker could not gate another
composition or interpret event documents; and, later, the whole declaration in that
identity, because a projection onto a list of key names cannot say what a door is once the
door carries a plan feature it never learned to read; and, later still, an exact text for a
number and an unambiguous spelling for a set of enum members, because a bound decoded
through a `float64` cannot tell `9007199254740993` from `9007199254740992` and members
joined with `|` cannot tell one member that holds a pipe from two members. The kernel held
neither: no numeric helper existed to compose, and the only lossless JSON precedent in the
tree keeps a `json.Number` as text without ever comparing values. **Made reusable** — one
standard-library-only comparator and golden helper, with
independent fixtures and dependency enforcement in
[check_packages.sh](../../scripts/check_packages.sh). The mutation table over an
authorization declaration — added, removed, renamed, reordered — is a pattern the
[reference contract test](../../kit/wire/wire_test.go) shows rather than a table another
tree can import: it lives in a `wire_test` test file, so a composition that wants it writes
its own rows over its own golden, as the
[reference caller](../../apps/platformkit/wire_compatibility_test.go) does. So is the table
over a value: a member that holds a pipe, an empty member, a numeric member, a bound past
what a `float64` can hold, each row stating the shape text and the named member as
literals, run through `Compare` and through the real golden subprocess from one list.

The application now keeps its renderer/setup and fixture inspection assertions;
it delegates all compatibility decisions and both file update paths to this
package. Reviewer tests retain their names and assertions.

## Limits

This is the existing emitted-profile gate, not a complete schema equivalence checker. OpenAPI
traversal keeps its operation-level parameters, body, response and schema-keyword coverage —
type as a name or a set, format, ref, enums of any member kind, bounds including `minItems`
and `maxItems`, nullable, properties, required, whole-body requiredness, items,
additionalProperties and anyOf — and the keywords it reads are now a closed whitelist rather
than a list of what happens to be read: a keyword a node declares and this gate neither
records nor descends refuses as B7 by name, whatever it is. The keywords that say nothing
about which instances satisfy a schema — `title`, `description`, `default`, `example`,
`deprecated`, `readOnly`, `discriminator`, `xml`, `$defs`, `$comment` and any `x-` extension
— are ignored by design, as a description always was. Everything else inside a schema the
walk reached is an unread constraint: `allOf`, `oneOf`, `not`, `if`/`then`/`else`, `const`,
`patternProperties`, `propertyNames`, `uniqueItems`, `multipleOf`, `contains`,
`dependentRequired` and the rest. Those are named, never analysed: no inference, no
subtyping, no property-level diff engine, and a keyword that later joins the read set is a
profile change owing this paragraph, a signature rule and cases of its own. It does not cover
arbitrary dialects, broker ACLs or security schemes outside `x-platformkit-auth`. It does not
reach a response header either: OpenAPI spells `headers` as an object and the loop over it
reads a list, so a header's name, its own shape text and the schema under it are recorded
nowhere and a header that retypes, renames or disappears is invisible on both sides. Request
enum growth and response enum shrink still refuse as B3 under the conservative signature
rule: an enum that gained a request value or lost an answer is a changed shape, not a B5.

Two limits bound one reading, and each states itself in the B7 beside the address it gave up
on. The depth of one schema path is 24 levels; the number of schema nodes one document's
traversal may visit is 200,000. Depth alone bounds neither the work nor the memory: a shared
component is expanded once per path that reaches it, and every record the walk writes embeds
the whole expanded path. The reference composition's document is 14 levels deep over 14,949
node visits and its traversal converges there — the same addresses at 14, at 24, at 128 — so
both numbers sit above what a real document asks with the work unchanged. A shape at the limit
is compared, and one past it refuses. Neither limit is an argument a caller
can raise, because a caller that could raise its own could certify what it never read; raising
one is a change to this package, written beside the measured depth and node counts of the
document it must still read in full. A walk that stopped is not a shape that changed, which is
why it is B7 and never B3, and why the traversal visits children in a fixed order — properties
by name, requirements, items, the open slot, `anyOf` as written, a reference last — so the
address two runs name is the same address and a fact a reviewer can check.

Malformed documents refuse as B3 at `$`, even when equal; malformed allowances
refuse before document comparison. Every member under `x-platformkit-auth` takes part
in the identity; an unknown descriptive or extension key is ignored where the walk does not
read a schema, and inside a reached schema a keyword outside the read set refuses as B7.
Declarations are compared, never judged: what may be declared — the five kinds, a
well-formed permission, a feature some plan sells — is `kit/httpx`'s decision at boot,
not this gate's. A declaration that is not an object is compared as `value=<its JSON>`,
and two members of one hand-written declaration can still share a spelling when one is
the JSON of the other (`true` and `"true"`); `kit/httpx` marshals strings only.
A B1–B6 refusal is correctable by fixing the document, the allowance or the baseline. A B7 is
correctable by flattening the shape, expressing the constraint in the read vocabulary, or
raising a limit here beside the measured numbers; a caller who will do none of those holds a
pair this gate cannot certify, which is what its message says. Neither kind is laundered:
no override, no flag and no allowance turns an unfinished comparison into a certificate. Inputs are not modified or retained; read-only
comparisons can run concurrently. Callers must serialize golden writers;
`os.WriteFile` errors may leave a partial file. Products own baseline review,
rendering, CI adoption, deprecation and installed-client verification. No downstream
application or device runtime is covered merely by importing this package.
