package ui_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The dump-door questions are asked of a door only once the walk has found a file that takes it out of the
// environment, and the two halves of that condition are read apart: one case asks whether any file at all takes a
// documented door, the per-door cases ask only of the doors the walk returned, and the section's links are read as
// a set of their own. A documented door whose only reader stops existing therefore leaves the per-door cases with
// nothing to ask of it, while the paragraph goes on naming a route that hands the gate's dumps to nobody.
//
// TestEachDocumentedDumpDoorIsTakenByAFileTheSectionPointsAt joins the halves for the doors naming where the
// gate's dumps go, over the section, its links and the walk the checks beside this one use: some file must take
// each documented dump door out of the environment and that file must be one the section links, so the prose that
// documents a route names the code that keeps it. A door with no such reader is named, and the fix is to link the
// file that places the gate's dumps or to read the door in a file the section links. A section documenting no dump
// door leaves the question to the cases beside this one, which say so.
func TestEachDocumentedDumpDoorIsTakenByAFileTheSectionPointsAt(t *testing.T) {
	section := servedDesignFloorProse(t)
	var dumps []string
	for _, door := range servedDoor.FindAllString(section, -1) {
		if strings.HasSuffix(door, "_OUT") && !slices.Contains(dumps, door) {
			dumps = append(dumps, door)
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
	readers := gatePathReaders(t)
	for _, door := range dumps {
		var answerers []string
		for _, reader := range readers {
			if _, ok := reader.variables[door]; !ok {
				continue
			}
			if where, err := filepath.Abs(reader.path); err != nil {
				t.Errorf("place %s where the walk found it: %v", reader.path, err)
			} else if pointed[filepath.Clean(where)] {
				answerers = append(answerers, reader.path)
			}
		}
		if len(answerers) == 0 {
			t.Errorf("documented dump door %s is taken out of the environment by no file the served design floor section points at, so the paragraph that documents the route names no code that keeps it: link the file that places the gate's dumps from that section, or read the door in a file it links", door)
			continue
		}
		t.Logf("%s is taken out of the environment by %s, which the section points at", door, strings.Join(answerers, ", "))
	}
}
