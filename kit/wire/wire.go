// Package wire checks a composition's OpenAPI or AsyncAPI document against its
// published contract. It owns no renderer or application authorization policy.
package wire

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Break identifies one refused change. Message preserves the B1–B6 diagnostics.
type Break struct {
	Rule    string
	Path    string
	Member  string
	Message string
}

func (b Break) String() string { return b.Message }

// AuthorizationAllowance is a reviewed, exact pair of normalized declarations.
// ReviewedOn is a YYYY-MM-DD review date, not an expiry or a wall-clock check.
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
	seen := map[[2]string]bool{}
	for i, a := range allowances {
		date, err := time.Parse("2006-01-02", a.ReviewedOn)
		pair := [2]string{a.From, a.To}
		if a.From == "" || a.To == "" || a.From == a.To || err != nil || date.Format("2006-01-02") != a.ReviewedOn || strings.TrimSpace(a.Reason) == "" || seen[pair] {
			add("B6", "$allowances", fmt.Sprintf("#%d", i), fmt.Sprintf("B6 (breaking): invalid authorization allowance #%d", i))
		}
		seen[pair] = true
	}
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
	known := map[string]bool{}
	for _, op := range updated {
		known[op.operationID] = true
	}
	for _, old := range oldOps {
		fresh, ok := updated.find(old.key)
		switch {
		case !ok:
			add("B2", old.key, "route", fmt.Sprintf("B2 (breaking): %s is no longer in the document", old.key))
		case fresh.operationID != old.operationID:
			add("B2", old.key, "route", fmt.Sprintf("B2 (breaking): %s is answered by %q, which used to be %q", old.key, fresh.operationID, old.operationID))
		}
		if ok && fresh.auth != old.auth && !authorizationsWiden(old.auth, fresh.auth, allowances) {
			add("B6", old.key, "x-platformkit-auth", fmt.Sprintf("B6 (breaking): %s (%s) is authorized %s where it was %s", old.key, old.operationID, fresh.auth, old.auth))
		}
		if !known[old.operationID] {
			add("B1", old.key, "operationId", fmt.Sprintf("B1 (breaking): operationId %q is no longer in the document", old.operationID))
		}
	}
	// Associate shape records with their operation without parsing diagnostic text.
	addresses := map[string]string{}
	for _, op := range oldOps {
		addresses[op.operationID] = op.key
	}
	shapePath := func(key string) string {
		if path, ok := addresses[wireOperationOf(key)]; ok {
			return path
		}
		return asyncStructuralPath(oldDoc, key)
	}
	oldShapes, newShapes := wireShapes(oldDoc), wireShapes(newDoc)
	for _, key := range sortedKeys(oldShapes) {
		fresh, ok := newShapes[key]
		switch {
		case !ok:
			add("B3", shapePath(key), key, fmt.Sprintf("B3 (breaking): %s is gone", key))
		default:
			if problem := enumMove(key, oldShapes[key], fresh); problem != "" {
				add("B5", shapePath(key), key, problem)
			} else if fresh != oldShapes[key] {
				add("B3", shapePath(key), key, fmt.Sprintf("B3 (breaking): %s changed from %q to %q", key, oldShapes[key], fresh))
			}
		}
	}
	for _, key := range sortedKeys(newShapes) {
		if _, old := oldShapes[key]; old {
			continue
		}
		if _, announced := addresses[wireOperationOf(key)]; !announced || !strings.Contains(key, " request ") || !strings.Contains(key, " requires ") {
			continue
		}
		add("B4", shapePath(key), key, fmt.Sprintf("B4 (breaking): %s is asked for and an older client never sends it", key))
	}
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
