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

// The door naming where the loop's gate writes tells a caller where the run's dumps are, and the checks
// beside this one answer it per file: they ask whether the file that reads the door writes bytes at a name
// built from the value it read. One such write answers them, so a journey can go on writing one dump there
// while the pages it measures land in a directory no caller named — the gate still measures them, and
// nothing reports that the door lied. A line of prose naming a write answers them too, because those two
// checks search code that keeps its comments.
//
// TestEachFileThatReadsADumpDoorWritesOnlyWhereItSays asks what that door leaves no room for: in every file
// that takes a documented dump door out of the environment, each call that puts bytes on disk writes at a
// name built from the value that file read, at least one call does, and a call whose target is written out
// by hand is reported by the line that makes it. A call is a call here: naming the function in an import
// line or in a comment says nothing about where anything goes. As in the checks beside it, a name counts
// when the file assigns it from the door or from another name that counts, and only inside that one file —
// a value carried into a helper of its own moves the answer there, and a section documenting no dump door
// leaves this nothing to ask.
func TestEachFileThatReadsADumpDoorWritesOnlyWhereItSays(t *testing.T) {
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
	judged := 0
	for _, door := range dumps {
		for _, reader := range readers {
			variable, ok := reader.variables[door]
			if !ok {
				continue
			}
			judged++
			strays, inside := writesAtTheDoor(t, reader.path, variable)
			if inside == 0 {
				t.Errorf("%s reads the documented dump door %s into %s and puts bytes on disk nowhere built from it: no call writes there, whatever the file says about it", reader.path, door, variable)
			}
			for _, line := range strays {
				t.Errorf("%s reads the documented dump door %s into %s and puts bytes on disk at a name no dump door named: %s", reader.path, door, variable, line)
			}
		}
	}
	if judged == 0 {
		t.Logf("no file reads the documented dump door(s) %s, which the check beside this one reports", strings.Join(dumps, ", "))
	}
}

// writesBytes puts bytes on disk at a target the line names: a call to os.Write*, os.Create or os.OpenFile
// in a Go file, or to writeFile, appendFile and createWriteStream in a journey. Naming one of them in an
// import line or a comment is not a call, and making a directory is not bytes.
var writesBytes = regexp.MustCompile(`\b(?:os\.(?:WriteFile|Create|OpenFile)|writeFileSync|writeFile|appendFileSync|appendFile|createWriteStream)\s*\(`)

// writesAtTheDoor reads the file at path and reports the lines that put bytes on disk at a name the file
// did not build from variable, together with how many calls wrote at a name it did build, over any chain of
// assignments inside that one file. A file that went away while the tree was being read is skipped; any
// other read failure is reported.
func writesAtTheDoor(t *testing.T, path, variable string) (strays []string, inside int) {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("read %s to judge the dump door variable %s: %v", path, variable, err)
		}
		return nil, 0
	}
	var code []string
	assigned := map[string]string{}
	for _, line := range strings.Split(string(source), "\n") {
		// Prose says nothing about where bytes go: keep the code, drop the literals and the comment.
		if text := literalText.ReplaceAllString(line, ""); text != line {
			line = text
		}
		if note := strings.Index(line, "//"); note >= 0 {
			line = line[:note]
		}
		code = append(code, line)
		if match := doorAssignment.FindStringSubmatch(line); match != nil {
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
		if !writesBytes.MatchString(line) {
			continue
		}
		named := false
		for name := range built {
			if matchedWord(name, line) {
				named = true
				break
			}
		}
		if named {
			inside++
			continue
		}
		strays = append(strays, strings.TrimSpace(line))
	}
	return strays, inside
}
