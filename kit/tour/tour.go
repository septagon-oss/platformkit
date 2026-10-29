// Package tour declares how one feature teaches itself, step by step, on the
// screens a composition actually publishes.
//
// A module that owns a feature writes its tours beside its Nav and Views, and
// writes no renderer, no CSS and no JavaScript: the web controller and a phone
// kit component play the same declaration. A composition that selects no tour
// runner has every declaration validated all the same, and shows nothing.
//
// A tour borrows its names — a view, a control, an operation, an event — from
// the owners that published them and re-derives none of them. Automatic, Step
// and Advance are opaque values built only by their constructors, so a document
// decoded into a declaration yields zero values, which the manifest pass refuses
// by name. The rules live here and the doors do not: kit/module reads a
// declaration as a manifest, kit/app against what the composition published.
package tour

import "github.com/septagon-oss/platformkit/kit/screen"

// ID is a tour's name: the declaring module's name, a dot, and the tour's own
// lower-case name — "task.first_task". A tour belongs to the module whose name
// opens its id, and that id is what a person's completion state is kept under.
type ID string

// StepID names one step inside one tour. Ids are unique per tour rather than per
// composition: a run pins a tour, so the tour's name is already on the wire.
type StepID string

// Ref is one edition of one tour. Edition 1 is a tour's first; a bump means
// materially different instruction, which is why the new edition may be offered
// again to a person who finished the edition before it. Correcting a sentence is
// not a bump: it moves the content revision of a live run, not the edition.
type Ref struct {
	ID      ID
	Edition uint32
}

// Message is one piece of copy: a key a translation answers and the English a
// person reads when none does — the same pair locale.Formatter.Text takes, which
// is the only door that reads it. Copy resolves where it renders, never where a
// tour is declared.
type Message struct {
	Key      string
	Fallback string
}

// Content is an instruction's copy: what to notice, and what to do about it.
type Content struct {
	Title, Body Message
}

// Missing is what a run does when an anchored step's control is not there. A
// centred substitute for a missing control is never one of the answers.
type Missing string

const (
	// Pause stops at the step with Retry and Exit. An empty WhenMissing means
	// Pause: a step that says nothing about it is a required step.
	Pause Missing = "pause"
	// Skip moves on without the step, recorded skipped and never viewed. It
	// never bypasses an authorization: a lost permission stops the run rather
	// than stepping over it.
	Skip Missing = "skip"
)

// AnchoredOptions is the only choice an anchored step makes. Placement is not
// here: the renderer chooses it from the viewport, the keyboard and the safe
// area.
type AnchoredOptions struct {
	WhenMissing Missing
}

// OperationRef names one operation of the composition — the id its published
// screen contract carries — whose authoritative successful outcome the server can
// observe. The module that owns the operation owns the name; a tour borrows it
// and never invents one.
type OperationRef string

// PersonEvent names one event of the composition, "<module>.<event>", matched to
// the person who would see the tour: which event a run waits for, or which one
// retires it. That this person caused the event is not this value's claim.
type PersonEvent string

// StepKind is how a step presents itself. There is no mode field and no
// nullable anchor: the constructor that built a step is the only word about it.
type StepKind string

const (
	StepSpotlight StepKind = "spotlight"
	StepTip       StepKind = "tip"
	StepModal     StepKind = "modal"
)

// AdvanceKind is what ends a step.
type AdvanceKind string

const (
	AdvanceNext      AdvanceKind = "next"
	AdvanceOn        AdvanceKind = "on"
	AdvanceCompletes AdvanceKind = "completes"
)

// AutomaticKind is what offers a tour without anyone asking for it.
type AutomaticKind string

const (
	AutomaticOnVisit AutomaticKind = "onVisit"
	AutomaticAfter   AutomaticKind = "after"
)

// Tour is one guided instruction, declared by the module that owns the feature
// it teaches. Steps are the instructions in the order a person walks them: there
// is no order field, because the order is the declaration. Every tour a person
// may reach is also askable for by hand; Automatic says only which events or
// visits may offer it without being asked.
type Tour struct {
	Ref Ref
	// Title names this tour in the help listing; a step's own copy is Content.
	Title Message

	// Prerequisites are tours this one waits for, each at one exact edition.
	// A reference nobody declares, a self-reference and a cycle are refused:
	// a tour no person can ever be offered is worse than one that is absent.
	Prerequisites []Ref

	// Automatic is nil for a tour that is only ever asked for.
	Automatic *Automatic

	Steps []Step
}

// Automatic is opaque: its zero value is refused, and only OnVisit and After
// build one. The constructors return a value, so a declaration takes a local
// first: `start := tour.OnVisit(view); … Automatic: &start`.
type Automatic struct {
	kind    AutomaticKind
	view    screen.ViewRef
	start   PersonEvent
	retires []PersonEvent
}

