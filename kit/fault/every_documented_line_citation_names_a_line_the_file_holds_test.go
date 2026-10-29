package fault_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// citationDocuments are the documents whose prose cites source lines by number: the
// implementation map and the decision records. They are named here rather than walked
// so a document added elsewhere states that it wants its citations checked.
var citationDocuments = []string{"ARCHITECTURE.md", "docs/adr"}

// lineCitation matches `path/to/file.go:NN` or `path/to/file.go:NN-MM` inside a code
// span: the shape these documents use for "the answer is on these lines".
var lineCitation = regexp.MustCompile(
	"`([A-Za-z0-9_./-]+\\.(?:go|sql|md|json|ts|tsx|sh|py|mjs|js|ya?ml|yml|rego))(?::(\\d+)(?:\\s*[-–]\\s*(\\d+))?)?`")

// TestEveryDocumentedLineCitationNamesALineTheFileHolds reads the architecture map and
// every ADR the way its reader does: a citation of `path/file.go:NN` names lines of a
// file that exists in this repository, and both ends of its range are inside it. A
// citation past the end sends the reader to a line that is not there.
//
// Editing a comment or moving a function shifts every line below it, so this breaks
// without any test failing and no compiler noticing — the only witness is the number
// the document wrote.
//
// The pillar section's shorthand citations, and the declarations of the runner's own
// files, are held elsewhere to sit inside what the prose names; this case asks the
// weaker question of the whole document and of every decision record: that the line a
// citation names exists at all.
//
// A citation naming a path that is not in this repository is not refused here, because
// these documents name illustrative files (a product's module.go, a manifest a reader
// would write) beside this tree's own sources: what can be checked of a real file is
// the line it names, and that is what this asserts.
func TestEveryDocumentedLineCitationNamesALineTheFileHolds(t *testing.T) {
	root := filepath.Join("..", "..")
	paths, err := citedDocuments(root)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, doc := range paths {
		text, err := os.ReadFile(doc)
		if err != nil {
			t.Errorf("%s: %v", doc, err)
			continue
		}
		rel, err := filepath.Rel(root, doc)
		if err != nil {
			t.Errorf("%s: %v", doc, err)
			continue
		}
		for _, m := range lineCitation.FindAllStringSubmatch(string(text), -1) {
			cited, from, to := m[1], m[2], m[3]
			if from == "" {
				continue // the file is named for itself, with no line to check
			}
			source, ok := resolveCited(root, filepath.Dir(rel), cited)
			if !ok {
				continue // an illustrative name, not this repository's file
			}
			lines, err := readLines(source)
			if err != nil {
				t.Errorf("%s cites %s: %v", rel, cited, err)
				continue
			}
			start, err := strconv.Atoi(from)
			if err != nil {
				t.Errorf("%s cites %s: unreadable first line %q", rel, cited, from)
				continue
			}
			end := start
			if to != "" {
				if end, err = strconv.Atoi(to); err != nil {
					t.Errorf("%s cites %s: unreadable last line %q", rel, cited, to)
					continue
				}
			}
			checked++
			if start < 1 || end < start || end > len(lines) {
				t.Errorf("%s cites %s, which has %d lines: %s is outside it",
					rel, cited, len(lines), m[0])
			}
		}
	}
	if checked == 0 {
		t.Fatal("no line citation was checked: the documents' citation shape changed and this test reads nothing")
	}
}

// citedDocuments lists the documents to read: the named files, and every .md inside a
// named directory.
func citedDocuments(root string) ([]string, error) {
	var out []string
	for _, name := range citationDocuments {
		full := filepath.Join(root, name)
		info, err := os.Stat(full)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			out = append(out, full)
			continue
		}
		entries, err := os.ReadDir(full)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				out = append(out, filepath.Join(full, e.Name()))
			}
		}
	}
	return out, nil
}

// resolveCited answers with the repository file a citation names, tried first from the
// repository root and then from the citing document's own directory. A name that
// resolves nowhere is an illustrative one and answers false.
func resolveCited(root, docDir, cited string) (string, bool) {
	for _, candidate := range []string{filepath.Join(root, cited), filepath.Join(root, docDir, cited)} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return filepath.Clean(candidate), true
		}
	}
	return "", false
}

// readLines returns the file's lines, counting a last line with no newline as a line.
func readLines(path string) ([]string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(body), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines, nil
}
