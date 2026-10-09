package wire_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/wire"
)

// TestAReviewedWideningCoversNoFeatureTheDoorGained holds the one direction the other
// allowance cases do not: the golden is the reviewed pair's own From, feature-free, and
// the delivery widens the kind exactly as reviewed while the door also gains a plan
// feature. The pair is exact over the whole declaration, so it names neither side of
// this transition and the gate refuses it as B6 with both identities spelled — the
// widening somebody reviewed cannot carry a plan gate nobody did.
func TestAReviewedWideningCoversNoFeatureTheDoorGained(t *testing.T) {
	for _, kind := range []string{"openapi", "asyncapi"} {
		t.Run(kind, func(t *testing.T) {
			path, id := "GET /items", "list"
			if kind != "openapi" {
				path, id = "SEND item.changed", "publish"
			}
			old := baseline(kind)
			gained := mutated(t, old, func(doc map[string]any) {
				operation(doc, kind)["x-platformkit-auth"] = map[string]any{"kind": "any_credential", "feature": "pro"}
			})
			want := wire.Break{
				Rule: "B6", Path: path, Member: "x-platformkit-auth",
				Message: fmt.Sprintf("B6 (breaking): %s (%s) is authorized kind=any_credential feature=pro where it was kind=signed_in", path, id),
			}
			if got := wire.CompareWithAllowances(old, gained, allowance()); !slices.Contains(got, want) {
				t.Fatalf("got %#v; want %#v", got, want)
			}
			widened := mutated(t, old, func(doc map[string]any) {
				operation(doc, kind)["x-platformkit-auth"] = map[string]any{"kind": "any_credential"}
			})
			if got := wire.CompareWithAllowances(old, widened, allowance()); len(got) != 0 {
				t.Fatalf("the reviewed widening alone stopped matching its pair: %#v", got)
			}
		})
	}
}
