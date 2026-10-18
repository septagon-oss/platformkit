package wire_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/wire"
)

func TestAuthorizationMemberNamesCannotBorrowReviewedTransitions(t *testing.T) {
	for _, kind := range []string{"openapi", "asyncapi"} {
		t.Run(kind, func(t *testing.T) {
			declare := func(authKind, key, value string) []byte {
				return mutated(t, baseline(kind), func(doc map[string]any) {
					operation(doc, kind)["x-platformkit-auth"] = map[string]any{
						"kind": authKind, key: value,
					}
				})
			}
			plain := declare("signed_in", "a", "b=c")
			plainWidened := declare("any_credential", "a", "b=c")
			quoted := declare("signed_in", "a=b", "c")
			quotedWidened := declare("any_credential", "a=b", "c")
			allowed := []wire.AuthorizationAllowance{{
				From: "kind=signed_in a=b=c", To: "kind=any_credential a=b=c",
				ReviewedOn: "2026-10-09", Reason: "Review of the declaration with member a",
			}}
			if got := wire.CompareWithAllowances(plain, plainWidened, allowed); len(got) != 0 {
				t.Fatalf("the explicitly reviewed transition refused: %#v", got)
			}
			if got := wire.Compare(quoted, quoted); len(got) != 0 {
				t.Fatalf("an unchanged quoted member refused: %#v", got)
			}
			for _, pair := range [][2][]byte{
				{quoted, quotedWidened}, {plain, quotedWidened}, {quoted, plainWidened},
			} {
				got := wire.CompareWithAllowances(pair[0], pair[1], allowed)
				if len(got) != 1 || got[0].Rule != "B6" || got[0].Member != "x-platformkit-auth" {
					t.Errorf("an allowance for member a covered member a=b: %#v", got)
				}
			}
			for _, pair := range [][2][]byte{{plain, quoted}, {quoted, plain}} {
				got := wire.Compare(pair[0], pair[1])
				if len(got) != 1 || got[0].Rule != "B6" || !strings.Contains(got[0].Message, `"a=b"=c`) {
					t.Errorf("a member changed without a distinct identity: %#v", got)
				}
				output, onDisk, err := runGolden(t, pair[0], pair[1], "1", "")
				if err == nil || !strings.Contains(output, "B6 (breaking)") || !bytes.Equal(onDisk, pair[0]) {
					t.Errorf("golden mutation: err=%v unchanged=%v\n%s", err, bytes.Equal(onDisk, pair[0]), output)
				}
			}
		})
	}
}
