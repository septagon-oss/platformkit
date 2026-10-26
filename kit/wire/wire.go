// Package wire checks a composition's OpenAPI or AsyncAPI document against its
// published contract. It owns no renderer or application authorization policy.
package wire

import (
	"cmp"
	"fmt"
	"slices"
)

// Break identifies one refused change. Message preserves the B1–B6 diagnostics.
type Break struct {
	Rule    string
	Path    string
	Member  string
	Message string
}

func (b Break) String() string { return b.Message }

// AuthorizationAllowance is a reviewed, exact pair of normalized declarations. A
// declaration is the whole declaration the server emitted — kind, permission, feature
// and any other member — so a pair names every member on both sides and covers no
// change it does not spell out. ReviewedOn is a YYYY-MM-DD review date, not an expiry
// or a wall-clock check.
type AuthorizationAllowance struct {
	From       string
	To         string
	ReviewedOn string
	Reason     string
}

// Compare reports all breaks, sorted by message, with no authorization allowances.
// Invalid documents refuse as B3 even when their bytes are equal, and a comparison that
// could not read both documents to their end refuses as B7 for the same reason: an empty
// result states that the pair was read in full and nothing moved.
func Compare(golden, current []byte) []Break {
	return CompareWithAllowances(golden, current, nil)
}

// CompareWithAllowances applies the caller's explicit policy without retaining or
// modifying inputs. Read-only calls are safe to run concurrently.
func CompareWithAllowances(golden, current []byte, allowances []AuthorizationAllowance) []Break {
	return compareWire(golden, current, allowances, nil)
}

func compareWire(golden, current []byte, allowances []AuthorizationAllowance, required []RequirednessAllowance) []Break {
	var problems []Break
	add := func(rule, path, member, message string) {
		problems = append(problems, Break{rule, path, member, message})
	}
	finish := func() []Break {
		slices.SortFunc(problems, func(a, b Break) int {
			return cmp.Or(cmp.Compare(a.Message, b.Message), cmp.Compare(a.Path, b.Path), cmp.Compare(a.Member, b.Member), cmp.Compare(a.Rule, b.Rule))
		})
		return problems
	}
	checkAllowances(allowances, add)
	if len(problems) > 0 {
		return finish()
	}
	oldDoc, oldErr := wireDocument(golden)
	newDoc, newErr := wireDocument(current)
	for _, side := range []struct {
		name string
		err  error
	}{{"golden", oldErr}, {"current", newErr}} {
		if side.err != nil {
			add("B3", "$", side.name, fmt.Sprintf("B3 (breaking): %s document is invalid: %v", side.name, side.err))
		}
	}
	if len(problems) > 0 {
		return finish()
	}
	if oldDoc["openapi"] != newDoc["openapi"] || oldDoc["asyncapi"] != newDoc["asyncapi"] {
		add("B3", "$", "format", "B3 (breaking): document format changed")
		return finish()
	}
	oldOps, updated := wireOperations(oldDoc), wireOperations(newDoc)
	compareOperations(oldOps, updated, allowances, add)
	oldRead, newRead := wireShapes(oldDoc), wireShapes(newDoc)
	shapes := newShapeIndex(oldDoc, oldOps)
	// A requiredness list that no reviewer signed record by record, or that could carry
	// more than the one move it names, is refused and forgives nothing: the rules below
	// still read the whole pair, so a bad review never hides a break beside it.
	if refused := reviewRequiredness(required, oldRead.shapes, newRead.shapes); len(refused) > 0 {
		problems = append(problems, refused...)
		required = nil
	}
	compareShapes(shapes, oldRead.shapes, newRead.shapes, required, add)
	problems = append(problems, incompleteReads(oldRead, newRead, shapes.path)...)
	return finish()
}

// incompleteReads states, beside every comparison verdict, the addresses an analysis gave up
// on. A walk that ran out of a bound read neither side to the end, so it compared nothing
// about the shapes below where it stopped, and the empty result that would otherwise follow
// is the answer every caller reads as certified — a golden gate that saw one would rewrite
// the baseline from a document nobody had finished reading. That is why this verdict arrives
// in the same list rather than beside it. No allowance reaches it: a reviewed pair of
// declarations cannot cover an address that has no record on either side.
func incompleteReads(oldRead, newRead wireAnalysis, shapePath func(string) string) []Break {
	var problems []Break
	for _, side := range []struct {
		name string
		read wireAnalysis
	}{{"golden", oldRead}, {"current", newRead}} {
		for _, stop := range side.read.stops {
			problems = append(problems, Break{"B7", shapePath(stop.key), stop.key, stop.refusal(side.name)})
		}
	}
	return problems
}

func authorizationsWiden(from, to string, allowances []AuthorizationAllowance) bool {
	return slices.ContainsFunc(allowances, func(a AuthorizationAllowance) bool { return a.From == from && a.To == to })
}

func wireOperations(doc map[string]any) wireRoutes {
	if _, ok := doc["asyncapi"]; ok {
		return asyncAPIOperations(doc)
	}
	return openAPIOperations(doc)
}

func wireShapes(doc map[string]any) wireAnalysis {
	if _, ok := doc["asyncapi"]; ok {
		return asyncAPIShapes(doc)
	}
	return openAPIShapes(doc)
}
