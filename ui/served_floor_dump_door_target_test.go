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

// A door naming where the loop's gate writes is a promise that the dumps land at that path: a directory
// made there and the files written somewhere else satisfies no reading of it. A check that asks only
// whether the value reaches a write-shaped call — and os.Mkdir and mkdirSync make directories, not
// dumps — is therefore satisfied by the weakest line in the file and sleeps while every measured page
// goes elsewhere.
//
// TestEachDocumentedDumpDoorNamesTheFileItWrites asks the question the promise actually makes: does the
// file that takes a documented dump door out of the environment write a file at a name built from the
// value it read? The variable itself counts, and so does a variable assigned from it in the same file,
// because that is how a path is built here. Like the door checks beside it this one reads one file at a
// time and follows assignments rather than data flow: a value carried into another file moves the answer
// there, and a section documenting no dump door at all leaves it nothing to ask.
func TestEachDocumentedDumpDoorNamesTheFileItWrites(t *testing.T) {
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
		var read, named []string
		for _, reader := range readers {
			variable, ok := reader.variables[door]
			if !ok {
				continue
			}
			read = append(read, reader.path)
			if name := fileWrittenAt(t, reader.path, variable); name != "" {
				named = append(named, name)
			}
		}
		if len(named) == 0 {
			t.Errorf("documented dump door %s names no file it writes: %d file(s) read it — %s — and none writes bytes at a path built from the variable it read", door, len(read), strings.Join(read, ", "))
			continue
		}
		t.Logf("%s names the file written by %s", door, strings.Join(named, ", "))
	}
}

// fileBytes puts bytes on disk: os.Write*/Create/OpenFile in a Go test, writeFile/appendFileSync and
// their async forms in a Playwright journey. Making a directory is deliberately absent — the dump door
// promises the file, and a route that hands the directory to the gate itself belongs to the door checks
// that read a process start.
var fileBytes = regexp.MustCompile(`\b(?:os\.(?:WriteFile|Create|OpenFile)|writeFileSync|writeFile|appendFileSync|appendFile|createWriteStream)\b`)

// doorAssignment names one variable and what it was assigned, in Go or in TypeScript/JavaScript.
var doorAssignment = regexp.MustCompile(`(?m)^\s*(?:const |let |var )?([A-Za-z_]\w*)\s*:?=\s*(.*)$`)

// fileWrittenAt returns the file's path when it writes bytes at a name built from variable, by any chain
// of assignments inside that one file, and the empty string when it does not. A file that went away while
// the tree was being read is skipped; any other read failure is reported.
func fileWrittenAt(t *testing.T, path, variable string) string {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("read %s to judge the dump door variable %s: %v", path, variable, err)
		}
		return ""
	}
	var code []string
	assigned := map[string]string{}
	for _, line := range strings.Split(string(source), "\n") {
		blanked := literalText.ReplaceAllString(line, "")
		code = append(code, blanked)
		if match := doorAssignment.FindStringSubmatch(blanked); match != nil {
			assigned[match[1]] = match[2]
		}
	}
	built := map[string]bool{variable: true}
	for range 6 {
		grew := false
		for name, value := range assigned {
			if built[name] {
				continue
			}
			for from := range built {
				if matchedWord(from, value) {
					built[name] = true
					grew = true
					break
				}
			}
		}
		if !grew {
			break
		}
	}
	for _, line := range code {
		if !fileBytes.MatchString(line) {
			continue
		}
		for name := range built {
			if matchedWord(name, line) {
				return path
			}
		}
	}
	return ""
}

// matchedWord reports whether name stands as its own identifier in a line of code.
func matchedWord(name, line string) bool {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`).MatchString(line)
}
