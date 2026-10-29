package tour

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/septagon-oss/platformkit/kit/screen"
)

// The three grammars a declaration is held to. A tour id is a namespaced name like
// an event's and a step id an identifier like a module's, both the kernel's own
// spellings. A copy key is decided narrower than the keys the copy tables in this
// tree hold, some of which carry a digit, a hyphen or a colon: the lower-case
// dotted form below is what a tour author is held to, so a tour never inherits a
// catalogue's irregular spelling by pointing at one of its keys.
var (
	tourID   = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)
	stepName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	copyKey  = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-zA-Z0-9_]*)*$`)
)

// maxKeyBytes bounds a copy key: a key longer than this is a sentence written
// where a name belongs, and every catalogue this kernel reads keys out of.
const maxKeyBytes = 128

// publicSurfaceRefusal is why nothing played on the public surface lasts: a run is
// kept per signed-in person and that surface admits none, so no step or trigger
// there is ever reached. Which person-facing surfaces a product guides a person
// over stays the product's, and this asks nothing of the choice between them.
const publicSurfaceRefusal = "sits on the public surface, which admits no signed-in person, so no run can be kept"

// DeclarationProblems says what is wrong with this tour as a declaration: its own
// grammar, its own references and its own copy, against the name of the module
// that declares it. It needs no route, no inventory and no service, so the
// manifest pass can answer it while it reads every manifest, before anything
// opens. It returns one sentence per problem, in declaration order, and never
// panics; an empty result says nothing is wrong as far as this pass can see.
func (t Tour) DeclarationProblems(module string) []string {
	bad := t.identityProblems(module)
	bad = append(bad, t.copyProblems(module)...)
	bad = append(bad, t.stepProblems()...)
	return append(bad, t.triggerProblems()...)
}

// CompositionProblems says what this tour asks of a composition that is not
// there: a view nobody published, an anchor no view carries, an interaction the
// anchor does not report, an operation the composition never mounted, a step
// waiting for a completion its own control cannot start. It needs the screen
// contract, which a manifest reader does not have, so the application's
// composition gate asks it after routes are registered. One sentence per problem,
// in declaration order, and the same non-panic.
func (t Tour) CompositionProblems(c Composition) []string {
	var bad []string
	if t.Automatic != nil {
		bad = t.Automatic.compositionProblems(t.Ref.ID, c)
	}
	for _, s := range t.Steps {
		bad = append(bad, s.compositionProblems(t.Ref.ID, c)...)
	}
	return bad
}

func (t Tour) identityProblems(module string) []string {
	var bad []string
	id := string(t.Ref.ID)
	if !tourID.MatchString(id) {
		bad = append(bad, fmt.Sprintf("tour %q: an id is %q", t.Ref.ID, "<module>.<name>"))
	} else if owner, _, _ := strings.Cut(id, "."); owner != module {
		bad = append(bad, fmt.Sprintf("tour %q: its id names %q, which does not declare it; a tour belongs to the module whose name opens its id", t.Ref.ID, owner))
	}
	if t.Ref.Edition == 0 {
		bad = append(bad, fmt.Sprintf("tour %q: edition 0; a tour's first edition is 1, and an edition is bumped only for instruction a person may be given twice", t.Ref.ID))
	}
	return bad
}

// copyPlace is one piece of copy a tour declares, named the way a refusal names
// it: "its title" for the listing's own, and a step's name for a step's.
type copyPlace struct {
	where string
	msg   Message
}

func (t Tour) copies() []copyPlace {
	places := []copyPlace{{where: "its title", msg: t.Title}}
	for _, s := range t.Steps {
		if s.kind == "" {
			continue
		}
		who := fmt.Sprintf("step %q", s.id)
		places = append(places,
			copyPlace{where: who + ", its title", msg: s.content.Title},
			copyPlace{where: who + ", its body", msg: s.content.Body})
	}
	return places
}

func (t Tour) copyProblems(module string) []string {
	var bad []string
	answers := map[string]string{}
	for _, place := range t.copies() {
		bad = append(bad, t.messageProblems(place, module)...)
		bad = append(bad, t.oneSentenceProblems(place, answers)...)
	}
	return bad
}

func (t Tour) messageProblems(place copyPlace, module string) []string {
	switch {
	case place.msg.Key == "":
		return []string{fmt.Sprintf("tour %q: %s has no message key; a copy is a key and the English behind it", t.Ref.ID, place.where)}
	case place.msg.Fallback == "":
		return []string{fmt.Sprintf("tour %q: %s has no fallback copy; the fallback is what a person reads when no translation answers the key", t.Ref.ID, place.where)}
	case place.msg.Key == place.msg.Fallback:
		return []string{fmt.Sprintf("tour %q: %s is its own message key; a key is a name and a fallback is a sentence", t.Ref.ID, place.where)}
	case len(place.msg.Key) > maxKeyBytes || !copyKey.MatchString(place.msg.Key):
		return []string{fmt.Sprintf("tour %q, %s: copy key %q is not a dotted key of lower-case segments", t.Ref.ID, place.where, place.msg.Key)}
	case strings.SplitN(place.msg.Key, ".", 2)[0] != module:
		return []string{fmt.Sprintf("tour %q, %s: copy key %q is not namespaced by %q, the module that declares the tour", t.Ref.ID, place.where, place.msg.Key, module)}
	}
	return nil
}

// oneSentenceProblems refuses one key answering two sentences inside one tour:
// the copies resolve per person, and two instructions that disagree about what a
// key says is a defect no renderer can choose a side of honestly.
func (t Tour) oneSentenceProblems(place copyPlace, answers map[string]string) []string {
	if place.msg.Key == "" || !copyKey.MatchString(place.msg.Key) {
		return nil
	}
	first, seen := answers[place.msg.Key]
	switch {
	case !seen:
		answers[place.msg.Key] = place.msg.Fallback
	case first != place.msg.Fallback:
		return []string{fmt.Sprintf("tour %q: copy key %q answers two fallbacks, %q and %q; one key is one sentence", t.Ref.ID, place.msg.Key, first, place.msg.Fallback)}
	}
	return nil
}

func (t Tour) stepProblems() []string {
	if len(t.Steps) == 0 {
		return []string{fmt.Sprintf("tour %q: declares no steps; an offer that instructs nothing is not a tour", t.Ref.ID)}
	}
	var bad []string
	named := map[StepID]bool{}
	for i, s := range t.Steps {
		bad = append(bad, s.declarationProblems(t.Ref.ID, i, named)...)
	}
	return bad
}

// declarationProblems checks one step against its tour: whether it is a step at
// all, whether the tour can name it, where it belongs and how it is left. named
// carries the ids already used, so one pass says "twice" rather than once per
// repeat.
func (s Step) declarationProblems(id ID, index int, named map[StepID]bool) []string {
	if s.kind == "" {
		return []string{fmt.Sprintf("tour %q: step %d is the zero value; build a step with Spotlight, Tip or Modal", id, index)}
	}
	if s.id == "" {
		return []string{fmt.Sprintf("tour %q: a step has no id; a run says where it is by that name", id)}
	}
	if !stepName.MatchString(string(s.id)) {
		return []string{fmt.Sprintf("tour %q, step %q: a step id is a lower-case identifier", id, s.id)}
	}
	if named[s.id] {
		return []string{fmt.Sprintf("tour %q, step %q: one tour cannot name a step twice", id, s.id)}
	}
	named[s.id] = true
	bad := s.placeProblems(id)
	if len(bad) == 0 {
		bad = append(bad, s.surfaceProblems(id)...)
	}
	return append(bad, s.advanceProblems(id)...)
}

// surfaceProblems refuses a step sitting where no person can be: the view it was
// parsed off has already answered for its shape, so only the surface is left to
// ask, and the public one carries no run to play a step on.
func (s Step) surfaceProblems(id ID) []string {
	view := s.placedView()
	if !onPublicView(view) {
		return nil
	}
	return []string{fmt.Sprintf("tour %q, step %q: view %s %s", id, s.id, view, publicSurfaceRefusal)}
}

// placedView is the view a step sits on — its own for a modal, the host of the
// control for an anchored step — and is only asked once the place parsed.
func (s Step) placedView() screen.ViewRef {
	if s.kind == StepModal {
		return s.view
	}
	return anchorView(s.anchor)
}

func onPublicView(view screen.ViewRef) bool {
	return strings.HasPrefix(string(view), "public/")
}

func (s Step) placeProblems(id ID) []string {
	if s.kind == StepModal {
		return s.modalPlaceProblems(id)
	}
	if s.anchor == "" {
		return []string{fmt.Sprintf("tour %q, step %q: a %s step names no anchor", id, s.id, s.kind)}
	}
	if _, err := screen.ParseAnchor(string(s.anchor)); err != nil {
		return []string{fmt.Sprintf("tour %q, step %q: %v", id, s.id, err)}
	}
	if s.when != "" && s.when != Pause && s.when != Skip {
		return []string{fmt.Sprintf("tour %q, step %q: missing-anchor behaviour %q is neither %q nor %q", id, s.id, s.when, Pause, Skip)}
	}
	return nil
}

func (s Step) modalPlaceProblems(id ID) []string {
	if s.view == "" {
		return []string{fmt.Sprintf("tour %q, step %q: a %s step names no view", id, s.id, s.kind)}
	}
	if _, err := screen.ParseView(string(s.view)); err != nil {
		return []string{fmt.Sprintf("tour %q, step %q: %v", id, s.id, err)}
	}
	return nil
}

func (s Step) advanceProblems(id ID) []string {
	if s.kind == StepModal {
		return nil // Modal takes no Advance, so only an anchored step must say how it is left.
	}
	switch s.advance.kind {
	case AdvanceNext:
		return nil
	case AdvanceOn:
		return s.interactionProblems(id)
	case AdvanceCompletes:
		return s.operationProblems(id)
	}
	return []string{fmt.Sprintf("tour %q, step %q: it says nothing about how it is left; build an advance with Next, On or Completes", id, s.id)}
}

func (s Step) interactionProblems(id ID) []string {
	switch s.advance.interaction {
	case screen.Activated, screen.Committed, screen.Submitted:
		return nil
	}
	return []string{fmt.Sprintf("tour %q, step %q: %q is not an interaction an anchor reports; it reports %q, %q or %q",
		id, s.id, s.advance.interaction, screen.Activated, screen.Committed, screen.Submitted)}
}

func (s Step) operationProblems(id ID) []string {
	// screen holds a published operation id to this same segment rule, so the
	// sentence below states the whole of what this check can see and existence
	// stays the composition's to answer.
	if _, err := screen.Segment(string(s.advance.operation)); err == nil {
		return nil
	}
	return []string{fmt.Sprintf("tour %q, step %q: operation %q is not a name an operation can be mounted under; a name is non-empty, valid UTF-8 and holds no control character", id, s.id, s.advance.operation)}
}

func (t Tour) triggerProblems() []string {
	if t.Automatic == nil {
		return nil
	}
	switch t.Automatic.kind {
	case AutomaticOnVisit:
		if _, err := screen.ParseView(string(t.Automatic.view)); err != nil {
			return []string{fmt.Sprintf("tour %q: its automatic trigger: %v", t.Ref.ID, err)}
		}
		if onPublicView(t.Automatic.view) {
			return []string{fmt.Sprintf("tour %q: its automatic trigger's view %s %s", t.Ref.ID, t.Automatic.view, publicSurfaceRefusal)}
		}
	case AutomaticAfter:
		if t.Automatic.start == "" {
			return []string{fmt.Sprintf("tour %q: its automatic trigger names no event", t.Ref.ID)}
		}
	case "":
		return []string{fmt.Sprintf("tour %q: its automatic trigger is the zero value; build one with OnVisit or After", t.Ref.ID)}
	}
	return t.retirementProblems()
}

func (t Tour) retirementProblems() []string {
	var bad []string
	named := map[PersonEvent]bool{}
	for _, e := range t.Automatic.retires {
		if e == t.Automatic.start {
			bad = append(bad, fmt.Sprintf("tour %q: its automatic trigger and its retirement both name %q; the event that offers a tour cannot be the event that retires it", t.Ref.ID, e))
		}
		if named[e] {
			bad = append(bad, fmt.Sprintf("tour %q: its retirement names %q twice", t.Ref.ID, e))
		}
		named[e] = true
	}
	return bad
}
