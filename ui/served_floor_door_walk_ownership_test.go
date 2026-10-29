package ui_test

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The dump-door checks read the whole tree for the files that take a documented door out of the environment, and
// the reading matches a door's name wherever it appears, inside a string literal included. A check that spells a
// documented door inside the source of a fixture it writes for its own questions is therefore taken for a
// journey: the walk asks that check where it writes the gate's dumps, the fixture is read as the code that places
// them, and the journey the check was written to judge goes unasked. The checks here keep their fixtures away
// from that spelling — a journey fixture names the directory its caller handed it, and lands outside the tree.
//
// TestNoCheckOfTheDoorsIsItselfTakenForAJourney asks both halves of that arrangement, of the walk these checks
// share and over the same doors they document: that it reaches at least one file taking a dump door out of the
// environment, so the questions beside this one are asked of a real journey rather than of an empty reader list,
// and that no reader it reports lives in this package, where the checks and the fixtures they write live. A
// reader found here is named with the documented doors it takes, so the fix is to name a door only in the file
// that writes the gate's dumps, or to blank string literals in the walk before it matches a name.
func TestNoCheckOfTheDoorsIsItselfTakenForAJourney(t *testing.T) {
	here, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("name the directory this package's checks and their fixtures live in: %v", err)
	}
	var dumps []string
	for _, door := range servedDoor.FindAllString(servedDesignFloorProse(t), -1) {
		if strings.HasSuffix(door, "_OUT") && !slices.Contains(dumps, door) {
			dumps = append(dumps, door)
		}
	}
	readers := gatePathReaders(t)
	reached := 0
	for _, reader := range readers {
		taken := false
		for _, door := range dumps {
			if _, ok := reader.variables[door]; ok {
				taken = true
			}
		}
		if !taken {
			continue
		}
		reached++
		path, err := filepath.Abs(reader.path)
		if err != nil {
			t.Errorf("place %s where the walk found it: %v", reader.path, err)
			continue
		}
		if filepath.Dir(path) != here {
			continue
		}
		var doors []string
		for _, door := range dumps {
			if _, ok := reader.variables[door]; ok {
				doors = append(doors, door)
			}
		}
		sort.Strings(doors)
		t.Errorf("%s takes the documented dump door(s) %s out of the environment, so the walk judges a check of these doors as the journey and never asks the journey it was written for: name a dump door only in the file that writes the gate's dumps, or blank string literals in the walk before it matches a name", reader.path, strings.Join(doors, ", "))
	}
	if len(dumps) > 0 && reached == 0 {
		t.Errorf("the walk reported %d file(s) and none of them takes the documented dump door(s) %s out of the environment, so nothing is asked about where the gate's dumps land", len(readers), strings.Join(dumps, ", "))
	}
}
