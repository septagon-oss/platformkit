package main

import (
	"cmp"
	"maps"
	"slices"
	"strings"
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

// moduleReport is the aggregate module graph: which layer each module of this
// build sits in, and which module reaches which. Composition edges — those whose
// source is a module's own root package — are printed apart from the rest,
// because Go package imports cannot cycle while module composition can, and
// printing the two in one list invites a reader to see a cycle where no import
// cycle exists. Direction, sources and justification are computed from the graph
// and the declaration; nothing in this document is declared by the repository it
// measures.
type moduleReport struct {
	Schema        string       `json:"schema"`
	Layers        []string     `json:"layers"`
	Nodes         []moduleNode `json:"nodes"`
	Edges         []moduleEdge `json:"edges"`
	Constructions []moduleEdge `json:"constructions"`
}

type moduleNode struct {
	Module     string `json:"module"`
	Repository string `json:"repository"`
	Layer      string `json:"layer"`
}

// moduleEdge aggregates every package-level import running from one module into
// another, in that direction. A pair that imports each other appears twice, once
// each way, so an upward import is never hidden inside a single undirected row.
type moduleEdge struct {
	From      string         `json:"from"`
	To        string         `json:"to"`
	FromLayer string         `json:"fromLayer"`
	ToLayer   string         `json:"toLayer"`
	Direction string         `json:"direction"`
	Sources   []string       `json:"sources"`
	Justified bool           `json:"justified"`
	Reason    string         `json:"reason,omitempty"`
	Packages  []reportedEdge `json:"packages"`
}

// sourceKind names the position of the consuming package: a module's own root
// package composes, a contracts tree publishes, an internal tree implements, and
// anything else is ordinary implementation.
func sourceKind(rest string) string {
	switch {
	case rest == "":
		return "composition"
	case contract(rest):
		return "contracts"
	case slices.Contains(strings.Split(rest, "/"), "internal"):
		return "internal"
	default:
		return "implementation"
	}
}

// moduleGraph aggregates the measured edges by module owner. Own-owner,
// application and non-module edges contribute nothing, exactly as in the
// package-level report, and a module the build holds with no row is listed as
// unassigned rather than left out.
func moduleGraph(g *graph, declared *layerDeclaration, justified map[layerPair]bool) moduleReport {
	type aggregate struct {
		from, to  string
		composing bool
	}
	type link struct{ from, to string }
	grouped := map[aggregate]map[link]map[string]bool{}
	nodes := map[string]bool{}
	for _, owner := range g.owned {
		nodes[owner] = true
	}
	for _, e := range g.edges {
		from, sourceRest, _ := g.owner(e.ownerPath)
		to, _, _ := g.owner(e.to)
		if from == "" || to == "" || from == to {
			continue
		}
		nodes[from], nodes[to] = true, true
		key := aggregate{from, to, sourceKind(sourceRest) == "composition"}
		if grouped[key] == nil {
			grouped[key] = map[link]map[string]bool{}
		}
		if grouped[key][link{e.from, e.to}] == nil {
			grouped[key][link{e.from, e.to}] = map[string]bool{}
		}
		grouped[key][link{e.from, e.to}][e.context] = true
	}
	result := moduleReport{
		Schema:        "platformkit.module-graph.v1",
		Layers:        slices.Clone(moduleLayers),
		Nodes:         []moduleNode{},
		Edges:         []moduleEdge{},
		Constructions: []moduleEdge{},
	}
	for _, owner := range slices.Sorted(maps.Keys(nodes)) {
		result.Nodes = append(result.Nodes, moduleNode{owner, g.root(owner), declared.layer(owner)})
	}
	byOwner := func(a, b aggregate) int {
		if order := cmp.Or(cmp.Compare(a.from, b.from), cmp.Compare(a.to, b.to)); order != 0 {
			return order
		}
		if a.composing == b.composing {
			return 0
		}
		if a.composing {
			return 1
		}
		return -1
	}
	for _, key := range slices.SortedFunc(maps.Keys(grouped), byOwner) {
		edge := moduleEdge{
			From: key.from, To: key.to,
			FromLayer: declared.layer(key.from), ToLayer: declared.layer(key.to),
		}
		edge.Direction = direction(edge.FromLayer, edge.ToLayer)
		if row := declared.justifications[newLayerPair(key.from, key.to)]; row != nil && justified[newLayerPair(key.from, key.to)] {
			edge.Justified, edge.Reason = true, row.reason
		}
		kinds := map[string]bool{}
		for _, link := range slices.SortedFunc(maps.Keys(grouped[key]), func(a, b link) int {
			return cmp.Or(cmp.Compare(a.from, b.from), cmp.Compare(a.to, b.to))
		}) {
			_, sourceRest, _ := g.owner(link.from)
			kinds[sourceKind(sourceRest)] = true
			edge.Packages = append(edge.Packages, reportedEdge{link.from, link.to, slices.Sorted(maps.Keys(grouped[key][link]))})
		}
		edge.Sources = slices.Sorted(maps.Keys(kinds))
		if key.composing {
			result.Constructions = append(result.Constructions, edge)
		} else {
			result.Edges = append(result.Edges, edge)
		}
	}
	return result
}
