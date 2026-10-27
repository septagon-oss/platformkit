package wire_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/wire"
)

// refusalsAsMessages returns the diagnostics of one rule and fails on any other rule, so a
// case that pins an incomplete verdict cannot pass by reporting a break instead.
func refusalsAsMessages(t *testing.T, breaks []wire.Break, rule string) []string {
	t.Helper()
	var out []string
	for _, b := range breaks {
		if b.Rule != rule {
			t.Errorf("unexpected %s: %s", b.Rule, b.Message)
		}
		out = append(out, b.Message)
	}
	return out
}

// A comparison that stopped early is not a compatible pair, and UPDATE_GOLDEN=1 must not
// reach the baseline because of it: the incomplete verdict arrives on the same channel as a
// break, before the write, which is the only reason a stale document cannot be regenerated
// over a contract that was never read.
func TestIncompleteAnalysisRefusesTheGoldenBeforeWriting(t *testing.T) {
	t.Parallel()
	deep := func(kind string) []byte {
		return withRequestSchema(t, nestedSchemas(depthLimit+1, leafOfType(kind)))
	}
	wide := sharedSchemaDocument()
	for _, tc := range []struct {
		name    string
		golden  []byte
		current []byte
		want    string
	}{
		{"a deep leaf changed", deep("string"), deep("integer"), "past the traversal limit of 24 schema levels"},
		{"a deep document did not change at all", deep("string"), deep("string"), "past the traversal limit of 24 schema levels"},
		{"the node budget ran out", wide, wide, "after 200000 schema nodes"},
		{"a schema declares a keyword nobody reads", baseline("openapi"),
			withForm(t, func(s map[string]any) { s["allOf"] = []any{map[string]any{"type": "string"}} }),
			"declares allOf"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, onDisk, err := runGolden(t, tc.golden, tc.current, "1", "")
			if err == nil || !strings.Contains(output, tc.want) || !strings.Contains(output, goldenKept) || !bytes.Equal(onDisk, tc.golden) {
				t.Fatalf("err=%v; unchanged=%v; want %q in\n%s", err, bytes.Equal(onDisk, tc.golden), tc.want, output)
			}
		})
	}
}

// goldenKept is what the golden gate says beside a refusal it honoured.
const goldenKept = "is left as it was: the rules above stand, and UPDATE_GOLDEN=1 regenerates a document that is stale, never one that is broken"

// The same three verdicts arrive in the ordinary call, in the sorted order, with the address
// and the side that stopped the walk: a caller reads one list and cannot mistake the third
// verdict for the first.
func TestIncompleteAnalysisArrivesInTheSameList(t *testing.T) {
	t.Parallel()
	deep := withRequestSchema(t, nestedSchemas(depthLimit+1, leafOfType("string")))
	breaks := wire.Compare(deep, deep)
	if len(breaks) != 2 || !slices.IsSortedFunc(breaks, func(a, b wire.Break) int { return strings.Compare(a.Message, b.Message) }) {
		t.Fatalf("got %v; want two sorted refusals", breaks)
	}
	for _, b := range breaks {
		if b.Rule != "B7" || b.Member != nestedAddress(depthLimit+1) || !strings.HasPrefix(b.Message, "B7 (incomplete): ") {
			t.Fatalf("got %v; want a B7 that states what it is", breaks)
		}
	}
}
