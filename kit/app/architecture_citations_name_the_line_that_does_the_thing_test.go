package app

// ARCHITECTURE.md cites source by line number, so a comment shortened inside a cited file
// moves every line below it, and a citation can stay inside its file while landing on a
// neighbour's comment. This case holds the three citations this package's own files receive
// from the pillars: the pair that names the calls installing the trace and metric providers,
// the one that names the AsyncAPI emitter, and the one that names the cover it counts. Each
// cited line must sit inside the declaration the prose names, doc comment counted as part of
// it, so the reader is sent to the code that does the thing.
//
// It reads ARCHITECTURE.md and these two files from the repository root and asks nothing of
// a database; when the prose stops naming a claim the way it does now, the case fails by
// name rather than passing on nothing.

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// archCitation reads the line citations of one file out of a window of the document, in the
// order the document writes them, accepting the full `path.go:N-M` form and the shorthand
// `:N-M` that continues the path named before it.
func archCitation(t *testing.T, window string) []int {
	t.Helper()
	re := regexp.MustCompile(`kit/app/[\w./-]+\.go:(?P<full>\d+)|` + "`" + `:(?P<short>\d+)(?:-\d+)?` + "`")
	var out []int
	for _, m := range re.FindAllStringSubmatch(window, -1) {
		n, err := strconv.Atoi(m[1] + m[2])
		if err != nil {
			t.Fatalf("%q: unreadable line number: %v", m[0], err)
		}
		out = append(out, n)
	}
	return out
}

// readRoot returns the lines of path as the reader at the repository root counts them.
func readRoot(t *testing.T, path string) []string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", path))
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
}

// declarationSpan returns the first and last line of the function whose signature starts
// with sig, its doc comment counted as part of it, or found false when the file holds no
// such signature.
func declarationSpan(t *testing.T, lines []string, sig string) (from, to int, found bool) {
	t.Helper()
	decl := 0
	for i, l := range lines {
		if strings.HasPrefix(l, sig) {
			decl = i + 1
			break
		}
	}
	if decl == 0 {
		return 0, 0, false
	}
	from = decl
	for from > 1 && strings.HasPrefix(strings.TrimSpace(lines[from-2]), "//") {
		from--
	}
	for i := decl; i < len(lines); i++ {
		if lines[i] == "}" {
			return from, i + 1, true
		}
	}
	return from, len(lines), true
}

// TestTheProviderCitationNamesTheCallsThatInstallTheProviders holds the pillar sentence that
// hands the install of the two global providers to two named lines of telemetry.go.
func TestTheProviderCitationNamesTheCallsThatInstallTheProviders(t *testing.T) {
	doc := strings.Join(readRoot(t, "ARCHITECTURE.md"), "\n")
	at := strings.Index(doc, "(`kit/app/telemetry.go:")
	if at < 0 {
		t.Fatal("the telemetry pillar no longer hands the provider install to named lines of kit/app/telemetry.go: the prose changed and this case reads nothing")
	}
	nums := archCitation(t, doc[at:at+80])
	if len(nums) != 2 {
		t.Fatalf("the sentence names %d lines of kit/app/telemetry.go, want the tracer and the meter: %q", len(nums), doc[at:at+80])
	}
	want := []string{"otel.SetTracerProvider(", "otel.SetMeterProvider("}
	lines := readRoot(t, "kit/app/telemetry.go")
	for i, sig := range want {
		line := nums[i]
		if line < 1 || line > len(lines) {
			t.Fatalf("kit/app/telemetry.go:%d is outside the file, which has %d lines", line, len(lines))
		}
		if !strings.Contains(lines[line-1], sig) {
			t.Errorf("the provider citation names kit/app/telemetry.go:%d for %s, but that line is %q", line, sig, strings.TrimSpace(lines[line-1]))
		}
	}
}

// TestTheAsyncAPICitationNamesTheEmitter holds the register's citation for `AsyncAPI`: the
// line it names is the emitter's own signature, not a constant above it.
func TestTheAsyncAPICitationNamesTheEmitter(t *testing.T) {
	doc := strings.Join(readRoot(t, "ARCHITECTURE.md"), "\n")
	at := strings.Index(doc, "`AsyncAPI` `:")
	if at < 0 {
		t.Fatal("the register no longer names the AsyncAPI emitter by line: the prose changed and this case reads nothing")
	}
	nums := archCitation(t, doc[at:at+20])
	if len(nums) != 1 {
		t.Fatalf("the register names %d lines beside the name AsyncAPI, want the one it cites: %q", len(nums), doc[at:at+20])
	}
	from, to, found := declarationSpan(t, readRoot(t, "kit/app/asyncapi.go"), "func AsyncAPI(")
	if !found {
		t.Fatal("kit/app/asyncapi.go no longer declares func AsyncAPI(")
	}
	if nums[0] < from || nums[0] > to {
		t.Errorf("`AsyncAPI` `:%d` is outside func AsyncAPI (lines %d-%d)", nums[0], from, to)
	}
}

// TestTheCoverCitationNamesTheCounter holds the sentence that says CoveredEvents counts the
// declared events the document names: the line it cites must be inside that function or its
// doc comment, which is where the promise is written down.
func TestTheCoverCitationNamesTheCounter(t *testing.T) {
	doc := strings.Join(readRoot(t, "ARCHITECTURE.md"), "\n")
	phrase := "counts the declared events the document names"
	at := strings.Index(doc, phrase)
	if at < 0 {
		t.Fatal("no sentence about CoveredEvents counting declared events: the prose changed and this case reads nothing")
	}
	window := doc[max(0, at-len(phrase)-160):at]
	nums := archCitation(t, window)
	if len(nums) == 0 {
		t.Fatalf("the sentence %q cites no line of kit/app before it: %q", phrase, window)
	}
	from, to, found := declarationSpan(t, readRoot(t, "kit/app/asyncapi.go"), "func CoveredEvents(")
	if !found {
		t.Fatal("kit/app/asyncapi.go no longer declares func CoveredEvents(")
	}
	if nums[len(nums)-1] < from || nums[len(nums)-1] > to {
		t.Errorf("the cover sentence cites kit/app/asyncapi.go:%d, which is outside func CoveredEvents (lines %d-%d)",
			nums[len(nums)-1], from, to)
	}
}
