// ARCHITECTURE.md sends a reader to `Observe` by line number, and the range it prints stays a
// legal number inside this file however far it drifts: the pillars' check asks only that a
// citation resolve inside the file it names. What the sentence promises is more than that — it
// names the door a live connection is built on — so the cited range must contain the line that
// declares Observe and must begin no earlier than that line's own doc comment. A number that
// falls on a neighbour's comment or on the blank line between two members is refused here by
// name, which is the only way a shortened comment above it is caught before a reader arrives.
package transport_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestTheObserveCitationLandsOnTheDeclaration holds the one sentence in the pillars that cites
// this file by number: its range must cover the declaration of Observe and must start inside
// that declaration's doc comment. When the prose stops naming Observe at all the case fails by
// name too, so it can never pass by reading nothing.
func TestTheObserveCitationLandsOnTheDeclaration(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	doc := readDocument(t, filepath.Join(root, "ARCHITECTURE.md"))
	at := strings.Index(doc, "`Observe`\n(")
	if at < 0 {
		t.Fatal("the register no longer cites Observe by number: the prose changed and this case reads nothing")
	}
	window := doc[at:min(at+120, len(doc))]
	pattern := "`" + `kit/events/transport/transport\.go:(\d+)(?:-(\d+))?` + "`"
	cite := regexp.MustCompile(pattern).FindStringSubmatch(window)
	if cite == nil {
		t.Fatalf("the sentence that names Observe cites no line of kit/events/transport/transport.go: %q", window)
	}
	from, err := strconv.Atoi(cite[1])
	if err != nil {
		t.Fatalf("%s: unreadable first line: %v", cite[0], err)
	}
	to := from
	if cite[2] != "" {
		if to, err = strconv.Atoi(cite[2]); err != nil {
			t.Fatalf("%s: unreadable last line: %v", cite[0], err)
		}
	}
	if to < from {
		t.Fatalf("%s: the range is reversed", cite[0])
	}

	lines := readLines(t, filepath.Join(root, "kit/events/transport/transport.go"))
	decl := 0
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "Observe(") {
			decl = i + 1
			break
		}
	}
	if decl == 0 {
		t.Fatal("kit/events/transport/transport.go no longer declares Observe(")
	}
	comment := decl
	for comment > 1 && strings.HasPrefix(strings.TrimSpace(lines[comment-2]), "//") {
		comment--
	}
	if from > len(lines) || to > len(lines) {
		t.Fatalf("%s names lines beyond this file, which has %d of them", cite[0], len(lines))
	}

	if decl < from || decl > to {
		t.Errorf("the register cites %s, which does not cover the declaration of Observe at kit/events/transport/transport.go:%d; a reader lands on %q",
			cite[0], decl, strings.TrimSpace(lines[from-1]))
	}
	if from < comment || from > decl {
		t.Errorf("the register cites %s, whose first line %d is outside Observe's own promise (lines %d-%d); a reader lands on %q",
			cite[0], from, comment, decl, strings.TrimSpace(lines[from-1]))
	}
}

// readDocument returns a file's whole body, for prose matched by phrase rather than by line.
func readDocument(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return string(body)
}

// readLines returns a file's lines, counted the way a reader with a citation counts them.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	return strings.Split(strings.TrimSuffix(readDocument(t, path), "\n"), "\n")
}
