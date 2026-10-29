package main

import (
	"cmp"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// committedGraphRoot resolves the foundation whose committed baseline and real
// import graph this package's whole-tree cases read.
func committedGraphRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// TestCommittedContractBaselineRowsCarryOnlyEdgesTheStrictRuleRefuses asks the
// committed baseline the question beside the one it is usually asked: with no row
// in force at all, which production contract imports would the strict purity rule
// refuse? Every row must name one of exactly those edges — a row that names an
// import the rule already permits admits nothing and stays in the file as
// decoration, which is how a baseline stops recording the frozen surface. And the
// refused set must hold no edge the file omits, or a production contract
// dependency crosses the boundary under no declared reason. The comparison is
// between the file and the graph Go reads in this repository, so it asserts no
// count: a repository that moves an edge out or publishes another one answers for
// its own surface.
func TestCommittedContractBaselineRowsCarryOnlyEdgesTheStrictRuleRefuses(t *testing.T) {
	root := committedGraphRoot(t)
	t.Setenv("GOWORK", "off")
	g, err := load(t.Context(), root, nil, runGo)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := readGrants(root, g)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "scripts", "imports-contract-baseline.txt")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := parseBaseline(file, string(data), g)
	if err != nil {
		t.Fatal(err)
	}
	byEdge := func(a, b baselineKey) int {
		return cmp.Or(cmp.Compare(a.dir, b.dir), cmp.Compare(a.target, b.target))
	}
	refused := map[baselineKey]string{}
	for _, r := range decideFixture(g, grants, map[baselineKey]*baselineRow{}).refused {
		if r.rule == "contract-adapter" || r.rule == "contract-external" {
			refused[baselineKey{strings.TrimPrefix(r.from, g.self+"/"), r.to}] = r.rule
		}
	}
	t.Logf("rows in the committed file=%d contract edges the rule refuses with no row=%d", len(rows), len(refused))
	for _, key := range slices.SortedFunc(maps.Keys(rows), byEdge) {
		if _, ok := refused[key]; !ok {
			t.Errorf("row %s -> %s admits an import the strict contract rule permits, so it carries nothing; reason: %s",
				key.dir, key.target, rows[key].reason)
		}
	}
	for _, key := range slices.SortedFunc(maps.Keys(refused), byEdge) {
		if rows[key] == nil {
			t.Errorf("production contract import %s -> %s is refused by the rule (%s) with no row naming it",
				key.dir, key.target, refused[key])
		}
	}
}
