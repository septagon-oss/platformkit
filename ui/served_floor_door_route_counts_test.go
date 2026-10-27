package ui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheDoorGuardCountsEachRouteByAMarkerOfItsOwnOverEverySourceItWalks holds the two counts of the door
// guard's reach refusal apart over the text that refusal reads.
//
// The guard confines this package's door vocabulary by walking the checks it reads, and the reach beside it
// walks the sources beside those checks, counting the lines of each that name the guide and the lines that
// repeat the door pattern, so a source reading the served design floor by a route of its own is named with the
// count of the route it took. One count per route means two markers, and the assertion beside this one holds
// them apart on three lines built from nothing. Held there, they are still untried over the text the walk
// actually reads: a marker that answers to a line of the other route too would raise the count of a route an
// added line never took, and a marker that answers to nothing would let the second declaration of the doors it
// searches for travel unheard under a zero it never earned.
//
// So each source the reach walks is counted once as it stands, then counted again with one line added of each
// route and one line of neither, and each added line must raise its own count by exactly one and leave the
// other where it was, whatever the source already holds. Inputs: the sources of this directory that are not
// checks, through the same listing the walk uses, read only; a directory whose sources are all checks is
// refused, because then the refusal whose counts these are could name no file at all. A source that cannot be
// read is refused. Nothing here writes, and whatever the corpus is the counts move by one.
func TestTheDoorGuardCountsEachRouteByAMarkerOfItsOwnOverEverySourceItWalks(t *testing.T) {
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list the sources of this package: %v", err)
	}
	core := doorPatternCore()
	added := []struct {
		route          string
		line           string
		opens, repeats int
	}{
		{"names the guide", "\tread, err := os.ReadFile(" + readmePathQuoted() + ")", 1, 0},
		{"repeats the door pattern", "var third = regexp.MustCompile(`" + servedDoor.String() + "`)", 0, 1},
		{"takes neither route", "\tmeasure := width * 2 / 3", 0, 0},
	}
	walked := 0
	for _, name := range sources {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Errorf("read the source %s of this package, which the door guard's reach walks too: %v", name, err)
			continue
		}
		walked++
		held := doorGuardRouteCounts(string(source), core)
		for _, route := range added {
			got := doorGuardRouteCounts(string(source)+"\n"+route.line, core)
			if got[0] != held[0]+route.opens || got[1] != held[1]+route.repeats {
				t.Errorf("%s holds %d line(s) naming the guide and %d repeating the door pattern, and adding one line that %s counts them as %d and %d: one line of one route must raise that route's count by one and leave the other alone, so a marker of the reach above answers to a route its own count does not name, and the refusal prints the count of a route the refused file never took — count each route by a marker of its own",
					name, held[0], held[1], route.route, got[0], got[1])
			}
		}
	}
	if walked == 0 {
		t.Errorf("the door guard's reach walks no source of this package outside its checks, so the refusal whose two counts these markers answer to could name no file at all")
	}
}

// doorGuardRouteCounts counts the lines of one source the two markers of the reach answer to: the lines naming
// the guide as a quoted path, and the lines declaring a compiled pattern built on the class of name a door
// holds. Each count comes from one marker, which is what the case above holds of them.
func doorGuardRouteCounts(source, core string) [2]int {
	var counts [2]int
	for _, line := range strings.Split(source, "\n") {
		if strings.Contains(line, readmePathQuoted()) {
			counts[0]++
		}
		if declaresDoorPattern(line, core) {
			counts[1]++
		}
	}
	return counts
}
