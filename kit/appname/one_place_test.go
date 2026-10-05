package appname_test

import (
	"sort"
	"strings"
	"testing"
)

// TestNoSharedNameIsStillFormedWhereItWas is the rule the task states as its own
// invariant: "every name two apps could share carries the app's name, formed in
// one place". The census is what enforces it, so a census that tolerates a site
// still spelling a name inline is a census whose rule is not in force.
//
// An entry whose reason begins "pending" is the census recording that the name is
// formed at the old site and not through a constructor; every one of those is a
// shared name the app is not in, on a path that a caller reaches at run time. A
// "shared" entry is a name decision 0074 keeps process-wide and its reason names
// the reason; an "owner" entry is a name formed here. Neither is a debt, and this
// case does not count them.
//
// The failing list is the work the brief names as the delivery: events (subject,
// filter, durable, relay), jobs, cookies, storage and the records — each with the
// window it needs.
func TestNoSharedNameIsStillFormedWhereItWas(t *testing.T) {
	var owed []string
	for _, r := range census {
		for _, a := range r.allow {
			if strings.HasPrefix(a.why, "pending") {
				owed = append(owed, a.path+" ("+r.name+", "+itoa(a.lines)+" line"+plural(a.lines)+")")
			}
		}
	}
	if len(owed) > 0 {
		sort.Strings(owed)
		for _, o := range owed {
			t.Errorf("a shared name is still formed at %s", o)
		}
		t.Errorf("%d sites still form a shared name inline; the app is in none of the names they form", len(owed))
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
