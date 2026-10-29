package module

import (
	"fmt"
	"strings"

	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tour"
)

// tourProblems reads one module's tours as a manifest: everything one declaration
// contradicts in itself, plus the one rule this package holds and kit/tour may not
// link — an event name is the kernel's grammar, and a tour that names events is
// still only a declaration here. Existence in a composition is not asked: no
// screen inventory and no route table reaches this pass.
func tourProblems(m Module) []string {
	var bad []string
	for _, t := range m.Tours {
		bad = append(bad, t.DeclarationProblems(m.Name)...)
		bad = append(bad, tourEventProblems(t)...)
	}
	return bad
}

func tourEventProblems(t tour.Tour) []string {
	if t.Automatic == nil {
		return nil
	}
	var bad []string
	if start := t.Automatic.Start(); start != "" && !events.ValidName(string(start)) {
		bad = append(bad, fmt.Sprintf("tour %q: its automatic trigger names event %q, which is not %q", t.Ref.ID, start, "<name>.<event>"))
	}
	for _, e := range t.Automatic.Retires() {
		if !events.ValidName(string(e)) {
			bad = append(bad, fmt.Sprintf("tour %q: its retirement names event %q, which is not %q", t.Ref.ID, e, "<name>.<event>"))
		}
	}
	return bad
}

// tourEmissionProblems asks whether the events a tour waits on or retires on are
// events some composed manifest says it emits — the same footing as a
// subscription, and for the same reason: a tour that waits on a name nothing
// emits is a tour nobody is ever offered, and the name outlives the module that
// renamed it.
func tourEmissionProblems(m Module, emitted map[string]bool) []string {
	var bad []string
	for _, t := range m.Tours {
		if t.Automatic == nil {
			continue
		}
		if start := t.Automatic.Start(); start != "" && !emitted[string(start)] {
			bad = append(bad, fmt.Sprintf("tour %q: its automatic trigger names %q, which no composed module emits", t.Ref.ID, start))
		}
		for _, e := range t.Automatic.Retires() {
			if !emitted[string(e)] {
				bad = append(bad, fmt.Sprintf("tour %q: its retirement names %q, which no composed module emits", t.Ref.ID, e))
			}
		}
	}
	return bad
}

// tourReferenceProblems resolves every tour's prerequisites and identity against
// the whole set, which is the only place existence can be answered: an id is
// declared by one module at one edition, a prerequisite names a tour this
// composition actually declares, and no tour can be left waiting on itself.
func tourReferenceProblems(mods []Module) []string {
	declared, edges := tourSet(mods)
	bad := duplicateTourProblems(mods)
	walked := map[tour.Ref]bool{}
	for _, m := range mods {
		for _, t := range m.Tours {
			bad = append(bad, prerequisiteProblems(t, declared)...)
			if walked[t.Ref] {
				continue
			}
			path := cycleFrom(t.Ref, edges)
			if len(path) == 0 {
				continue
			}
			// One cycle, one sentence: the tour the composition reached first says
			// what the whole walk round it costs, and the members behind it on the
			// same ring are not asked again.
			for _, ref := range path {
				walked[ref] = true
			}
			bad = append(bad, cycleSentence(t, path))
		}
	}
	return bad
}

// tourSet reads the declared editions and the prerequisite edges between them.
// A prerequisite naming its own tour is left out of the graph: the self-reference
// gets its own sentence, and a walk that reported it again would say one defect
// twice.
func tourSet(mods []Module) (map[tour.Ref]string, map[tour.Ref][]tour.Ref) {
	declared := map[tour.Ref]string{}
	edges := map[tour.Ref][]tour.Ref{}
	for _, m := range mods {
		for _, t := range m.Tours {
			declared[t.Ref] = m.Name
			for _, p := range t.Prerequisites {
				if p.ID != t.Ref.ID {
					edges[t.Ref] = append(edges[t.Ref], p)
				}
			}
		}
	}
	return declared, edges
}

func duplicateTourProblems(mods []Module) []string {
	var bad []string
	declared := map[tour.Ref]string{}
	for _, m := range mods {
		for _, t := range m.Tours {
			if owner, seen := declared[t.Ref]; seen {
				bad = append(bad, fmt.Sprintf("tour %q edition %d: declared twice, by module %q and module %q", t.Ref.ID, t.Ref.Edition, owner, m.Name))
				continue
			}
			declared[t.Ref] = m.Name
		}
	}
	return bad
}

func prerequisiteProblems(t tour.Tour, declared map[tour.Ref]string) []string {
	var bad []string
	named := map[tour.Ref]bool{}
	for _, p := range t.Prerequisites {
		switch {
		case p.Edition == 0:
			bad = append(bad, fmt.Sprintf("tour %q edition %d: prerequisite %q names edition 0; a tour's first edition is 1", t.Ref.ID, t.Ref.Edition, p.ID))
		case p.ID == t.Ref.ID:
			bad = append(bad, fmt.Sprintf("tour %q edition %d: a tour cannot require a person to finish it first", t.Ref.ID, t.Ref.Edition))
		case declared[p] == "":
			bad = append(bad, fmt.Sprintf("tour %q edition %d: prerequisite %q edition %d is not a tour of this composition", t.Ref.ID, t.Ref.Edition, p.ID, p.Edition))
		case named[p]:
			bad = append(bad, fmt.Sprintf("tour %q edition %d: prerequisite %q edition %d is named twice; one tour is finished once or it is not a prerequisite", t.Ref.ID, t.Ref.Edition, p.ID, p.Edition))
		}
		named[p] = true
	}
	return bad
}

// cycleFrom follows prerequisite edges from one tour and returns the walk up to
// and including the first edition it meets twice. Tours are walked in the order
// the composition declares them and prerequisites in declaration order, so the
// cycle a composition reads is the same one every time.
func cycleFrom(start tour.Ref, edges map[tour.Ref][]tour.Ref) []tour.Ref {
	return walkCycle([]tour.Ref{start}, map[tour.Ref]bool{start: true}, edges)
}

func walkCycle(path []tour.Ref, onPath map[tour.Ref]bool, edges map[tour.Ref][]tour.Ref) []tour.Ref {
	for _, next := range edges[path[len(path)-1]] {
		if onPath[next] {
			return append(path, next)
		}
		onPath[next] = true
		if found := walkCycle(append(path, next), onPath, edges); len(found) > 0 {
			return found
		}
		delete(onPath, next)
	}
	return nil
}

func cycleSentence(t tour.Tour, path []tour.Ref) string {
	walk := make([]string, len(path))
	for i, r := range path {
		walk[i] = fmt.Sprintf("%s@%d", r.ID, r.Edition)
	}
	return fmt.Sprintf("tour %q edition %d: prerequisite cycle %s", t.Ref.ID, t.Ref.Edition, strings.Join(quoteAll(walk), " -> "))
}

func quoteAll(names []string) []string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = fmt.Sprintf("%q", name)
	}
	return quoted
}
