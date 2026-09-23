package db_test

// review11_a_refusal_id_is_declared_wherever_the_runner_keeps_it_test.go is the tenth round's
// acceptance-review pin for the refusal-id gate.
//
// kit/db/README.md promises, of the four runtime ids: "Between them the two halves of that
// promise hold: an id named here that nothing prints, or a printed id nothing names, fails one
// case or the other rather than drifting." The second half holds only while every id is
// declared in the one file the two regexps read. Measured in a copy of HEAD, a fifth id —
//
//	// kit/db/backfill.go
//	const refusalDataBodySplit = "data-body-multiple-statements"
//	…
//	return drainReport{}, refusal(refusalDataBodySplit, "a data file is one statement: …", "split the file")
//
// declared in the file that prints it, named by no document, and printed by a run every case
// in this package already reaches — leaves `go test ./kit/db/... ./migrations/... -count=1`
// green, and all three of the id cases green. `declaredRefusalID` and `refusalIDConst` read
// `refusals.go` by name, and `inlinedRefusalID` looks for the id written into a sentence as a
// literal, which a `%s` is not. The tenth round's commit records that limit under *Not
// verified*; the sentence above does not.
//
// So this case reads the declarations off the package rather than off one file: an id is what
// `refusal<Name> = "…"` makes it, wherever the runner keeps it, and every one of those has to be
// one a case walks and one the operator's table names. It passes on a tree where the ids all
// live in `refusals.go` and fails on the copy above, which is the drift the README sentence says
// it refuses.

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// anyRefusalIDConst is one refusal id and the constant the code calls it by, in any file of
// this package: the same line `refusals.go` is read for, read of the whole directory.
var anyRefusalIDConst = regexp.MustCompile(`(?m)^\s*(?:const\s+)?(refusal[A-Za-z]*)\s*=\s*"([a-z0-9-]+)"\s*$`)

func TestARefusalIDDeclaredAnywhereInTheRunnerIsOneItsTableListsAndACaseWalks(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("this package's own directory: %v", err)
	}
	home := map[string]string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		text, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, found := range anyRefusalIDConst.FindAllStringSubmatch(string(text), -1) {
			if seen, again := home[found[2]]; again && seen != name {
				t.Errorf("refusal id %q is declared in both %s and %s: an id has one home, and two files a gate reads by name is two ids", found[2], seen, name)
			}
			home[found[2]] = name
		}
	}
	if len(home) == 0 {
		t.Fatal("no refusal id is declared anywhere in this package, which is not what refusals.go says")
	}
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("this package's own README.md: %v", err)
	}
	walked := map[string]bool{}
	for _, door := range refusalDoors {
		walked[door.id] = true
	}
	for id, file := range home {
		if file != "refusals.go" {
			t.Errorf("refusal id %q is declared in %s and not in refusals.go, the one file the id's own comment says owns the ids: the tables that count them read that file by name, so an id kept anywhere else is printed by a run and named by no list a gate reads", id, file)
		}
		if !walked[id] {
			t.Errorf("refusal id %q is declared and printed by nothing this package walks: a printed id nothing names is a name that drifts from the sentence it stands for", id)
		}
		if !strings.Contains(string(readme), "`"+id+"`") {
			t.Errorf("README.md names no refusal id %q, which a run prints: the table an operator reads and the sentence that refused them have to be one list", id)
		}
	}
}
