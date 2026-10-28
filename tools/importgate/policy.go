package main

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
)

const foundation = "github.com/septagon-oss/platformkit"

type refusal struct {
	edge
	rule, detail string
}

type grantTarget struct{ suite, target string }

type decision struct {
	refused   []refusal
	used      map[grantTarget]bool
	carried   map[baselineKey]bool
	spent     map[string]bool
	justified map[layerPair]bool
}

var remedies = map[string]string{
	"undeclared-module-root":        "declare the dependency module root, then satisfy its boundary",
	"test-helper-in-production":     "use an ordinary contract or move the import to actual test source",
	"foreign-internal":              "consume the public contract or compose the public root",
	"foreign-implementation":        "consume contracts and move construction to composition",
	"undeclared-integration":        "use contracts or declare this test suite's exact constructor targets with a reason",
	"contract-adapter":              "move execution, rendering or fixtures to their owning implementation or test layer",
	"contract-external":             "move the dependency to implementation or separately review a pure value dependency",
	"contract-baseline-unused":      "remove this row; no production contract source imports it, so the frozen type no longer carries it",
	"module-layer-missing":          "give this module its row in the scripts/imports-layers.txt named above; a dependency's row needs that repository and a new pin",
	"module-layer-upward":           "consume a port the lower module declares, which removes the import, or assign the module the layer its real dependencies show; no row admits an upward production import",
	"module-layer-cycle-undeclared": "move one direction behind a declared port, or add the pair's row with the reason both directions stay",
	"module-layer-cycle-unused":     "remove this row; the pair is not reciprocal in production in this build context, and one run judges one build context",
}

func (g *graph) owner(name string) (owner, rest string, unknown bool) {
	root := g.root(name)
	relative := strings.TrimPrefix(name, root+"/")
	if root != "" && strings.HasPrefix(relative, "modules/") {
		moduleName, sub, _ := strings.Cut(strings.TrimPrefix(relative, "modules/"), "/")
		return root + "/modules/" + moduleName, sub, false
	}
	return "", "", strings.Contains(relative, "/modules/")
}

func contract(rest string) bool { return rest == "contracts" || strings.HasPrefix(rest, "contracts/") }

func helper(rest string) bool {
	child, ok := strings.CutPrefix(rest, "contracts/")
	child, _, _ = strings.Cut(child, "/")
	return ok && strings.HasSuffix(child, "test")
}

// decide reports every direct import that crosses a boundary. A production
// contract dependency the purity rule refuses is admitted only while a committed
// baseline row names that exact pair, and a row that names no such import is
// itself refused, so a repository's contract baseline can only shrink.
//
// The layer pass reads the same edges a second way: every module this build
// reaches has to hold a row, a production import may not climb the platform's
// order, and a pair that imports each other has to say why. A declared port
// appears here as the absence of an edge, never as a waiver, so this function
// holds no port-aware branch and no exemption of any kind.
func decide(g *graph, grants map[string]*grant, baseline map[baselineKey]*baselineRow, layers *layerDeclaration) decision {
	refused := map[refusal]bool{}
	used := map[grantTarget]bool{}
	carried := map[baselineKey]bool{}
	spent := map[string]bool{}
	justified := map[layerPair]bool{}
	for _, e := range g.edges {
		from, sourceRest, _ := g.owner(e.ownerPath)
		to, targetRest, unknown := g.owner(e.to)
		add := func(rule string) { refused[refusal{e, rule, ""}] = true }
		if unknown {
			add("undeclared-module-root")
		}
		if helper(targetRest) && e.context == "production" {
			add("test-helper-in-production")
		}
		if to != "" && from != to {
			switch {
			case slices.Contains(strings.Split(targetRest, "/"), "internal"):
				add("foreign-internal")
			case from == "" || contract(targetRest):
			case e.context == "production" || targetRest != "":
				add("foreign-implementation")
			default:
				row := grants[e.suite]
				if row == nil {
					add("undeclared-integration")
				} else if _, ok := row.targets[e.to]; !ok {
					add("undeclared-integration")
				} else {
					used[grantTarget{e.suite, e.to}] = true
				}
			}
		}
		if e.context == "production" && contract(sourceRest) && !helper(sourceRest) {
			if rule := g.purity(e.to); rule != "" {
				key := baselineKey{strings.TrimPrefix(e.from, g.self+"/"), e.to}
				if baseline[key] == nil {
					add(rule)
				} else {
					carried[key] = true
				}
			}
		}
	}
	for key, row := range baseline {
		if carried[key] {
			continue
		}
		// The row outlived its import: the frozen type moved out, or the row names
		// a dependency the rule never refused. Either way it is no longer a fact.
		pkg := g.self + "/" + key.dir
		refused[refusal{edge{pkg, key.target, pkg, "baseline", ""}, "contract-baseline-unused", row.reason}] = true
	}
	g.layers(refused, spent, justified, layers)
	result := slices.Collect(maps.Keys(refused))
	slices.SortFunc(result, func(a, b refusal) int {
		return cmp.Or(cmp.Compare(a.from, b.from), cmp.Compare(a.to, b.to), cmp.Compare(a.context, b.context), cmp.Compare(a.rule, b.rule))
	})
	return decision{result, used, carried, spent, justified}
}

