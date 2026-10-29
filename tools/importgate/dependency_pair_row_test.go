package main

import (
	"slices"
	"strings"
	"testing"
)

// TestJustificationRowReadFromADependencyIsNotJudgedHere checks which build judges
// a reciprocal pair's row. Go reports packages for the main module alone, so a pair
// of a dependency's own modules holds no measurable direction in this build: the
// dependency's own gate judges that row, and the consumer announces it without
// refusing it. Refusing it here would deny a product repository the rows its
// dependency legitimately carries, which is the false verdict the row's comment
// promises cannot happen.
func TestJustificationRowReadFromADependencyIsNotJudgedHere(t *testing.T) {
	g := fixtureGraph(edge{product + "/modules/a/contracts", dependency + "/modules/b/contracts",
		product + "/modules/a/contracts", "production", ""})
	g.owned = []string{product + "/modules/a"}
	declared := newLayerDeclaration()
	for _, pass := range []struct{ file, rootPath, text string }{
		{"scripts/imports-layers.txt", product, product + "/modules/a platform\n"},
		{"foundation/scripts/imports-layers.txt", dependency, dependency + "/modules/b platform\n" +
			dependency + "/modules/c platform\n" +
			dependency + "/modules/b <-> " + dependency + "/modules/c Both dependency contracts name each other's values.\n"},
	} {
		if err := parseLayers(pass.file, pass.text, g, pass.rootPath, declared); err != nil {
			t.Fatalf("%s: %v", pass.file, err)
		}
	}
	got := decide(g, nil, nil, declared)
	if slices.ContainsFunc(got.refused, func(r refusal) bool { return r.rule == "module-layer-cycle-unused" }) {
		t.Fatalf("the consumer judged a dependency's own pair row: %+v", got.refused)
	}
	notice := declared.notices(got.spent, got.justified)
	if strings.Count(notice, dependency+"/modules/b <-> "+dependency+"/modules/c") != 1 {
		t.Fatalf("a dependency's pair row is announced %d times, want once: %s",
			strings.Count(notice, dependency+"/modules/b <-> "+dependency+"/modules/c"), notice)
	}
}
