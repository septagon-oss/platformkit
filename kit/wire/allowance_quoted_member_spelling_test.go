package wire_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/wire"
)

// A reviewer writes an allowance in the identity's canonical spelling, so the spelling a
// quoted member gets is a contract: a pair written as `"a=b"=c` has to keep covering the
// member named a=b, and must cover neither the plain member a nor its own reverse.
func TestAnAllowanceOverAQuotedMemberNamesItsExactSpelling(t *testing.T) {
	for _, kind := range []string{"openapi", "asyncapi"} {
		t.Run(kind, func(t *testing.T) {
			declare := func(authKind, key, value string) []byte {
				return mutated(t, baseline(kind), func(doc map[string]any) {
					operation(doc, kind)["x-platformkit-auth"] = map[string]any{
						"kind": authKind, key: value,
					}
				})
			}
			quoted := declare("signed_in", "a=b", "c")
			quotedWidened := declare("any_credential", "a=b", "c")
			plain := declare("signed_in", "a", "b=c")
			plainWidened := declare("any_credential", "a", "b=c")
			allowed := []wire.AuthorizationAllowance{{
				From: `kind=signed_in "a=b"=c`, To: `kind=any_credential "a=b"=c`,
				ReviewedOn: "2026-10-10", Reason: "Review of the declaration with member a=b",
			}}
			if got := wire.CompareWithAllowances(quoted, quotedWidened, allowed); len(got) != 0 {
				t.Errorf("the reviewed quoted-member transition refused: %#v", got)
			}
			if got := wire.CompareWithAllowances(plain, plainWidened, allowed); len(got) != 1 || got[0].Rule != "B6" || got[0].Member != "x-platformkit-auth" {
				t.Errorf("an allowance for member a=b covered member a: %#v", got)
			}
			if got := wire.CompareWithAllowances(quotedWidened, quoted, allowed); len(got) != 1 || got[0].Rule != "B6" || got[0].Member != "x-platformkit-auth" {
				t.Errorf("the reverse of the reviewed pair read as the reviewed one: %#v", got)
			}
		})
	}
}
