package httpx_test

// ARCHITECTURE.md cites this package's files by line number. A shrink of a cited
// file moves every line below what it shortened, so the citations into kit/httpx
// are held here: each one must resolve inside the file it names, and the ones the
// prose attaches to a named span or to a named comment must land inside that span
// or carry that comment, so a reader is sent to the code that holds the claim.

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// citation is a `path.go:N` or `path.go:N-M` reference read out of the document,
// with the line numbers already parsed.
type citation struct {
	path string
	from int
	to   int
	text string
}

// citationsInto returns every line citation whose path starts with prefix, in the
// order the document writes them.
func citationsInto(t *testing.T, doc, prefix string) []citation {
	t.Helper()
	re := regexp.MustCompile(`(?P<path>kit/httpx/[\w./-]+\.go):(?P<from>\d+)(?:-(?P<to>\d+))?`)
	var out []citation
	for _, m := range re.FindAllStringSubmatchIndex(doc, -1) {
		matched, path := doc[m[0]:m[1]], doc[m[2]:m[3]]
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		from, err := strconv.Atoi(doc[m[4]:m[5]])
		if err != nil {
			t.Fatalf("%s: unreadable line number: %v", matched, err)
		}
		to := from
		if m[6] >= 0 {
			to, err = strconv.Atoi(doc[m[6]:m[7]])
			if err != nil {
				t.Fatalf("%s: unreadable line number: %v", matched, err)
			}
		}
		out = append(out, citation{path: path, from: from, to: to, text: matched})
	}
	return out
}

// fileLines returns the lines of path as the reader at the repository root sees them.
func fileLines(t *testing.T, root, path string) []string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	lines := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	return lines
}

// TestEveryCitationIntoThisPackageResolvesInsideTheFile reads every line citation
// into kit/httpx and asks the cited file how long it is. A citation the file cannot
// answer sends a reader to a line that does not exist.
func TestEveryCitationIntoThisPackageResolvesInsideTheFile(t *testing.T) {
	root := filepath.Join("..", "..")
	doc := string(readFile(t, filepath.Join(root, "ARCHITECTURE.md")))
	cites := citationsInto(t, doc, "kit/httpx/")
	if len(cites) == 0 {
		t.Fatal("ARCHITECTURE.md cites no line of kit/httpx: its citation convention changed and this case reads nothing")
	}
	for _, c := range cites {
		lines := fileLines(t, root, c.path)
		if c.from < 1 || c.to < c.from || c.to > len(lines) {
			t.Errorf("%s is outside %s, which has %d lines", c.text, c.path, len(lines))
		}
	}
}

// TestTheRefusalLanguageRuleIsCitedWhereItIsWritten holds the one citation the
// locale pillar hands to a comment: the prose says the reason a refusal may not
// answer in an undeclared language is written at a named range, so those lines
// must state that reason — the header a refusal would otherwise negotiate from,
// and the declaration a tenant owns.
func TestTheRefusalLanguageRuleIsCitedWhereItIsWritten(t *testing.T) {
	root := filepath.Join("..", "..")
	doc := string(readFile(t, filepath.Join(root, "ARCHITECTURE.md")))
	at := strings.Index(doc, "undeclared language is the comment at")
	if at < 0 {
		t.Fatal("the locale pillar no longer hands its reason to a code comment: the prose changed and this case reads nothing")
	}
	cites := citationsInto(t, doc[at:at+200], "kit/httpx/")
	if len(cites) != 1 {
		t.Fatalf("the sentence names %d citations into kit/httpx, want the one comment it points at", len(cites))
	}
	c := cites[0]
	lines := fileLines(t, root, c.path)
	if c.to > len(lines) {
		t.Fatalf("%s is outside %s, which has %d lines", c.text, c.path, len(lines))
	}
	cited := strings.Join(lines[c.from-1:c.to], "\n")
	for _, phrase := range []string{"Accept-Language", "declared"} {
		if !strings.Contains(cited, phrase) {
			t.Errorf("%s cites the reason a refusal is answered in a declared language, but those lines of %s never say %q; the comment they name is elsewhere:\n%s",
				c.text, c.path, phrase, cited)
		}
	}
}

// TestTheTraceCitationsLandOnTheSpansTheyName holds the two citations the tracing
// paragraph makes to kit/httpx/traced.go: the transport span is opened by tracing,
// and the span named after the operation id is renamed by traced. Each cited line
// must sit in the function the prose names, doc comment included.
func TestTheTraceCitationsLandOnTheSpansTheyName(t *testing.T) {
	root := filepath.Join("..", "..")
	doc := string(readFile(t, filepath.Join(root, "ARCHITECTURE.md")))
	at := strings.Index(doc, "the transport span")
	if at < 0 {
		t.Fatal("no tracing paragraph cites the transport span: the prose changed and this case reads nothing")
	}
	want := []struct {
		text   string
		signat string
	}{
		{text: "the transport span", signat: "func tracing("},
		{text: "named after the operation id", signat: "func (a *API) traced("},
	}
	window := doc[at:min(at+220, len(doc))]
	nums := regexp.MustCompile(`kit/httpx/traced\.go:(\d+)|`+"`"+`:(\d+)`+"`").FindAllStringSubmatch(window, -1)
	if len(nums) < 2 {
		t.Fatalf("the tracing paragraph names %d lines of traced.go, want the span and its rename: %q", len(nums), window)
	}
	for i, w := range want {
		line, err := strconv.Atoi(nums[i][1] + nums[i][2])
		if err != nil {
			t.Fatalf("%s: unreadable line number: %v", w.text, err)
		}
		from, to, found := declaration(fileLines(t, root, "kit/httpx/traced.go"), w.signat)
		if !found {
			t.Fatalf("kit/httpx/traced.go no longer declares %q", w.signat)
		}
		if line < from || line > to {
			t.Errorf("%s cites kit/httpx/traced.go:%d, which is outside %s (lines %d-%d)", w.text, line, w.signat, from, to)
		}
	}
}

// declaration returns the first and last line of the function whose signature
// starts with sig, its doc comment counted as part of it, or found false when the
// file holds no such signature.
func declaration(lines []string, sig string) (from, to int, found bool) {
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

// readFile reads a file the way the cases read the repository, failing the case
// rather than skipping it when the file is not there.
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return body
}
