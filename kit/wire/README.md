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
| B6 | Authorization declaration changes without an explicit allowance |

`CompareWithAllowances` and `GoldenWithAllowances` take an explicit
`[]AuthorizationAllowance` after their ordinary arguments. Each entry names exact
normalized `From`/`To` strings, a `ReviewedOn` date (`YYYY-MM-DD`) and a nonblank
`Reason`. Dates record review; they are not expiry checks. Invalid or duplicate
pairs refuse as B6 at `$allowances`. The package has no default policy. The
[reference caller](../../apps/platformkit/wire_compatibility_test.go) explicitly
allows `kind=signed_in` to `kind=any_credential`; reverse changes and permission
changes remain refused. Declare product allowances in the composition itself.

AsyncAPI operations use their top-level key as identity and action plus resolved
channel address as path. Send payloads are responses; receive payloads are
requests. Channel/message existence, message name/effective content type, channel
links and individual message links are checked, including unreferenced channel
and message records. Adding a message link preserves existing links. Local JSON
pointers decode `~0` and `~1`; missing or external references refuse.

## Composition

**Reused** — `breakingWireChanges`, `walkWire`, `refuseWireBreak`, `app.AsyncAPI`,
`deviceComposition` and the reference application's reviewer regression cases.
**Added** — structured breaks, explicit dated allowances and AsyncAPI projection,
because the previous test-private OpenAPI walker could not gate another
composition or interpret event documents.
**Made reusable** — one standard-library-only comparator and golden helper,
with independent fixtures and dependency enforcement in
[check_packages.sh](../../scripts/check_packages.sh).

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
refuse before document comparison. Unknown descriptive/extension keys are ignored.
All refusals are correctable by fixing the document, allowance or baseline.
No override launders a break. Inputs are not modified or retained; read-only
comparisons can run concurrently. Callers must serialize golden writers;
`os.WriteFile` errors may leave a partial file. Products own baseline review,
rendering, CI adoption, deprecation and installed-client verification. No downstream
application or device runtime is covered merely by importing this package.