// OnVisit offers the tour when the person reaches view. Whether this particular
// visit starts a run is the frequency policy's, not this declaration's.
func OnVisit(view screen.ViewRef, retireWhen ...PersonEvent) Automatic {
	return Automatic{kind: AutomaticOnVisit, view: view, retires: append([]PersonEvent{}, retireWhen...)}
}

// After offers the tour once start has happened for this person.
func After(start PersonEvent, retireWhen ...PersonEvent) Automatic {
	return Automatic{kind: AutomaticAfter, start: start, retires: append([]PersonEvent{}, retireWhen...)}
}

// Kind reports how this trigger offers the tour, or "" for the zero value.
func (a Automatic) Kind() AutomaticKind { return a.kind }

// View is the view whose visit offers the tour, and is empty for any other kind.
func (a Automatic) View() screen.ViewRef { return a.view }

// Start is the event that offers the tour, and is empty for any other kind.
func (a Automatic) Start() PersonEvent { return a.start }

// Retires names the events after which this tour teaches nothing more.
func (a Automatic) Retires() []PersonEvent { return append([]PersonEvent{}, a.retires...) }

// Step is opaque: its zero value is refused, and only Spotlight, Tip and Modal
// build one. An anchored step answers View with "" and a modal answers Anchor
// with "", so the two cannot contradict each other the way a mode field and a
// nullable anchor could.
type Step struct {
	id      StepID
	kind    StepKind
	anchor  screen.AnchorRef
	view    screen.ViewRef
	content Content
	advance Advance
	when    Missing
}

// Spotlight dims around anchor and leaves the real control usable.
func Spotlight(id StepID, anchor screen.AnchorRef, content Content, advance Advance, opts AnchoredOptions) Step {
	return Step{id: id, kind: StepSpotlight, anchor: anchor, content: content, advance: advance, when: opts.WhenMissing}
}

// Tip is non-modal guidance beside anchor; it never takes focus on its own.
func Tip(id StepID, anchor screen.AnchorRef, content Content, advance Advance, opts AnchoredOptions) Step {
	return Step{id: id, kind: StepTip, anchor: anchor, content: content, advance: advance, when: opts.WhenMissing}
}

// Modal is unanchored instruction at view. It advances by acknowledgement, which
// is why it takes no Advance.
func Modal(id StepID, at screen.ViewRef, content Content) Step {
	return Step{id: id, kind: StepModal, view: at, content: content}
}

// ID is the step's name within its tour.
func (s Step) ID() StepID { return s.id }

// Kind reports how this step presents itself, or "" for the zero value.
func (s Step) Kind() StepKind { return s.kind }

// Anchor is the control this step points at, and is empty for a modal step.
func (s Step) Anchor() screen.AnchorRef { return s.anchor }

// View is the view a modal step belongs to, and is empty for an anchored step.
func (s Step) View() screen.ViewRef { return s.view }

// Content is the instruction's copy.
func (s Step) Content() Content { return s.content }

// Advance is what ends this step, and is zero for a modal step.
func (s Step) Advance() Advance { return s.advance }

// WhenMissing is what a run does when this step's control is not there. The
// empty value means Pause.
func (s Step) WhenMissing() Missing { return s.when }

// Advance is opaque: its zero value is refused for an anchored step, and only
// Next, On and Completes build one.
type Advance struct {
	kind        AdvanceKind
	interaction screen.Interaction
	operation   OperationRef
}

// Next ends the step when the person acknowledges it.
func Next() Advance { return Advance{kind: AdvanceNext} }

// On ends the step when the guided anchor reports interaction — successfully.
// Committed is an accepted value change and Submitted a successful submission; a
// control that rejected its input reports neither.
func On(i screen.Interaction) Advance { return Advance{kind: AdvanceOn, interaction: i} }

// Completes ends the step when the server observes the authoritative successful
// outcome of operation — the write the guided control started, which the gate
// holds it to. A failed save, a write awaiting approval and a queued submission
// are not that outcome.
func Completes(operation OperationRef) Advance {
	return Advance{kind: AdvanceCompletes, operation: operation}
}

// Kind reports what ends the step, or "" for the zero value.
func (a Advance) Kind() AdvanceKind { return a.kind }

// Interaction is the interaction On waits for, and is empty for any other kind.
func (a Advance) Interaction() screen.Interaction { return a.interaction }

// Operation is the operation Completes waits for, and is empty for any other
// kind.
func (a Advance) Operation() OperationRef { return a.operation }

// Composition is what a composition published, as far as a tour may ask.
// screen.Inventory answers View and Anchor as they are; the operation door is
// filled at composition from the published snapshot, so a tour never reads a
// route table of its own.
type Composition interface {
	View(screen.ViewRef) (screen.View, error)
	Anchor(screen.AnchorRef) (screen.Anchor, error)
	Operation(id string) (screen.Operation, bool)
}
