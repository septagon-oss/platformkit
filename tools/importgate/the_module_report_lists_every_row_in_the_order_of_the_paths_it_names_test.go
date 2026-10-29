package main

import (
	"slices"
	"testing"
)

// TestTheModuleReportListsEveryRowInTheOrderOfThePathsItNames reads the order of the
// aggregate report `--modules` answers, the machine-readable form of the module graph.
// scripts/module_graph.sh renders its nodes and its edge rows into docs/MODULE-GRAPH.md
// and `make check-generated` asks for those bytes twice, so rows that move between two
// runs of one tree are a generated document nobody can keep; the package rows beneath one
// edge reach no row of that document but belong to any reader of the JSON. The report is
// collected out of maps, so each list is in this order only because moduleGraph sorts it:
// nodes by module, edges and constructions by the pair they run between, the packages of
// one pair by the package they run from, and the contexts of one package by name. Inputs:
// one graph whose eight edges name three modules and arrive in another order. Failure: a
// list out of that order, which answers one tree two ways.
func TestTheModuleReportListsEveryRowInTheOrderOfThePathsItNames(t *testing.T) {
	z, m, a := product+"/modules/z", product+"/modules/m", product+"/modules/a"
	g := fixtureGraph(
		edge{z + "/impl", a + "/contracts", z + "/impl", "production", ""},
		edge{z + "/internal/x", a + "/contracts", z + "/internal/x", "internal-test", ""},
		edge{z, a + "/contracts", z, "production", ""},
		edge{m + "/impl", z + "/contracts", m + "/impl", "production", ""},
		edge{z + "/internal/x", a + "/contracts", z + "/internal/x", "production", ""},
		edge{a + "/contracts", m + "/contracts", a + "/contracts", "production", ""},
		edge{m + "/impl", a + "/contracts", m + "/impl", "production", ""},
		edge{z + "/internal/y", a + "/contracts", z + "/internal/y", "production", ""},
	)
	report := moduleGraph(g, fixtureLayers(g), nil)
	asked := func(what string, got []string, want ...string) {
		if !slices.IsSorted(got) || !slices.Equal(got, want) {
			t.Errorf("%s read %q, want %q listed in the order of the paths it names", what, got, want)
		}
	}
	pair := func(from, to string) string { return from + " -> " + to }
	nodes := []string{}
	for _, node := range report.Nodes {
		nodes = append(nodes, node.Module)
	}
	asked("the report's nodes", nodes, a, m, z)
	rows := func(what string, edges []moduleEdge) []string {
		out := []string{}
		for _, edge := range edges {
			out = append(out, pair(edge.From, edge.To))
			packages := []string{}
			for _, loaded := range edge.Packages {
				packages = append(packages, pair(loaded.From, loaded.To))
				if !slices.IsSorted(loaded.Contexts) || loaded.Contexts[len(loaded.Contexts)-1] != "production" {
					t.Errorf("%s %s -> %s package %s lists contexts %v, want them named in order with production last",
						what, edge.From, edge.To, loaded.From, loaded.Contexts)
				}
			}
			if what == "edge" && edge.From == z && edge.To == a {
				asked(what+" "+edge.From+" -> "+edge.To+" packages", packages,
					pair(z+"/impl", a+"/contracts"), pair(z+"/internal/x", a+"/contracts"), pair(z+"/internal/y", a+"/contracts"))
			}
		}
		return out
	}
	measured := rows("edge", report.Edges)
	asked("edges", measured, pair(a, m), pair(m, a), pair(m, z), pair(z, a))
	asked("constructions", rows("construction", report.Constructions), pair(z, a))
}
