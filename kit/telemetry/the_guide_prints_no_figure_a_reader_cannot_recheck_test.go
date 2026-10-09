package telemetry_test

// Two checks over this package's guide.
//
// The guide once quoted a base commit (`0c3a040`) which a merge had moved, so the
// sentence it quoted for had gone stale while the reading behind it was still true.
// The cure removed the SHA and printed the command instead — and, in the same block,
// printed one *figure*: the `# 8 here` comment
// beside `grep -c 'go.opentelemetry.io/otel' go.mod`. A quoted SHA goes stale on a
// merge; a quoted count goes stale on a `go mod tidy` or a new exporter. The failure
// mode is the same one, one character removed from its cause, so the
// first case below checks the printed figure against the tree that prints it. It
// pins a figure this branch is the sole writer of — go.mod's OpenTelemetry lines are
// what this branch's `build(deps)` commit added — so a case that fails when the quote
// and the file disagree is the brief's duty ("the branch that changes the figure
// updates the quote in the same change") and not a tax on anybody else's merge.
//
// The second case keeps the first cure from rotting back: the guide must name a
// command for its base, not a commit, which it checks by refusing a hex literal
// anywhere in the file. A guide that quotes a SHA again fails it.
//
// The third pins the agreement between the two guards the guide claims speak for the
// same sentence — `scripts/check_packages.sh`'s last line and
// `TestOnlyTheCompositionLinksTheMeasurementSDK`, above — the claim the script once
// failed, and which holds only while both name the same import paths. This is the
// case that would have caught a cure that widened one guard's list and not the
// other's; it costs nothing to run and cannot be satisfied by moving a figure.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// readGuide loads the package guide, or skips the case if it has moved: the cases
// below are about this file's claims, and a renamed guide is a different fact.
func readGuide(t *testing.T) string {
	t.Helper()
	guide, err := os.ReadFile(filepath.Join("..", "..", "kit", "telemetry", "README.md"))
	if err != nil {
		t.Fatalf("read the package guide: %v", err)
	}
	return string(guide)
}

// TestTheGuidesPrintedFigureIsWhatTheCommandPrints: the *Limits* block prints
//
//	grep -c 'go.opentelemetry.io/otel' go.mod    # 8 here
//
// and the number is a claim about this tree. The case runs the same count the
// comment claims and refuses the day they part. It says nothing about the other side
// of the pair — the base is whatever `git merge-base` prints, and pinning its value
// would be pinning a moving tree, which is the regression this file refuses.
func TestTheGuidesPrintedFigureIsWhatTheTreeHas(t *testing.T) {
	guide := readGuide(t)
	const probe = "grep -c 'go.opentelemetry.io/otel' go.mod"
	var line string
	for _, l := range strings.Split(guide, "\n") {
		if strings.Contains(l, probe) && strings.Contains(l, "#") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("the guide no longer prints %q with a figure beside it; this case and the guide must agree about one thing", probe)
	}
	printed := regexp.MustCompile(`#\s*(\d+)`).FindStringSubmatch(line)
	if printed == nil {
		t.Fatalf("the figure beside %q is not a number this case can compare: %s", probe, line)
	}
	want := printed[1]
	gomod, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	got := 0
	for _, l := range strings.Split(string(gomod), "\n") {
		if strings.Contains(l, "go.opentelemetry.io/otel") {
			got++
		}
	}
	if want != strconv.Itoa(got) {
		t.Errorf("the guide prints %q beside %s, and this tree prints %s: the branch that moves the figure updates the quote in the same change",
			"# "+want+" here", probe, strconv.Itoa(got))
	}
}

// TestTheGuideNamesACommandRatherThanACommit refuses a quoted base SHA a merge can
// move. The guide quotes `git merge-base HEAD origin/main` instead of a pin, and this
// case refuses the regression in the shape it had — a
// commit-shaped hex literal anywhere in the guide — and asks that the command still be
// there, so the paragraph cannot keep the promise by dropping the comparison too.
func TestTheGuideNamesACommandRatherThanACommit(t *testing.T) {
	guide := readGuide(t)
	if quoted := regexp.MustCompile(`\b[0-9a-f]{7,40}\b`).FindAllString(guide, -1); len(quoted) > 0 {
		sort.Strings(quoted)
		t.Errorf("the guide quotes what reads like a commit — %v — and a quoted base is stale the day somebody merges; it prints a command for that reading", quoted)
	}
	if !strings.Contains(guide, "git merge-base") {
		t.Errorf("the guide's measurably-better reading promises a command that names its own base; no `git merge-base` appears in it")
	}
}

// TestTheTwoMeasurementGuardsNameTheSameImports: the guide says the script's last line
// and the suite's own case speak for one sentence. `sdkImportPrefixes` above is what
// the Go case refuses; the script refuses what its `-e` patterns name. The metric SDK
// (`go.opentelemetry.io/otel/sdk/metric`) is caught by the sdk prefix rather than named,
// which a reader cannot see from either guard alone — this case says so in the set
// comparison, and goes red the day the two lists part.
func TestTheTwoMeasurementGuardsNameTheSameImports(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "check_packages.sh"))
	if err != nil {
		t.Fatalf("read the package-boundary script: %v", err)
	}
	quoted := regexp.MustCompile(`-e '(go\.opentelemetry\.io/[^']+)'`).FindAllStringSubmatch(string(script), -1)
	if len(quoted) == 0 {
		t.Fatal("the script no longer names its measurement import patterns with -e; this case cannot compare them to the suite's")
	}
	got := map[string]bool{}
	for _, m := range quoted {
		got[m[1]] = true
	}
	want := map[string]bool{}
	for _, p := range sdkImportPrefixes {
		want[p] = true
	}
	var extra, missing []string
	for p := range got {
		if !want[p] {
			extra = append(extra, p)
		}
	}
	for _, p := range sdkImportPrefixes {
		if !got[p] {
			missing = append(missing, p)
		}
	}
	if len(extra) > 0 || len(missing) > 0 {
		sort.Strings(extra)
		t.Errorf("the two guards of the measurement rule no longer name the same imports: the script refuses %v and the suite refuses %v"+
			" — the guide says one sentence covers both, and the day they part one of them stops speaking for it",
			extra, missing)
	}
}
