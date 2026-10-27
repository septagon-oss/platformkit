package ui_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The served design floor is judged twice: by this repository's port of the loop's
// rules, and — when a caller names where the loop's files are — by the loop's gate
// itself. Both routes are environment doors, so the section that documents the floor
// is the only place a reader can find them. These assertions keep that section honest:
// the doors it names are read by a test that exists, and the files it links exist.

var doorVariable = regexp.MustCompile(`\b[A-Z][A-Z0-9]*_(?:[A-Z0-9]+_)*GATE(?:_[A-Z0-9]+)*\b`)

func servedFloorSection(t *testing.T) string {
	t.Helper()
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read the ui README: %v", err)
	}
	_, rest, found := strings.Cut(string(readme), "## Served design floor")
	if !found {
		t.Fatal("ui/README.md holds no served design floor section")
	}
	if end, _, cut := strings.Cut(rest, "\n## "); cut {
		return end
	}
	return rest
}

// TestTheServedFloorSectionNamesTheDoorsThatRunTheGate keeps the documented route to
// the loop's own gate runnable: each door named in the section is read by some test or
// journey in the tree, and each file the section points at sits at the path it gives.
func TestTheServedFloorSectionNamesTheDoorsThatRunTheGate(t *testing.T) {
	section := servedFloorSection(t)
	var named []string
	seen := map[string]bool{}
	for _, variable := range doorVariable.FindAllString(section, -1) {
		if !seen[variable] {
			seen[variable] = true
			named = append(named, variable)
		}
	}
	if len(named) == 0 {
		t.Fatal("the served design floor section names no door to the loop's gate")
	}
	read := readByTests(t)
	for _, variable := range named {
		if !read[variable] {
			t.Errorf("documented door %s is read by no test or journey in this repository", variable)
		}
	}
	for _, link := range markdownTargets(t, section) {
		if _, err := os.Stat(filepath.Join(link)); err != nil {
			t.Errorf("the served design floor section links %s: %v", link, err)
		}
	}
}

// readByTests collects every environment variable name a Go test or a Playwright
// journey reads, so a door the README invents cannot pass as documented.
func readByTests(t *testing.T) map[string]bool {
	t.Helper()
	read := map[string]bool{}
	err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if name := info.Name(); name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".go", ".ts", ".mjs":
		default:
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, name := range doorVariable.FindAllString(string(source), -1) {
			read[name] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the tree for the doors its tests read: %v", err)
	}
	return read
}

var markdownLink = regexp.MustCompile(`\]\(([^)#]+)(?:#[^)]*)?\)`)

// markdownTargets resolves the section's relative links against ui/, which is where
// the README sits.
func markdownTargets(t *testing.T, section string) []string {
	t.Helper()
	var out []string
	for _, match := range markdownLink.FindAllStringSubmatch(section, -1) {
		target := strings.TrimSpace(match[1])
		if target == "" || strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
			continue
		}
		out = append(out, target)
	}
	return out
}
