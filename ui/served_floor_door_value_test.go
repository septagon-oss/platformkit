package ui_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// A documented door promises that the path it carries reaches the process which measures the served
// pages. The wiring pin judges the door by the presence of its name on a line that starts a process, and a
// name inside a string literal is not the value the environment handed over: a hard-coded path spelled like
// the variable keeps that pin green while the path read from the environment goes nowhere.
//
// TestEachDocumentedDoorHandsTheGatePathToTheProcess asks the stronger question of each documented door
// naming where the gate lives: does some file take that door out of the environment and start a process on a
// line where the variable appears outside every string literal? Doors naming where the gate's dumps go are
// left to the wiring pin, because their value reaches a written file rather than a command line. The case
// stays per file, so it says nothing about a value carried through a function or about the emptiness check a
// call sits under.
func TestEachDocumentedDoorHandsTheGatePathToTheProcess(t *testing.T) {
	named := map[string]bool{}
	for _, door := range servedDoor.FindAllString(servedDesignFloorProse(t), -1) {
		named[door] = true
	}
	if len(named) == 0 {
		t.Fatal("the served design floor section names no door to the loop's gate")
	}
	readers := gatePathReaders(t)
	for _, door := range sortedNames(named) {
		if strings.HasSuffix(door, "_OUT") {
			t.Logf("%s names where the gate's dumps go rather than where the gate is: the wiring pin judges it", door)
			continue
		}
		var handed, read []string
		for _, reader := range readers {
			if reader.hands[door] {
				handed = append(handed, reader.path)
			}
			if _, ok := reader.variables[door]; ok {
				read = append(read, reader.path)
			}
		}
		if len(handed) == 0 {
			t.Errorf("documented door %s never reaches a process as the value read from the environment: %d file(s) read it — %s — and none names the variable it read on a line that starts a process", door, len(read), strings.Join(read, ", "))
			continue
		}
		t.Logf("%s is handed to a process by %s", door, strings.Join(handed, ", "))
	}
}

// gateReader is one file that takes environment variables out of the environment, and the doors whose value
// it can be seen handing to a process.
type gateReader struct {
	path      string
	variables map[string]string
	hands     map[string]bool
}

// gatePathReaders reads every Go test and Playwright journey in this repository once, so each door is judged
// against the code that reads it rather than against prose.
func gatePathReaders(t *testing.T) []gateReader {
	t.Helper()
	var readers []gateReader
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
		assignments := append(goDoor.FindAllStringSubmatch(text, -1), tsDoor.FindAllStringSubmatch(text, -1)...)
		if len(assignments) == 0 {
			return nil
		}
		variables := map[string]string{}
		for _, assign := range assignments {
			variables[assign[2]] = assign[1]
		}
		reader := gateReader{path: path, variables: variables, hands: map[string]bool{}}
		words := map[string]*regexp.Regexp{}
		for _, line := range strings.Split(text, "\n") {
			code := literalText.ReplaceAllString(line, "")
			if !processStart.MatchString(code) {
				continue
			}
			for door, variable := range variables {
				word, ok := words[variable]
				if !ok {
					word = regexp.MustCompile(`\b` + regexp.QuoteMeta(variable) + `\b`)
					words[variable] = word
				}
				if word.MatchString(code) {
					reader.hands[door] = true
				}
			}
		}
		readers = append(readers, reader)
		return nil
	})
	if err != nil {
		t.Fatalf("read the tree for the files that take a documented door out of the environment: %v", err)
	}
	return readers
}

// literalText matches a string literal in Go, TypeScript or JavaScript. Its contents are blanked before a
// line is searched for a variable, so only an identifier the code reads survives.
var literalText = regexp.MustCompile("\"(?:[^\"\\\\]|\\\\.)*\"|'(?:[^'\\\\]|\\\\.)*'|`(?:[^`\\\\]|\\\\.)*`")

// sortedNames puts door names in order, so the report names them the same way twice.
func sortedNames(names map[string]bool) []string {
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