// layers judges the module layer rules over the same edges the direct rules
// judged. Only production source is measured: a test composition already names
// its exact foreign constructors and its reason in scripts/imports-integration.txt,
// and a rule over test source would need an exemption for a granted row. Every
// justification row of the declaration is judged, whether or not the graph still
// holds the pair it names, so a map's exceptions can only shrink.
func (g *graph) layers(refused map[refusal]bool, spent map[string]bool, justified map[layerPair]bool, declared *layerDeclaration) {
	required := map[string]bool{}
	for _, owner := range g.owned {
		required[owner] = true
	}
	// One pair aggregates the production edges running both ways between its two
	// modules; the set of module-level directions it carries says whether the pair
	// imports itself back. Package identity is not the question: two packages of
	// one module importing one package of another are still one direction.
	pairEdges := map[layerPair][]edge{}
	pairSides := map[layerPair]map[string]bool{}
	for _, e := range g.edges {
		if e.context != "production" {
			continue
		}
		from, _, _ := g.owner(e.ownerPath)
		to, _, _ := g.owner(e.to)
		if from != "" {
			required[from] = true
		}
		if to == "" || to == from {
			if to != "" {
				required[to] = true
			}
			continue
		}
		required[to] = true
		pair := newLayerPair(from, to)
		pairEdges[pair] = append(pairEdges[pair], e)
		if pairSides[pair] == nil {
			pairSides[pair] = map[string]bool{}
		}
		pairSides[pair][from] = true
		if direction(declared.layer(from), declared.layer(to)) == directionUpward {
			refused[refusal{e, "module-layer-upward", from + " " + declared.layer(from) + " -> " + to + " " + declared.layer(to)}] = true
		}
	}
	for owner := range required {
		spent[owner] = true
		if declared.assigned[owner] != nil {
			continue
		}
		// A module of a dependency is visible only as an import target: Go reports no
		// packages for it. Its row belongs to that repository, so the refusal names
		// that repository's file and a consumer can be corrected only by a new pin.
		refused[refusal{edge{owner, g.root(owner) + layerFileSuffix, owner, "layer", ""}, "module-layer-missing", ""}] = true
	}
	for pair, edges := range pairEdges {
		if len(pairSides[pair]) < 2 {
			// One direction only is a dependency the order admits, or a row the sweep
			// below judges.
			continue
		}
		justified[pair] = true
		if declared.justifications[pair] != nil {
			continue
		}
		// Two modules that import each other are one unit of composition whatever
		// their layer, so the pair has to say why neither direction moves.
		for _, e := range edges {
			refused[refusal{e, "module-layer-cycle-undeclared", ""}] = true
		}
	}
	// The exception is judged from the declaration rather than from the measured
	// pairs, as an unused contract baseline row is: a row whose pair holds no
	// production edge in either direction never enters the edge set above, and a rule
	// that could not see it would leave the one exception a map may carry free to grow
	// instead of shrink. Only a pair of the checked repository's own modules is judged
	// here, because Go reports packages for the main module alone: a row read from a
	// dependency's file is reciprocal or stale in that repository's own build, which is
	// where its gate judges it.
	for pair, row := range declared.justifications {
		if justified[pair] || g.root(pair.a) != g.self || g.root(pair.b) != g.self {
			continue
		}
		// The row outlived its reciprocation: one direction moved behind a port, or
		// the two modules never imported each other in production at all.
		refused[refusal{edge{pair.a, pair.b, pair.a, "layer", ""}, "module-layer-cycle-unused", row.reason}] = true
	}
}

func (g *graph) purity(target string) string {
	if target == "database/sql/driver" || target == "net/http" {
		return ""
	}
	for _, denied := range []string{"database/sql", "html", "html/template", "net/http/httptest", "testing", "C"} {
		if target == denied || strings.HasPrefix(target, denied+"/") {
			return "contract-adapter"
		}
	}
	if g.standard[target] {
		return ""
	}
	for _, allowed := range []string{
		"kit/crud", "kit/db", "kit/entity", "kit/events", "kit/events/transport", "kit/fault", "kit/limit",
		"kit/module", "kit/push", "kit/request", "kit/rest", "kit/richtext", "kit/screen", "kit/tenancy",
	} {
		if target == foundation+"/"+allowed {
			return ""
		}
	}
	owner, rest, _ := g.owner(target)
	if owner != "" && contract(rest) && !helper(rest) {
		return ""
	}
	if owner != "" || strings.HasPrefix(target, foundation+"/") {
		return "contract-adapter"
	}
	if slices.Contains([]string{"github.com/google/uuid", "golang.org/x/text/currency", "golang.org/x/text/unicode/norm", "golang.org/x/image/webp", "golang.org/x/crypto/argon2"}, target) {
		return ""
	}
	return "contract-external"
}

func diagnostics(refused []refusal) string {
	var text strings.Builder
	for _, r := range refused {
		prefix, detail := "", ""
		if r.rule == "foreign-internal" && strings.Contains(r.ownerPath, "/apps/") {
			prefix = "apps/ reaches into: "
		}
		if r.detail != "" {
			detail = "; row reason: " + r.detail
		}
		fmt.Fprintf(&text, "OUT OF BOUNDS [%s] %s%s -> %s (%s): %s%s\n", r.rule, prefix, r.from, r.to, r.context, remedies[r.rule], detail)
	}
	return text.String()
}
