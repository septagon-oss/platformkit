package wire_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/wire"
)

// shapeAddress is where one side of the baseline operation is reported: the path a break
// names and the shape key its diagnostic is headed with.
func shapeAddress(kind, direction string) (string, string) {
	if direction == "request" {
		if kind == "openapi" {
			return "POST /items", "create request body application/json .kind"
		}
		return "RECEIVE item.create", "consume request message/input/input payload .kind"
	}
	if kind == "openapi" {
		return "GET /items", "list response 200 application/json .name"
	}
	return "SEND item.changed", "publish response message/item/item payload .name"
}

// declaringMembers is the baseline with one side's enum written out: the request side's
// `kind` or the response side's `name`, and nothing else about the document moved.
func declaringMembers(t *testing.T, kind, direction string, members []any) []byte {
	t.Helper()
	property := "name"
	if direction == "request" {
		property = "kind"
	}
	return mutated(t, baseline(kind), func(doc map[string]any) {
		at(shape(doc, kind, direction), "properties", property)["enum"] = members
	})
}

// memberCase is one pair of accepted member sets on one side of one operation, and the
// break the difference earns. The member a B5 names and the shape text a B3 prints are
// written out here, so the spelling of a set is decided by the case rather than read back
// off whatever the comparator emitted.
type memberCase struct {
	name, direction, rule, want string
	golden, current             []any
}

func memberCases() []memberCase {
	return []memberCase{
		{"one member that holds a pipe is not two members", "request", "B5",
			`B5 (breaking): %s no longer accepts "a|b", which an older client sends`, []any{"a|b"}, []any{"a", "b"}},
		{"two members become one that holds a pipe", "response", "B5",
			`B5 (breaking): %s may now answer "a|b", which an older reader has no branch for`, []any{"a", "b"}, []any{"a|b"}},
		{"a lost member that holds a pipe is named as itself", "request", "B5",
			`B5 (breaking): %s no longer accepts "a|b", which an older client sends`, []any{"a|b"}, []any{"a"}},
		{"a gained member that holds a pipe is printed quoted", "request", "B3",
			`B3 (breaking): %s changed from "type=string enum=a" to "type=string enum=\"b|c\"|a"`, []any{"a"}, []any{"a", "b|c"}},
		{"a member that holds a space is not two members", "request", "B5",
			`B5 (breaking): %s no longer accepts "a b", which an older client sends`, []any{"a b"}, []any{"a", "b"}},
		{"an empty member is a member", "request", "B5",
			`B5 (breaking): %s no longer accepts "", which an older client sends`, []any{"", "a"}, []any{"a"}},
		{"a pipe alone is a member", "request", "B5",
			`B5 (breaking): %s no longer accepts "|", which an older client sends`, []any{"|"}, []any{""}},
		{"a member that holds a quote is escaped and stays one member", "request", "B5",
			`B5 (breaking): %s no longer accepts "a\"b", which an older client sends`, []any{`a"b`}, []any{"a"}},
		{"a member that holds a tab is quoted", "request", "B5",
			`B5 (breaking): %s no longer accepts "a\tb", which an older client sends`, []any{"a\tb"}, []any{"ab"}},
		{"a numeric member is named as the number it is", "request", "B5",
			`B5 (breaking): %s no longer accepts 2, which an older client sends`, []any{1, 2}, []any{1}},
		{"the string one is not the number one", "request", "B5",
			`B5 (breaking): %s no longer accepts "1", which an older client sends`, []any{"1"}, []any{1}},
		{"the string true is not the boolean true", "request", "B5",
			`B5 (breaking): %s no longer accepts true, which an older client sends`, []any{true}, []any{"true"}},
		{"members in another order are the same set", "request", "", "", []any{"b", "a"}, []any{"a", "b"}},
		{"a member written twice is one member", "request", "", "", []any{"a", "a"}, []any{"a"}},
		{"a numeric member written another way is the same member", "request", "", "", []any{1, 2}, []any{json.Number("1.0"), json.Number("2e0")}},
	}
}

// TestEnumMemberSpellingsNameDistinctSets asks the two halves of one promise: a set of
// enum members has one spelling, so two different sets never read as no change, and the
// member a refused change names is the member that moved.
func TestEnumMemberSpellingsNameDistinctSets(t *testing.T) {
	for _, kind := range []string{"openapi", "asyncapi"} {
		for _, tc := range memberCases() {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				path, key := shapeAddress(kind, tc.direction)
				old := declaringMembers(t, kind, tc.direction, tc.golden)
				fresh := declaringMembers(t, kind, tc.direction, tc.current)
				breaks := wire.Compare(old, fresh)
				if tc.rule == "" {
					if len(breaks) != 0 {
						t.Fatalf("%v vs %v broke: %#v", tc.golden, tc.current, breaks)
					}
					return
				}
				want := wire.Break{Rule: tc.rule, Path: path, Member: key, Message: fmt.Sprintf(tc.want, key)}
				if len(breaks) != 1 || breaks[0] != want {
					t.Fatalf("got %#v; want %#v", breaks, want)
				}
			})
		}
	}
}

// TestGoldenRefusesALostPipeMemberBeforeWriting hands the mutation that vanished behind a
// pipe the golden update flag: the baseline a reviewer approved is left exactly as it was.
func TestGoldenRefusesALostPipeMemberBeforeWriting(t *testing.T) {
	for _, kind := range []string{"openapi", "asyncapi"} {
		_, key := shapeAddress(kind, "request")
		old := declaringMembers(t, kind, "request", []any{"a|b"})
		fresh := declaringMembers(t, kind, "request", []any{"a"})
		output, onDisk, err := runGolden(t, old, fresh, "1", "")
		want := fmt.Sprintf("B5 (breaking): %s no longer accepts \"a|b\", which an older client sends", key)
		if err == nil || !strings.Contains(output, want) || !strings.Contains(output, "is left as it was") || !bytes.Equal(onDisk, old) {
			t.Fatalf("%s: err=%v; unchanged=%v\n%s", kind, err, bytes.Equal(onDisk, old), output)
		}
	}
}
