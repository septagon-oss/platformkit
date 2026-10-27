package ui_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The door naming where the loop's gate writes is answered by two vocabularies in the checks beside this
// one: which names a journey puts bytes on disk through, and how much of a call the per-write question
// reads. Either can go quiet on its own — a writer the reading vocabulary never names answers nothing about
// where the door points, and a call read one line at a time refuses a target a formatter moved to the next
// line — so both are asked here of sources this test writes itself, away from the journey the checks
// judge.
//
// TestTheNamesThatPutBytesOnDiskAreTheNamesTheDoorIsAnsweredAt pins the writer names the per-call question
// refuses or reads: the file-system writers a journey imports and the browser calls that save to a path, with
// the directory makers and the readers out. TestTheDumpDoorCheckJudgesAWholeCallWhereverItsTargetSits pins the
// reading: a call is one call however a formatter laid it out, a path built by template string is built from
// the door as any other, and a path written out by hand, in a comment or as a URL stays a write no door named.

// doorJourney writes one journey into a temporary directory and returns its path. The dump directory arrives
// as a parameter rather than out of the environment: the checks beside this one seed their answer with the
// variable they were handed, and a file that spells a documented door inside a fixture of its own is taken
// for a journey by the reader walk, which then asks its questions of this file and not of the one it wrote.
func doorJourney(t *testing.T, body string) string {
	t.Helper()
	source := "import { mkdirSync, writeFileSync } from 'node:fs';\n" +
		"import { join } from 'node:path';\n\n" +
		"function judge(out: string, name: string, html: string): string {\n" + body + "\n}\n"
	path := filepath.Join(t.TempDir(), "journey.ts")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write a journey for the dump-door check to read: %v", err)
	}
	return path
}

func TestTheNamesThatPutBytesOnDiskAreTheNamesTheDoorIsAnsweredAt(t *testing.T) {
	const source = "import { appendFile, createWriteStream, mkdirSync, mkdtempSync, readFile, rmSync, writeFileSync } from 'node:fs';\n" +
		"import { join } from 'node:path';\n" +
		"mkdirSync(out, { recursive: true });\n" +
		"writeFileSync(file, html);\n" +
		"appendFile(other, report);\n" +
		"createWriteStream(third);\n" +
		"await page.screenshot({ path: shot });\n" +
		"await page.pdf({ path: paper });\n"

	want := []string{"appendFile", "createWriteStream", "pdf", "screenshot", "writeFileSync"}
	got := namesThatPutBytesOnDisk(source)
	if !slices.Equal(got, want) {
		t.Errorf("a journey puts bytes on disk through %s; the dump-door checks answer %s", got, want)
	}
	for _, name := range got {
		if writesBytes.MatchString(name + "(") {
			continue
		}
		if name != "screenshot" && name != "pdf" {
			t.Errorf("%s puts bytes on disk but the per-write check reads no call by that name: the door is answered at every call it reads, and this one is read by none", name)
		}
	}
}

func TestTheDumpDoorCheckJudgesAWholeCallWhereverItsTargetSits(t *testing.T) {
	cases := []struct {
		what   string
		body   string
		strays int
		inside int
	}{
		{
			what:   "the target on the line that names the call",
			body:   "  const file = join(out, `${name}.html`);\n  writeFileSync(file, html);\n  return file;",
			strays: 0, inside: 1,
		},
		{
			what:   "the target a formatter moved to the following line",
			body:   "  const file = join(out, `${name}.html`);\n  writeFileSync(\n    file,\n    html\n  );\n  return file;",
			strays: 0, inside: 1,
		},
		{
			what:   "the path built by template string out of the door",
			body:   "  writeFileSync(`${out}/${name}.html`, html);\n  return file;",
			strays: 0, inside: 1,
		},
		{
			what:   "the path built by template string, its call broken in two",
			body:   "  writeFileSync(`${out}/${name}.html`,\n    html\n  );\n  return file;",
			strays: 0, inside: 1,
		},
		{
			what:   "the path written out by hand, and nothing written at the door",
			body:   "  const file = join(out, `${name}.html`);\n  writeFileSync('/tmp/gate-dumps/leak.html', html);\n  return file;",
			strays: 1, inside: 0,
		},
		{
			what:   "the path written out by hand as a URL, the spelling no dump ever lands at",
			body:   "  const file = join(out, `${name}.html`);\n  writeFileSync(`file:///tmp/gate-dumps/leak.html`, html);\n  return file;",
			strays: 1, inside: 0,
		},
		{
			what:   "the call that exists only in a sentence about the run",
			body:   "  // writeFileSync(leak, html) would land outside the dump door\n  return '';",
			strays: 0, inside: 0,
		},
	}
	for _, one := range cases {
		t.Run(one.what, func(t *testing.T) {
			path := doorJourney(t, one.body)
			strays, inside := writesAtTheDoor(t, wholeCalls(t, path, readJourney(t, path)), "out")
			if inside != one.inside {
				t.Errorf("%s: %d call(s) wrote at a name built from the dump door, want %d\nsource: %s", one.what, inside, one.inside, one.body)
			}
			if len(strays) != one.strays {
				t.Errorf("%s: %d call(s) wrote at a name no dump door named, want %d: %s", one.what, len(strays), one.strays, strings.Join(strays, " | "))
			}
		})
	}
}

// readJourney hands wholeCalls the source of the file it is about to join, as the check beside this one does.
func readJourney(t *testing.T, path string) string {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s to join its calls: %v", path, err)
	}
	return string(source)
}
