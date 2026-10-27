package ui_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// A door named in the served design floor section is a promise that an environment variable
// hands a test the path of the loop's own gate. The section keeps faith with it only while the
// code behind the door still runs that gate: a test may go on reading the variable after the
// call that used it is gone, and then the suite stays green while the section describes a route
// that leads nowhere.
//
// TestEachDocumentedDoorStillHandsItsCallerTheGate reads the section for the doors it names, then
// asks of each one: does some file take it out of the environment, and does that file start a
// process with a value the environment gave it? A door is refused when its name survives in a
// file that runs nothing with it, which is what a documented route that no longer runs the gate
// looks like. The check is per file, so it says no more than the section's sentence does: it does
// not follow a value through a function or prove the call sits under the environment check.

// servedDoor names an environment variable that carries a path, in the section or in a file.
var servedDoor = regexp.MustCompile(`\b[A-Z][A-Z0-9]*_(?:[A-Z0-9]+_)*GATE(?:_[A-Z0-9]+)*\b`)

// goDoor and tsDoor match a file taking a door out of the environment, and name the variable
// that holds it.
var (
	goDoor = regexp.MustCompile(`(?m)(\w+)\s*:?=\s*os\.Getenv\("([A-Z][A-Z0-9_]+)"\)`)
	tsDoor = regexp.MustCompile(`(?m)(?:const|let|var)\s+(\w+)\s*=\s*process\.env\.([A-Z][A-Z0-9_]+)`)
)

// processStart is how this repository runs the loop's gate: an external command in a Go test, a
// synchronous child process in a Playwright journey.
var processStart = regexp.MustCompile(`exec\.Command|execFileSync|spawnSync|\bspawn\(`)

// doorFile is one source file that mentions a documented door: which doors it reads out of the
// environment, and whether it starts a process with what it read.
type doorFile struct {
	path      string
	reads     map[string]bool
	runsValue bool
}

func servedDesignFloorProse(t *testing.T) string {
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

// doorFiles maps each environment variable name a Go test or a Playwright journey mentions to
// the files that mention it, so a door is judged against the code that reads it and not against
// a claim in prose.
func doorFiles(t *testing.T) map[string]map[string]doorFile {
	t.Helper()
	byDoor := map[string]map[string]doorFile{}
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
		text := string(source)
		names := servedDoor.FindAllString(text, -1)
		if len(names) == 0 {
			return nil
		}
		file := doorFile{path: path, reads: map[string]bool{}}
		variables := map[string]bool{}
		for _, assign := range append(goDoor.FindAllStringSubmatch(text, -1), tsDoor.FindAllStringSubmatch(text, -1)...) {
			file.reads[assign[2]] = true
			variables[assign[1]] = true
		}
		for _, line := range strings.Split(text, "\n") {
			if !processStart.MatchString(line) {
				continue
			}
			for variable := range variables {
				if regexp.MustCompile(`\b` + regexp.QuoteMeta(variable) + `\b`).MatchString(line) {
					file.runsValue = true
				}
			}
		}
		for _, name := range names {
			if files := byDoor[name]; files != nil {
				files[path] = file
			} else {
				byDoor[name] = map[string]doorFile{path: file}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the tree for the files that read the documented doors: %v", err)
	}
	return byDoor
}

func TestEachDocumentedDoorStillHandsItsCallerTheGate(t *testing.T) {
	named := servedDoor.FindAllString(servedDesignFloorProse(t), -1)
	if len(named) == 0 {
		t.Fatal("the served design floor section names no door to the loop's gate")
	}
	byDoor := doorFiles(t)
	seen := map[string]bool{}
	for _, door := range named {
		if seen[door] {
			continue
		}
		seen[door] = true
		files := byDoor[door]
		if len(files) == 0 {
			t.Errorf("documented door %s is read by no test or journey in this repository", door)
			continue
		}
		var wired []string
		for _, path := range sortedKeys(files) {
			file := files[path]
			if file.reads[door] && file.runsValue {
				wired = append(wired, path)
			}
		}
		if len(wired) == 0 {
			t.Errorf("documented door %s runs nothing: %d file(s) name it and none both reads it from the environment and starts a process with what it read", door, len(files))
			continue
		}
		t.Logf("%s starts a process from %s", door, strings.Join(wired, ", "))
	}
}

// sortedKeys puts a door's files in path order, so the report names them the same way twice.
func sortedKeys(files map[string]doorFile) []string {
	out := make([]string, 0, len(files))
	for path := range files {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}
