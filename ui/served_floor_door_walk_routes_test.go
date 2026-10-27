package ui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheDoorGuardCountsBothRoutesInEveryWalkThatCountsOne holds the two markers of the door vocabulary together
// over every walk of this package that uses one of them.
//
// The refusal that names a source reading the served design floor from outside the door checks prints one count
// for each route — lines naming the guide, lines repeating the door pattern — and prints them from the walk it
// opens itself, not from the case beside this one that counts the same sources a second time to hold the two
// markers apart. A walk that asks a source line about one marker and not the other prints a count of a route it
// never tested, and no check compares the walk that prints with the case that holds, so the two could part with
// every check green. So each walk of this package — a loop over the lines of a source — that asks about one
// marker must ask about the other, and only the whole call of a marker counts as asking: a walk that widens one
// argument or swaps it for another asks a different question of that line, and the count it prints answers to no
// route the refusal names. Inputs: the sources of this directory, read only, through the same listing the walks
// use. A walk that opens with no body to close is refused, as is a directory no walk of which asks about either
// marker, because then nothing here would hold anything at all.
func TestTheDoorGuardCountsBothRoutesInEveryWalkThatCountsOne(t *testing.T) {
	guideCall := "strings." + "Contains(line, readme" + "PathQuoted())"
	doorCall := "declare" + "sDoorPattern(line, "
	// The opening of a walk is joined from parts, in the manner of the guide path the door checks search for, so
	// that this file does not hold that text of its own and refuse itself for walking it.
	walkOpens := "for " + "_, line := "
	walks := 0
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list the sources of this package: %v", err)
	}
	for _, name := range sources {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Errorf("read the source %s of this package, which the door walks read it from: %v", name, err)
			continue
		}
		for rest := string(source); ; {
			at := strings.Index(rest, walkOpens)
			if at < 0 {
				break
			}
			indent := lineIndent(rest[:at])
			body, after, ok := doorWalkBody(rest[at:], indent)
			if !ok {
				t.Errorf("%s opens a walk over the lines of a source with no body for it to close, so how far its two counts reach is not readable", name)
				break
			}
			askedGuide := strings.Contains(body, guideCall)
			askedDoor := strings.Contains(body, doorCall)
			if askedGuide != askedDoor {
				asked, missing := "names the guide by "+guideCall, doorCall
				if !askedGuide {
					asked, missing = "repeats the door pattern by "+doorCall, guideCall
				}
				t.Errorf("%s opens a walk over source lines that %s and never asks the same line by %s: the refusal over a source that takes both routes prints one count for each, so a walk of one marker prints a count of a route it never looked at — count both routes in the walk that counts one, as the case beside this one holds them apart",
					name, asked, missing)
			}
			if askedGuide && askedDoor {
				walks++
			}
			rest = after
		}
	}
	if walks == 0 {
		t.Errorf("no walk of this package asks a source line about the door vocabulary, so the two markers the door checks share are held apart by nothing that reads a source")
	}
}

// lineIndent returns the text between the last newline and the walk's opening keyword, which gofmt leaves as the
// walk's own indentation and which is where the brace closing that walk sits.
func lineIndent(before string) string {
	start := strings.LastIndexByte(before, '\n') + 1
	if indent := before[start:]; strings.TrimSpace(indent) == "" {
		return indent
	}
	return ""
}

// doorWalkBody returns the body of the walk its text opens — from the end of the opening line to the brace that
// closes it at the walk's own indentation — and the text after that brace. A walk with no body is refused. The
// closing brace is read by where it stands rather than by counting braces, because the lines these walks read
// hold braces of their own inside the text they quote.
func doorWalkBody(walk, indent string) (body, after string, ok bool) {
	head := strings.IndexByte(walk, '\n')
	if head < 0 || !strings.Contains(walk[:head], "{") {
		return "", "", false
	}
	rest := walk[head+1:]
	for rest != "" {
		line := rest
		if cut := strings.IndexByte(rest, '\n'); cut >= 0 {
			line, rest = rest[:cut], rest[cut+1:]
		} else {
			rest = ""
		}
		if strings.HasPrefix(line, indent+"}") {
			return body, rest, true
		}
		body += line + "\n"
	}
	return "", "", false
}
