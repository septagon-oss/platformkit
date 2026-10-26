package ui_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The door checks of this package live in several files, and two of them declare their vocabulary twice:
// servedFloorSection and servedDesignFloorProse both cut the served design floor section out of the README, and
// doorVariable and servedDoor both name an environment door inside it. Every check beside these reads one pair or
// the other, so what the family asks rests on the pairs agreeing: a door named by one pattern and not the other, or
// a section ending earlier for one cutter than for the other, leaves one check asking what its neighbours never
// learn to ask, with the same prose in place and no refusal anywhere.
//
// TestTheDoorChecksOfThisPackageReadOneSectionAndOneDoorPattern keeps that resting point: the cutters return the
// same text, and the two patterns name the same doors of that section and of door-shaped names it does not use
// today, so a divergence cannot hide behind the handful of doors documented now. Each difference is named, and the
// fix is what the later checks already do: keep one section cutter and one door pattern.
func TestTheDoorChecksOfThisPackageReadOneSectionAndOneDoorPattern(t *testing.T) {
	section := servedDesignFloorProse(t)
	if other := servedFloorSection(t); other != section {
		t.Errorf("the door checks cut the served design floor section apart: one stops %d bytes in and the other %d, so a door between the two is documented for some checks and not for others",
			len(other), len(section))
	}
	if apart := doorsApart(doorVariable, servedDoor, section); apart != "" {
		t.Errorf("the section's doors are named apart — %s — so one check learns of a door the others never ask about: keep one door pattern for these checks", apart)
	}
	if apart := doorsApart(doorVariable, servedDoor, doorNamesProbe()); apart != "" {
		t.Errorf("two door patterns read a door-shaped name apart — %s — so a door documented tomorrow could be named by one check and missed by the rest: keep one door pattern for these checks", apart)
	}
}

// doorNamesProbe builds door-shaped names at run time, so this file spells no documented door in its own source
// while still asking the two patterns about doors the section does not use today.
func doorNamesProbe() string {
	return strings.Join([]string{
		strings.Join([]string{"DESIGN", "GATE"}, "_"),
		strings.Join([]string{"DESIGN", "GATE", "OUT"}, "_"),
		strings.Join([]string{"PLATFORMKIT", "GATE", "DUMP", "DIR"}, "_"),
		strings.Join([]string{"DESIGN", "GATES"}, "_"),
		strings.Join([]string{"GATE", "OUT"}, "_"),
		strings.ToLower(strings.Join([]string{"design", "gate", "out"}, "_")),
	}, "\n")
}

// doorsApart names how the two patterns disagree over text — which doors each is seen naming where the other is
// not — or returns "" when they name the same doors, in any order.
func doorsApart(first, second *regexp.Regexp, text string) string {
	firstFound, secondFound := namedDoors(first, text), namedDoors(second, text)
	var apart []string
	for _, pair := range []struct {
		who            string
		found, against []string
	}{{"the first names", firstFound, secondFound}, {"the second names", secondFound, firstFound}} {
		var extra []string
		for _, name := range pair.found {
			if !slices.Contains(pair.against, name) {
				extra = append(extra, name)
			}
		}
		if len(extra) > 0 {
			apart = append(apart, pair.who+" "+strings.Join(extra, ", "))
		}
	}
	return strings.Join(apart, " while ")
}

// namedDoors lists the doors a pattern finds in text, once each, in the order it found them.
func namedDoors(pattern *regexp.Regexp, text string) []string {
	var out []string
	for _, found := range pattern.FindAllString(text, -1) {
		if !slices.Contains(out, found) {
			out = append(out, found)
		}
	}
	return out
}
