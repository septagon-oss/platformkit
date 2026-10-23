package wire

import (
	"fmt"
	"strings"
	"time"
)

func checkAllowances(allowances []AuthorizationAllowance, add func(rule, path, member, message string)) {
	seen := map[[2]string]bool{}
	for i, a := range allowances {
		date, err := time.Parse("2006-01-02", a.ReviewedOn)
		pair := [2]string{a.From, a.To}
		if a.From == "" || a.To == "" || a.From == a.To || err != nil || date.Format("2006-01-02") != a.ReviewedOn || strings.TrimSpace(a.Reason) == "" || seen[pair] {
			add("B6", "$allowances", fmt.Sprintf("#%d", i), fmt.Sprintf("B6 (breaking): invalid authorization allowance #%d", i))
		}
		seen[pair] = true
	}
}

func compareOperations(oldOps, updated wireRoutes, allowances []AuthorizationAllowance, add func(rule, path, member, message string)) {
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
}

func compareShapes(oldDoc, newDoc map[string]any, oldOps wireRoutes, add func(rule, path, member, message string)) {
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
}
