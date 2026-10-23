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
// Invalid documents refuse as B3 even when their bytes are equal.
func Compare(golden, current []byte) []Break {
	return CompareWithAllowances(golden, current, nil)
}

// CompareWithAllowances applies the caller's explicit policy without retaining or
// modifying inputs. Read-only calls are safe to run concurrently.
func CompareWithAllowances(golden, current []byte, allowances []AuthorizationAllowance) []Break {
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
	compareShapes(oldDoc, newDoc, oldOps, add)
	return finish()
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

func wireShapes(doc map[string]any) map[string]string {
	if _, ok := doc["asyncapi"]; ok {
		return asyncAPIShapes(doc)
	}
	return openAPIShapes(doc)
}
