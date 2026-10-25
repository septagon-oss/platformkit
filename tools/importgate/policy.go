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
	refused []refusal
	used    map[grantTarget]bool
	carried map[baselineKey]bool
}

var remedies = map[string]string{
	"undeclared-module-root":    "declare the dependency module root, then satisfy its boundary",
	"test-helper-in-production": "use an ordinary contract or move the import to actual test source",
	"foreign-internal":          "consume the public contract or compose the public root",
	"foreign-implementation":    "consume contracts and move construction to composition",
	"undeclared-integration":    "use contracts or declare this test suite's exact constructor targets with a reason",
	"contract-adapter":          "move execution, rendering or fixtures to their owning implementation or test layer",
	"contract-external":         "move the dependency to implementation or separately review a pure value dependency",
	"contract-baseline-unused":  "remove this row; no production contract source imports it, so the frozen type no longer carries it",
}

func (g *graph) owner(name string) (owner, rest string, unknown bool) {
	root := ""
	for _, candidate := range g.roots {
		if (name == candidate || strings.HasPrefix(name, candidate+"/")) && len(candidate) > len(root) {
			root = candidate
		}
	}
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
func decide(g *graph, grants map[string]*grant, baseline map[baselineKey]*baselineRow) decision {
	refused := map[refusal]bool{}
	used := map[grantTarget]bool{}
	carried := map[baselineKey]bool{}
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
	result := slices.Collect(maps.Keys(refused))
	slices.SortFunc(result, func(a, b refusal) int {
		return cmp.Or(cmp.Compare(a.from, b.from), cmp.Compare(a.to, b.to), cmp.Compare(a.context, b.context), cmp.Compare(a.rule, b.rule))
	})
	return decision{result, used, carried}
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
		"kit/module", "kit/push", "kit/request", "kit/rest", "kit/richtext", "kit/tenancy",
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
