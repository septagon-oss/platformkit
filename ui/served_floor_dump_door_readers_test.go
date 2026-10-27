package ui_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The dump-door checks answer only the doors the served design floor section names: each of them reads the
// section for a door whose value goes to a written file, and asks of the file that takes it out of the
// environment where the gate's dumps land. Two silences follow from answering a name read out of prose. A door
// the tree takes but the section no longer names is answered by none of them, so the gate's dumps land wherever
// that journey puts them. And a file of this package that takes such a door — a check which spells the door
// inside the fixture it writes for these questions — is collected as a reader, so the questions are asked of the
// check and never of the journey it wrote.
//
// TestEveryFileThatTakesTheGateDumpsIsAFileTheSectionPointsAt answers both, tree-wide and in either direction:
// every dump door a file takes out of the environment must be documented here, and every file that takes a
// documented dump door must be a file the section itself points at. A door read nowhere documented names the
// files that read it, and a reader the section points at none of names the door it read, so the fix reads off
// either message — document the route, or place the gate's dumps in the one file the section links for them. It
// composes the walk, the section and the section's links the checks beside this one use, and walks nothing of
// its own.
func TestEveryFileThatTakesTheGateDumpsIsAFileTheSectionPointsAt(t *testing.T) {
	section := servedDesignFloorProse(t)
	documented := map[string]bool{}
	for _, door := range servedDoor.FindAllString(section, -1) {
		if strings.HasSuffix(door, "_OUT") {
			documented[door] = true
		}
	}
	pointed := map[string]bool{}
	for _, link := range markdownTargets(t, section) {
		where, err := filepath.Abs(link)
		if err != nil {
			t.Errorf("place the link %s where the served design floor section points it: %v", link, err)
			continue
		}
		pointed[filepath.Clean(where)] = true
	}
	taken := map[string][]string{}
	for _, reader := range gatePathReaders(t) {
		doors := make([]string, 0, len(reader.variables))
		for door := range reader.variables {
			doors = append(doors, door)
		}
		slices.Sort(doors)
		for _, door := range doors {
			if !servedDoor.MatchString(door) || !strings.HasSuffix(door, "_OUT") {
				continue
			}
			taken[door] = append(taken[door], reader.path)
		}
	}
	named := make([]string, 0, len(taken))
	for door := range taken {
		named = append(named, door)
	}
	slices.Sort(named)
	for _, door := range named {
		readers := taken[door]
		if !documented[door] {
			t.Errorf("%s is taken out of the environment by %d file(s) — %s — but the served design floor section documents no such door, so none of the dump-door checks asks where these dumps land: document the route the way the other doors are documented, or hand the gate's dumps to a door the section names",
				door, len(readers), strings.Join(readers, ", "))
			continue
		}
		var unpointed []string
		for _, reader := range readers {
			where, err := filepath.Abs(reader)
			if err != nil {
				t.Errorf("place %s where the walk found it: %v", reader, err)
				continue
			}
			if !pointed[filepath.Clean(where)] {
				unpointed = append(unpointed, reader)
			}
		}
		if len(unpointed) > 0 {
			t.Errorf("documented dump door %s is taken out of the environment by file(s) the served design floor section points at none of — %s — so the walk judges a check of these doors as the journey and never asks the journey it was written for: name the door only in the file that places the gate's dumps, or link that file from the section",
				door, strings.Join(unpointed, ", "))
		}
		t.Logf("%s is taken out of the environment by %s", door, strings.Join(readers, ", "))
	}
}
