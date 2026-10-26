package ui_test

import (
	"errors"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// A documented door naming where the loop's gate writes is a promise about a directory, not about a
// command line: its value reaches the files a journey writes, never the process it starts. A process
// start therefore says nothing about it — a file can start a process with another door's value while
// this door's read feeds nothing at all, and a check built on process starts reads that as wired.
//
// TestEachDocumentedDumpDoorHandsItsPathToTheWrite asks the question that fits a dump door: does the
// file which takes it out of the environment write a file at a path built from the variable it read?
// The answer is per file, as its sibling door checks report it: a value carried into a helper of its
// own moves the write target along with the read, and a section that documents no dump door at all
// leaves nothing here to ask.
func TestEachDocumentedDumpDoorHandsItsPathToTheWrite(t *testing.T) {
	var dumps []string
	for _, door := range servedDoor.FindAllString(servedDesignFloorProse(t), -1) {
		if strings.HasSuffix(door, "_OUT") && !slices.Contains(dumps, door) {
			dumps = append(dumps, door)
		}
	}
	if len(dumps) == 0 {
		t.Log("the served design floor section documents no door naming where the gate's dumps go")
		return
	}
	sort.Strings(dumps)
	readers := gatePathReaders(t)
	for _, door := range dumps {
		var written, read []string
		for _, reader := range readers {
			variable, ok := reader.variables[door]
			if !ok {
				continue
			}
			read = append(read, reader.path)
			if writesAtVariable(t, reader.path, door, variable) {
				written = append(written, reader.path)
			}
		}
		if len(written) == 0 {
			t.Errorf("documented dump door %s hands its path to no write: %d file(s) read it — %s — and none builds a write target from the variable it read", door, len(read), strings.Join(read, ", "))
			continue
		}
		t.Logf("%s is written to by %s", door, strings.Join(written, ", "))
	}
}

// fileWrite names how this repository writes a file: a directory made with os.Mkdir* or mkdirSync, or
// bytes put down with os.Write*/writeFileSync. Only an identifier survives on the line, because the
// search runs over the code with every string literal blanked out.
var fileWrite = regexp.MustCompile(`\b(?:os\.(?:MkdirAll|WriteFile|Create|OpenFile)|mkdirSync|writeFileSync|appendFileSync)\b`)

// writesAtVariable reports whether one file writes at a path taken from the named variable.
func writesAtVariable(t *testing.T, path, door, variable string) bool {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			t.Logf("%s disappeared while the dump door %s was being judged", path, door)
			return false
		}
		t.Errorf("read %s to judge the dump door %s: %v", path, door, err)
		return false
	}
	word := regexp.MustCompile(`\b` + regexp.QuoteMeta(variable) + `\b`)
	for _, line := range strings.Split(string(source), "\n") {
		code := literalText.ReplaceAllString(line, "")
		if fileWrite.MatchString(code) && word.MatchString(code) {
			return true
		}
	}
	return false
}
