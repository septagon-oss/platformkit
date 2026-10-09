package fault_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestEveryPillarCitationResolvesFromTheFileBeforeIt reads ARCHITECTURE.md's
// `## Every pillar, end to end` the way its reader does: a bare `:N` or
// `:N-M` means a line of the last file the prose named. Every such citation
// must land inside that file, and that file must exist at the repository root's
// path, or the reader has nowhere to look.
func TestEveryPillarCitationResolvesFromTheFileBeforeIt(t *testing.T) {
	root := filepath.Join("..", "..")
	doc, err := os.ReadFile(filepath.Join(root, "ARCHITECTURE.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)
	_, section, ok := strings.Cut(text, "\n## Every pillar, end to end\n")
	if !ok {
		t.Fatal("ARCHITECTURE.md has no `## Every pillar, end to end` section")
	}
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}
	token := regexp.MustCompile("([A-Za-z0-9_./-]+\\.(?:go|sql|md|json|ts|sh|yaml|yml|rego|js)|Makefile|NOTICE)(?::\\d+(?:-\\d+)?)?|`:(\\d+)(?:-(\\d+))?`")
	lines := map[string]int{}
	count := func(path string) (int, bool) {
		if n, ok := lines[path]; ok {
			return n, n >= 0
		}
		body, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			lines[path] = -1
			return 0, false
		}
		n := strings.Count(string(body), "\n")
		if !strings.HasSuffix(string(body), "\n") {
			n++
		}
		lines[path] = n
		return n, true
	}
	last, checked := "", 0
	for _, m := range token.FindAllStringSubmatch(section, -1) {
		if m[1] != "" {
			last = m[1]
			continue
		}
		from, _ := strconv.Atoi(m[2])
		to := from
		if m[3] != "" {
			to, _ = strconv.Atoi(m[3])
		}
		checked++
		n, ok := count(last)
		switch {
		case last == "":
			t.Errorf("%s is cited before any file is named", m[0])
		case !ok:
			t.Errorf("%s follows %q, which is no file at the repository root", m[0], last)
		case from < 1 || to > n:
			t.Errorf("%s follows %s, which has %d lines", m[0], last, n)
		}
	}
	if checked == 0 {
		t.Fatal("found no shorthand citation; the section's convention changed and this test reads nothing")
	}
}
