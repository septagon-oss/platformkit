package ui_test

import (
	"os"
	"strings"
	"testing"
)

// TestTheDoorVocabularyGuardSearchesForWhatItNames holds the ground under the guard that confines this package's
// door vocabulary. That guard finds a second cutter by matching a joined spelling of the guide's path, and finds a
// second door pattern by matching a class name cut out of servedDoor's own expression. Neither string is written
// out, so nothing else asks whether the join still lands on a file that exists or whether the cut still falls
// where the guard says it does. If the guide moved, or the word that makes a door a gate left servedDoor's
// expression, the guard would go on passing while searching for text no file holds — a keeper would be refused for
// naming a path, or tolerated for hiding one, on the strength of a string that names nothing.
//
// Both halves have a passing branch today: the joined path is a file of this package, and the cut core is a strict
// prefix of the expression it is cut from, ending at the word a door name is expected to hold. A copy of the door
// pattern is then recognised whatever it adds or drops at either end, which is what the guard beside this one
// claims, and refuses the day either basis moves.
func TestTheDoorVocabularyGuardSearchesForWhatItNames(t *testing.T) {
	// The path the vocabulary guard searches for, with the quotes that delimit a Go literal either side, must
	// name a file of this package: the guards read this guide to ask about the doors it documents.
	guide := strings.Trim(readmePathQuoted(), `"`)
	if guide == "" {
		t.Fatal(`the door vocabulary guard searches for no path, so no second cutter can be named`)
	}
	info, err := os.Stat(guide)
	if err != nil {
		t.Errorf("the door vocabulary guard searches for %q, which is no file of this package: %v — a file that reads the guide would be refused for naming it or tolerated for hiding it on the strength of a name nothing answers", guide, err)
	} else if info.IsDir() {
		t.Errorf("the door vocabulary guard searches for %q, which is a directory, not the guide the door checks cut", guide)
	}

	// The class name the vocabulary guard recognises a copy by must still be cut out of the pattern the checks
	// read doors with, at the word a door name holds: a proper prefix of it, so a copy that widens either end is
	// still recognised as that copy, and a copy narrowed at the far end still repeats.
	pattern := servedDoor.String()
	core := doorPatternCore()
	switch {
	case core == "":
		t.Error("the door vocabulary guard recognises a pattern built on nothing, so every compiled pattern of this package is a copy of the door pattern")
	case !strings.Contains(pattern, core):
		t.Errorf("the door vocabulary guard recognises a copy by %q, which is no part of the pattern these checks name doors with (%q): a third pattern would be refused or tolerated for text that is not the one it shares with these two", core, pattern)
	case core == pattern:
		t.Errorf("the door vocabulary guard cuts the whole of servedDoor's pattern as the mark of a copy, so a third pattern that drops anything from either end repeats nothing it is searched for")
	case !strings.HasSuffix(core, strings.Join([]string{"G", "ATE"}, "")):
		t.Errorf("the door vocabulary guard cuts its mark of a copy at %q, which does not end at the word a door name holds: the word left servedDoor's expression and the guard now compares text no door is named by", core)
	}

	// And the cut must still do its work on the shapes it is asked about: a copy of the pattern and a copy of it
	// narrowed at the far end are both recognised, while a compiled pattern that names no door is not.
	shared := `var shared = regexp.MustCompile(` + "`" + pattern + "`" + `)`
	if !declaresDoorPattern(shared, core) {
		t.Errorf("a byte for byte copy of the door pattern is not recognised as one by the mark %q, so a third declaration of it agrees with no one and no check says so", core)
	}
	narrowed := "var narrowed = regexp.MustCompile(`" + strings.TrimSuffix(pattern, `(?:_[A-Z0-9]+)*\b`) + "`)"
	if !declaresDoorPattern(narrowed, core) {
		t.Errorf("a door pattern narrowed at its far end is not recognised as a copy by the mark %q, so the copy that stops naming a suffixed door is the one no check beside these learns of", core)
	}
	aside := "var aside = regexp.MustCompile(`^no door at all$`)"
	if declaresDoorPattern(aside, core) {
		t.Errorf("a compiled pattern that names no door is refused as a copy of the door pattern by the mark %q, so the guard confines compiled patterns rather than the vocabulary", core)
	}
}
