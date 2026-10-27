package main

import (
	"cmp"
	"maps"
	"slices"
)

type report struct {
	Schema string `json:"schema"`
	Pairs  []pair `json:"pairs"`
}

type pair struct {
	From  string         `json:"from"`
	To    string         `json:"to"`
	Edges []reportedEdge `json:"edges"`
}

type reportedEdge struct {
	From     string   `json:"from"`
	To       string   `json:"to"`
	Contexts []string `json:"contexts"`
}

func edgeReport(g *graph) report {
	type link struct{ from, to string }
	pairs := map[link]map[link]map[string]bool{}
	for _, e := range g.edges {
		from, _, _ := g.owner(e.ownerPath)
		to, _, _ := g.owner(e.to)
		if from == "" || to == "" || from == to {
			continue
		}
		owners, packages := link{from, to}, link{e.from, e.to}
		if pairs[owners] == nil {
			pairs[owners] = map[link]map[string]bool{}
		}
		if pairs[owners][packages] == nil {
			pairs[owners][packages] = map[string]bool{}
		}
		pairs[owners][packages][e.context] = true
	}
	result := report{Schema: "platformkit.import-edges.v1", Pairs: []pair{}}
	for owners, edges := range pairs {
		p := pair{From: owners.from, To: owners.to}
		for packages, contexts := range edges {
			p.Edges = append(p.Edges, reportedEdge{packages.from, packages.to, slices.Sorted(maps.Keys(contexts))})
		}
		slices.SortFunc(p.Edges, func(a, b reportedEdge) int {
			return cmp.Or(cmp.Compare(a.From, b.From), cmp.Compare(a.To, b.To))
		})
		result.Pairs = append(result.Pairs, p)
	}
	slices.SortFunc(result.Pairs, func(a, b pair) int {
		return cmp.Or(cmp.Compare(a.From, b.From), cmp.Compare(a.To, b.To))
	})
	return result
}
