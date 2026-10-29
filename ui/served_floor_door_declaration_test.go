package ui_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The door checks of this package ask their questions of two declarations: a cutter that hands a check the served
// design floor section of the README, and a pattern that names an environment door inside it. Two files declare
// them, and TestTheDoorChecksOfThisPackageReadOneSectionAndOneDoorPattern asks that the two declarations agree.
// That question stays worth asking only while nothing else in the package reaches the same text by a route of its
// own: a third cutter, or a third copy of the door pattern, is compared by no check at all, and the family beside
// it goes on agreeing with itself about a section and a door the newcomer reads apart from both.
//
// TestTheDoorVocabularyOfThisPackageStaysInTwoFiles keeps the vocabulary inside the files that declare it. A file
// of this package outside the pair that names the README path or repeats the door pattern is named with what it
// did, and the fix is to read the section through servedDesignFloorProse and a door through servedDoor. Either
// half of the pair going quiet is refused too, so the guard cannot outlive the code it guards. The pattern is
// recognised by the text of servedDoor's own expression, so this file spells no pattern of its own.
func TestTheDoorVocabularyOfThisPackageStaysInTwoFiles(t *testing.T) {
	declaredIn := []string{"served_floor_doors_test.go", "served_floor_door_wiring_test.go"}
	pattern := doorPatternCore()
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatalf("list the checks of this package: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("this package holds no checks to confine")
	}
	for _, name := range files {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read the check %s: %v", name, err)
		}
		opened, repeated := 0, 0
		for _, line := range strings.Split(string(source), "\n") {
			if strings.Contains(line, readmePathQuoted()) {
				opened++
			}
			if declaresDoorPattern(line, pattern) {
				repeated++
			}
		}
		if !slices.Contains(declaredIn, name) {
			if opened > 0 {
				t.Errorf("%s names the README path on %d line(s), where the served design floor section is cut: the door checks read that section through servedDesignFloorProse or servedFloorSection, so a cutter here asks about doors no check beside these ever learns of", name, opened)
			}
			if repeated > 0 {
				t.Errorf("%s repeats the door pattern of the served design floor checks on %d line(s): name a door through servedDoor, whose two declarations TestTheDoorChecksOfThisPackageReadOneSectionAndOneDoorPattern compares", name, repeated)
			}
			continue
		}
		if opened == 0 {
			t.Errorf("%s is named as one of the two files that cut the served design floor section, but names no README path: the cutter the checks read has moved and this list has not", name)
		}
		if repeated == 0 {
			t.Errorf("%s is named as one of the two files that declare the door pattern, but declares no pattern built on %q: the declarations TestTheDoorChecksOfThisPackageReadOneSectionAndOneDoorPattern compares have moved", name, pattern)
		}
	}
}

// readmePathQuoted names the README path as it is written in the source of a file that opens it, with the quotes
// that delimit a Go literal either side. It is joined from parts, in the manner of a door name no check spells,
// so that this file does not name the path it searches for and refuse itself for naming it.
func readmePathQuoted() string {
	quote := `"`
	return quote + "READ" + "ME" + "." + "md" + quote
}

// declaresDoorPattern reports whether a line declares a pattern built on the class of name the door checks name a
// documented door with, which is what a copy of it repeats whatever it adds or drops at either end.
func declaresDoorPattern(line, core string) bool {
	return strings.Contains(line, "regexp.MustCompile(") && strings.Contains(line, core)
}

// doorPatternCore returns servedDoor's pattern up to and including the word that makes a door a gate, with the
// leading word-boundary escape cut off. Reading a copy this way keeps the rule about the pattern the checks share
// rather than about the text of this file, which spells no door name at all.
func doorPatternCore() string {
	pattern := servedDoor.String()
	if end := strings.LastIndex(pattern, "GATE"); end >= 0 {
		pattern = pattern[:end+len("GATE")]
	}
	return strings.TrimPrefix(pattern, `\b`)
}
