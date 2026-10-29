package tour

import (
	"fmt"
	"strings"

	"github.com/septagon-oss/platformkit/kit/screen"
)

// writeMethods are the methods the kernel admits an operation outcome for, since
// an outcome is the record of a write that settled. A step that waits on a read
// could never be left by what it waits for.
var writeMethods = map[string]bool{"POST": true, "PUT": true, "PATCH": true, "DELETE": true}

func (a Automatic) compositionProblems(id ID, c Composition) []string {
	if a.kind != AutomaticOnVisit {
		return nil
	}
	if _, err := c.View(a.view); err != nil {
		return []string{fmt.Sprintf("tour %q: its automatic trigger's view %s is not a view of this composition", id, a.view)}
	}
	return nil
}

func (s Step) compositionProblems(id ID, c Composition) []string {
	if s.kind == StepModal {
		if _, err := c.View(s.view); err != nil {
			return []string{fmt.Sprintf("tour %q, step %q: its view %s is not a view of this composition", id, s.id, s.view)}
		}
		return nil
	}
	if s.kind == "" {
		return nil
	}
	if repeated(s.anchor, c) {
		return []string{fmt.Sprintf("tour %q, step %q: %s names no anchor; %s renders repeated rows and a repeated row carries no anchor until the platform has a typed instance binding",
			id, s.id, s.anchor, anchorView(s.anchor))}
	}
	anchor, err := c.Anchor(s.anchor)
	if err != nil {
		return []string{fmt.Sprintf("tour %q, step %q: %s is not an anchor of this composition", id, s.id, s.anchor)}
	}
	return s.advanceCompositionProblems(id, anchor, c)
}

// repeated answers whether the anchor names a control on a view that renders
// more than one instance per page. Such a view publishes no anchors, so the
// refusal names the missing thing — a typed instance binding — rather than
// reading as a typo in a name that could be corrected.
func repeated(ref screen.AnchorRef, c Composition) bool {
	view, err := c.View(anchorView(ref))
	if err != nil {
		return false
	}
	return view.Cardinality() == screen.Many
}

func anchorView(ref screen.AnchorRef) screen.ViewRef {
	view, _, _ := strings.Cut(string(ref), "#")
	return screen.ViewRef(view)
}

func (s Step) advanceCompositionProblems(id ID, anchor screen.Anchor, c Composition) []string {
	switch s.advance.kind {
	case AdvanceOn:
		if anchor.Supports(s.advance.interaction) {
			return nil
		}
		return []string{fmt.Sprintf("tour %q, step %q: anchor %s does not report %q", id, s.id, s.anchor, s.advance.interaction)}
	case AdvanceCompletes:
		return s.completionProblems(id, anchor, c)
	}
	return nil
}

// completionProblems holds a step that waits for a completion to the write it can
// actually observe: an operation this composition mounted, answering with a method
// that can settle, and the one the control this step points at makes. An anchor's
// binding names that operation, so a step cannot wait on a write its own control
// never posts. Which person an outcome belongs to is not a fact a screen contract
// carries, and no rule here claims it.
func (s Step) completionProblems(id ID, anchor screen.Anchor, c Composition) []string {
	op, ok := c.Operation(string(s.advance.operation))
	if !ok {
		return []string{fmt.Sprintf("tour %q, step %q: operation %q is not an operation of this composition", id, s.id, s.advance.operation)}
	}
	if !writeMethods[op.Method] {
		return []string{fmt.Sprintf("tour %q, step %q: operation %q answers %s and can never be observed succeeding; an outcome is admitted only for a write",
			id, s.id, s.advance.operation, op.Method)}
	}
	binding := anchor.Binding()
	if startsNoWrite(binding.Kind) {
		return []string{fmt.Sprintf("tour %q, step %q: %s is a %s and starts no write; a step that waits for a completion has to point at the control that makes it",
			id, s.id, s.anchor, controlWord(binding.Kind))}
	}
	if binding.Target != string(s.advance.operation) {
		return []string{fmt.Sprintf("tour %q, step %q: %s starts %q; a step waits for the outcome of the write its own control makes, not for %q",
			id, s.id, s.anchor, binding.Target, s.advance.operation)}
	}
	return nil
}

func startsNoWrite(binding string) bool { return binding == "navigation" || binding == "display" }

func controlWord(binding string) string {
	if binding == "navigation" {
		return "link"
	}
	return "read-only display"
}
