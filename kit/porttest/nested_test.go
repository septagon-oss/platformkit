package porttest

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

// The bytes NestedRuns answers with, and the one thing it may never do: take
// the framing off a line the framework wrote about a test the package contains.

func TestUnnestTakesFramingOffTheNestedRunOnly(t *testing.T) {
	in := "\x16=== RUN   TestReal\n" +
		"\x16=== RUN   shelf_RunService\n" +
		"\x16=== RUN   shelf_RunService/places_the_box\n" +
		"\x16    shelf_test.go:12: the box is gone\n" +
		"\x16--- PASS: shelf_RunService/places_the_box (0.00s)\n" +
		"\x16--- FAIL: shelf_RunService (0.00s)\n" +
		"\x16PASS\n" +
		"\x16=== CONT  TestReal\n" +
		"\x16--- PASS: TestReal (0.00s)\n" +
		"\x16PASS\n"
	want := []string{
		"\x16=== RUN   TestReal\n",
		"=== RUN   shelf_RunService\n",
		"=== RUN   shelf_RunService/places_the_box\n",
		"\x16    shelf_test.go:12: the box is gone\n",
		"--- PASS: shelf_RunService/places_the_box (0.00s)\n",
		"--- FAIL: shelf_RunService (0.00s)\n",
		"PASS\n",
		"\x16=== CONT  TestReal\n",
		"\x16--- PASS: TestReal (0.00s)\n",
		"\x16PASS\n",
	}
	got := strings.SplitAfter(unnestOn(in, "shelf_RunService"), "\n")
	got = slices.Clip(got[:len(got)-1]) // the split's trailing empty piece
	if !slices.Equal(got, want) {
		t.Errorf("the nested run came out changed:\n got %q\nwant %q", got, want)
	}
}

// A run that ends in PASS owes a bare PASS; one that ends in FAIL owes a bare
// FAIL, and neither is the package's own result.
func TestUnnestClosesTheNestedRunOnItsOwnResult(t *testing.T) {
	for _, end := range []string{"PASS", "FAIL"} {
		out := unnestOn("\x16=== RUN   shelf_RunService\n"+
			"\x16=== PAUSE shelf_RunService\n"+
			"\x16=== CONT  shelf_RunService\n"+
			"\x16--- FAIL: shelf_RunService (0.00s)\n"+
			"\x16"+end+"\n"+
			"\x16--- PASS: TestReal (0.00s)\n\x16PASS\n", "shelf_RunService")
		want := "=== RUN   shelf_RunService\n=== PAUSE shelf_RunService\n=== CONT  shelf_RunService\n" +
			"--- FAIL: shelf_RunService (0.00s)\n" + end + "\n" +
			"\x16--- PASS: TestReal (0.00s)\n\x16PASS\n"
		if out != want {
			t.Errorf("a nested run ending in %s: got %q, want %q", end, out, want)
		}
	}
}

// The package's own final PASS is its result: a nested run that ended one line
// earlier must not have the last word taken off it.
func TestUnnestKeepsTheBinarysOwnResult(t *testing.T) {
	in := "\x16=== RUN   shelf_RunService\n" +
		"\x16--- PASS: shelf_RunService (0.00s)\n" +
		"\x16--- PASS: TestReal (0.00s)\n\x16PASS\n"
	out := unnestOn(in, "shelf_RunService")
	if !strings.HasSuffix(out, "\x16--- PASS: TestReal (0.00s)\n\x16PASS\n") {
		t.Errorf("the binary's own report lost its framing: %q", out)
	}
}

// An unframed line is already plain output, and a nested runner that was not
// asked for keeps its framing: the filter cannot silence a test nobody declared.
func TestUnnestLeavesEverythingElseAlone(t *testing.T) {
	for _, in := range []string{
		"=== RUN   TestReal\n",
		"\x16=== RUN   other_RunService\n\x16--- FAIL: other_RunService (0.00s)\n",
		"\x16some text with --- FAIL: inside it (0.00s)\n",
	} {
		if got := unnestOn(in, "shelf_RunService"); got != in {
			t.Errorf("unnestOn(%q) = %q; the bytes a line arrived with are the bytes it leaves with", in, got)
		}
	}
}

// The safety is the function's own and not the habit of its callers: a name a
// real test could report under is refused before the binary runs, so a proof
// that asked to strip "TestSomething" stops with a sentence instead of quietly
// demoting that test's failures to output. A nil *testing.M is safe precisely
// because both refusals answer before m.Run — which is also what makes them
// testable at all.
func TestNestedRunsRefusesANameARealTestCouldReportUnder(t *testing.T) {
	for _, names := range [][]string{
		{"TestFakeConforms"},
		{"shelf_RunService", "TestUnnestTakesFramingOffTheNestedRunOnly"},
		nil,
	} {
		if got := NestedRuns(nil, names...); got != 1 {
			t.Errorf("NestedRuns(nil, %q) = %d, want 1: the run is refused and nothing runs", names, got)
		}
	}
}

func unnestOn(in, name string) string {
	var out bytes.Buffer
	unnest(strings.NewReader(in), &out, []string{name})
	return out.String()
}
