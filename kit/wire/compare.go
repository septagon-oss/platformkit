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

// compareOperations states what moved between the two documents' operations: an address
// that went away or changed hands (B2), an operationId no route answers (B1), and a door
// that authorizes someone it did not (B6) beyond what a reviewed pair covers.
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

// shapeIndex answers two questions of a shape record key, both by the operation the record
// names rather than by parsing diagnostic text: which address a verdict should carry, and
// whether the golden document announces that operation at all.
type shapeIndex struct {
	doc       map[string]any
	addresses map[string]string
}

func newShapeIndex(oldDoc map[string]any, oldOps wireRoutes) shapeIndex {
	addresses := map[string]string{}
	for _, op := range oldOps {
		addresses[op.operationID] = op.key
	}
	return shapeIndex{doc: oldDoc, addresses: addresses}
}

// path names the route of the operation a shape record is read from, or the async structural
// address of the key when no announced operation matches it.
func (s shapeIndex) path(key string) string {
	if path, ok := s.addresses[wireOperationOf(key)]; ok {
		return path
	}
	return asyncStructuralPath(s.doc, key)
}

// announces reports whether the golden document carries an operation the shape record names.
func (s shapeIndex) announces(key string) bool {
	_, ok := s.addresses[wireOperationOf(key)]
	return ok
}

// compareShapes states every difference between two documents' shape records: a record that
// vanished or changed is B3, an enum that moved is B5, and a member a request newly requires
// that an older client never sends is B4. A record neither walk read is no difference; the
// caller reports the read that stopped instead. `required` is the caller's reviewed
// requiredness list, honoured for the one vanished requirement it names verbatim; a list the
// caller did not hold at the gate arrives here empty.
func compareShapes(shapes shapeIndex, oldShapes, newShapes map[string]string, required []RequirednessAllowance, add func(rule, path, member, message string)) {
	for _, key := range sortedKeys(oldShapes) {
		fresh, ok := newShapes[key]
		switch {
		case !ok:
			if requirednessRelaxation(required, key) {
				continue
			}
			add("B3", shapes.path(key), key, fmt.Sprintf("B3 (breaking): %s is gone", key))
		default:
			if problem := enumMove(key, oldShapes[key], fresh); problem != "" {
				add("B5", shapes.path(key), key, problem)
			} else if fresh != oldShapes[key] {
				add("B3", shapes.path(key), key, fmt.Sprintf("B3 (breaking): %s changed from %q to %q", key, oldShapes[key], fresh))
			}
		}
	}
	for _, key := range sortedKeys(newShapes) {
		if _, old := oldShapes[key]; old {
			continue
		}
		if !shapes.announces(key) || !strings.Contains(key, " request ") || !strings.Contains(key, " requires ") {
			continue
		}
		add("B4", shapes.path(key), key, fmt.Sprintf("B4 (breaking): %s is asked for and an older client never sends it", key))
	}
}
