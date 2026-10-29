package main

import (
	"bytes"
	"strings"
	"testing"
)

// The fixture tree under testdata holds one package per rule, so each case
// reads the rule it is named for and nothing else. It is read from the package
// directory, which is where `go test` runs.

func report(t *testing.T) []finding {
	t.Helper()
	tree, err := load("testdata", "kit")
	if err != nil {
		t.Fatalf("load testdata/kit: %v", err)
	}
	return classify(tree)
}

func answerFor(t *testing.T, findings []finding, name string) (string, bool) {
	t.Helper()
	for _, f := range findings {
		if f.Name == name {
			return f.Why, true
		}
	}
	return "", false
}

func mustBeReported(t *testing.T, findings []finding, name, want string) {
	t.Helper()
	why, ok := answerFor(t, findings, name)
	if !ok {
		t.Errorf("%s is not reported; the report reads %v", name, findings)
		return
	}
	if !strings.Contains(why, want) {
		t.Errorf("%s is reported as %q, which does not say %q", name, why, want)
	}
}

func mustStandAside(t *testing.T, findings []finding, names ...string) {
	t.Helper()
	for _, name := range names {
		if why, ok := answerFor(t, findings, name); ok {
			t.Errorf("%s is reported as %q, and nothing writes it or reaches through it", name, why)
		}
	}
}

func TestAVariableWrittenAfterItsDeclarationIsReported(t *testing.T) {
	findings := report(t)
	mustBeReported(t, findings, "Counter", "written outside its declaration")
	for _, f := range findings {
		if f.Name == "Counter" && !strings.Contains(f.Where, "counted.go") {
			t.Errorf("Counter is named at %s, not where it is declared", f.Where)
		}
	}
}

func TestAWriteFromAnotherPackageOfTheTreeIsReportedWhereItLands(t *testing.T) {
	why, ok := answerFor(t, report(t), "Items")
	if !ok {
		t.Fatalf("Items is not reported, so a write from writer/ was not followed")
	}
	if !strings.Contains(why, "writer/writer.go") {
		t.Errorf("Items is reported as %q, which does not name the package that writes it", why)
	}
}

func TestAVariableReachableThroughItsValueIsReported(t *testing.T) {
	findings := report(t)
	mustBeReported(t, findings, "Names", "reachable through its slice")
	mustBeReported(t, findings, "Index", "reachable through its map")
	mustBeReported(t, findings, "Store", "reachable through its pointer")
	mustBeReported(t, findings, "Notices", "reachable through its channel")
	mustBeReported(t, findings, "Rows", "reachable through its slice")
}

func TestAVariableNothingWritesAndNothingReachesIsNotReported(t *testing.T) {
	mustStandAside(t, report(t), "Limit", "Default", "Count", "clock")
}

func TestAnErrorSentinelOrAMarkedTableIsNotReported(t *testing.T) {
	findings := report(t)
	for _, f := range findings {
		if strings.Contains(f.Where, "sentinels.go") {
			t.Errorf("%s is reported as %q; a sentinel is compared and a marked table is read", f.Name, f.Why)
		}
	}
	mustStandAside(t, findings, "ErrMissing", "ErrLate", "ErrRefused", "Regions", "Flag")
}

func TestAWriteALocalOfTheSameNameHidesIsNotReported(t *testing.T) {
	mustStandAside(t, report(t), "Hidden")
}

// TestTheKernelIsReportedOnItsOwnTerms runs the command over the tree it exists
// for and logs the number it keeps as a trend. The count is not judged: what is
// checked is that the read reached the kernel and that every finding it printed
// points inside it, which is what a run over a mistyped directory would fail.
func TestTheKernelIsReportedOnItsOwnTerms(t *testing.T) {
	tree, err := load("../..", "kit")
	if err != nil {
		t.Fatalf("the report cannot read kit/: %v", err)
	}
	findings := classify(tree)
	if len(tree.decls) == 0 {
		t.Fatal("the walk of kit/ found no package-level variable at all")
	}
	for _, f := range findings {
		if !strings.HasPrefix(f.Where, "kit") {
			t.Errorf("%s is named at %s, outside the tree the run read", f.Name, f.Where)
		}
	}
	t.Logf("kit/: %d of %d package-level variables are still writable", len(findings), len(tree.decls))
}

func TestTheReportCountsTheTreeItRead(t *testing.T) {
	var out, errs bytes.Buffer
	code := run([]string{"-root", "testdata", "-under", "kit"}, &out, &errs)
	if code != 0 {
		t.Fatalf("a report over the fixture exits %d and says %q", code, errs.String())
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 8 {
		t.Fatalf("the report is %d lines over a fixture of: %s", len(lines), strings.Join(lines, " | "))
	}
	want := "pkgstate: 7 of 18 package-level variables under testdata/kit are still writable"
	if lines[len(lines)-1] != want {
		t.Errorf("the report closes %q, not %q", lines[len(lines)-1], want)
	}
}
