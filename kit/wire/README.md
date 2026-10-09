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
empty member contributing nothing. Adding, removing or renaming any member is therefore a
B6 change, and a permission that stayed put does not make a moved feature not-a-change. A
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

## Composition

**Reused** — `breakingWireChanges`, `walkWire`, `refuseWireBreak`, `app.AsyncAPI`,
`deviceComposition` and the reference application's reviewer regression cases; the plan
feature reaches the identity through the one `wireAuth` those two document kinds already
share, and the golden lifecycle, the `Break` shape and every diagnostic are unchanged.
**Added** — structured breaks, explicit dated allowances and AsyncAPI projection, because
the previous test-private OpenAPI walker could not gate another composition or interpret
event documents; and, later, the whole declaration in that identity, because a projection
onto a list of key names cannot say what a door is once the door carries a plan feature it
never learned to read.
**Made reusable** — one standard-library-only comparator and golden helper, with
independent fixtures and dependency enforcement in
[check_packages.sh](../../scripts/check_packages.sh), and a mutation table over an
authorization declaration — added, removed, renamed, reordered — that any composition can
run its own golden through.

The application now keeps its renderer/setup and fixture inspection assertions;
it delegates all compatibility decisions and both file update paths to this
package. Reviewer tests retain their names and assertions.

## Limits

This is the existing emitted-profile gate, not a complete schema equivalence
checker. OpenAPI traversal keeps the existing operation-level parameters, body,
response and schema-keyword coverage: type, format, ref, string enums, bounds,
nullable, properties, required, items, additionalProperties and anyOf, bounded
at depth 12. It does not cover arbitrary dialects, allOf/oneOf, broker ACLs or
security schemes outside `x-platformkit-auth`. Request enum growth and response
enum shrink still refuse as B3 under the original conservative signature rule.

Malformed documents refuse as B3 at `$`, even when equal; malformed allowances
refuse before document comparison. Every member under `x-platformkit-auth` takes part
in the identity; an unknown descriptive or extension key anywhere else is ignored.
Declarations are compared, never judged: what may be declared — the five kinds, a
well-formed permission, a feature some plan sells — is `kit/httpx`'s decision at boot,
not this gate's. A declaration that is not an object is compared as `value=<its JSON>`,
and two members of one hand-written declaration can still share a spelling when one is
the JSON of the other (`true` and `"true"`); `kit/httpx` marshals strings only.
All refusals are correctable by fixing the document, allowance or baseline.
No override launders a break. Inputs are not modified or retained; read-only
comparisons can run concurrently. Callers must serialize golden writers;
`os.WriteFile` errors may leave a partial file. Products own baseline review,
rendering, CI adoption, deprecation and installed-client verification. No downstream
application or device runtime is covered merely by importing this package.
