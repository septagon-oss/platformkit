package tour_test

// The table below is the declaration pass read one way at a time: each row is the
// fixture with exactly one thing wrong with it, and each expected sentence is the
// whole of what a module's author reads. The fixture is the declaration a feature
// would actually write, so every refusal is a move away from something real rather
// than from nothing.

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/kit/tour"
)

func text(key, fallback string) tour.Message { return tour.Message{Key: key, Fallback: fallback} }

func fixtureTour() tour.Tour {
	start := tour.OnVisit("app/feature/custom/editor", "feature.note.created")
	return tour.Tour{
		Ref:           tour.Ref{ID: "feature.first", Edition: 1},
		Title:         text("feature.first", "Add your first note"),
		Prerequisites: []tour.Ref{{ID: "feature.signed_in", Edition: 1}},
		Automatic:     &start,
		Steps: []tour.Step{
			tour.Tip("title", "app/feature/custom/editor#field.title",
				tour.Content{Title: text("feature.first.step.title", "Name it"), Body: text("feature.first.step.title.body", "The title is what you will search for later.")},
				tour.On(screen.Committed), tour.AnchoredOptions{}),
			tour.Spotlight("save", "app/feature/custom/editor#action.submit",
				tour.Content{Title: text("feature.first.step.save", "Save it"), Body: text("feature.first.step.save.body", "Saving writes the note and takes you to it.")},
				tour.Completes("feature-note-create"), tour.AnchoredOptions{WhenMissing: tour.Skip}),
			tour.Modal("done", "app/feature/custom/done",
				tour.Content{Title: text("feature.first.step.done", "That is the shape of it"), Body: text("feature.first.step.done.body", "Come back to the help button whenever you want it again.")}),
		},
	}
}

// replace swaps one step of the fixture for the step under test, so a row states
// the one thing it changes and repeats nothing else about the declaration.
func replace(t *tour.Tour, id tour.StepID, step tour.Step) {
	for i, s := range t.Steps {
		if s.ID() == id {
			t.Steps[i] = step
			return
		}
	}
	panic("the fixture has no step " + string(id))
}

func TestAValidTourDeclaresItselfWithoutProblem(t *testing.T) {
	if problems := fixtureTour().DeclarationProblems("feature"); len(problems) != 0 {
		t.Fatalf("the fixture declaration is refused: %v", problems)
	}
}

func TestATourDeclarationIsRefusedForTheOneThingWrongWithIt(t *testing.T) {
	for _, tc := range []struct {
		name     string
		wrong    func(t *tour.Tour)
		sentence string
	}{
		{"id without a module", func(t *tour.Tour) { t.Ref.ID = "first" },
			`tour "first": an id is "<module>.<name>"`},
		{"id in another module's name", func(t *tour.Tour) { t.Ref.ID = "other.first" },
			`tour "other.first": its id names "other", which does not declare it; a tour belongs to the module whose name opens its id`},
		{"edition zero", func(t *tour.Tour) { t.Ref.Edition = 0 },
			`tour "feature.first": edition 0; a tour's first edition is 1, and an edition is bumped only for instruction a person may be given twice`},
		{"title is a fallback alone", func(t *tour.Tour) { t.Title = tour.Message{Fallback: "Add your first note"} },
			`tour "feature.first": its title has no message key; a copy is a key and the English behind it`},
		{"title is a key alone", func(t *tour.Tour) { t.Title = tour.Message{Key: "feature.first"} },
			`tour "feature.first": its title has no fallback copy; the fallback is what a person reads when no translation answers the key`},
		{"title is its own key", func(t *tour.Tour) {
			t.Title = tour.Message{Key: "feature.first", Fallback: "feature.first"}
		}, `tour "feature.first": its title is its own message key; a key is a name and a fallback is a sentence`},
		{"step copy key is a sentence", func(t *tour.Tour) {
			replace(t, "save", tour.Spotlight("save", "app/feature/custom/editor#action.submit",
				tour.Content{Title: text("feature.first.step.save", "Save it"), Body: text("Save it", "Saving writes the note and takes you to it.")},
				tour.Completes("feature-note-create"), tour.AnchoredOptions{WhenMissing: tour.Skip}))
		}, `tour "feature.first", step "save", its body: copy key "Save it" is not a dotted key of lower-case segments`},
		{"step copy key is another module's", func(t *tour.Tour) {
			replace(t, "save", tour.Spotlight("save", "app/feature/custom/editor#action.submit",
				tour.Content{Title: text("other.first.step.save", "Save it"), Body: text("feature.first.step.save.body", "Saving writes the note and takes you to it.")},
				tour.Completes("feature-note-create"), tour.AnchoredOptions{WhenMissing: tour.Skip}))
		}, `tour "feature.first", step "save", its title: copy key "other.first.step.save" is not namespaced by "feature", the module that declares the tour`},
		{"one key answers two sentences", func(t *tour.Tour) {
			replace(t, "save", tour.Spotlight("save", "app/feature/custom/editor#action.submit",
				tour.Content{Title: text("feature.first.step.title", "Give it a name"), Body: text("feature.first.step.save.body", "Saving writes the note and takes you to it.")},
				tour.Completes("feature-note-create"), tour.AnchoredOptions{WhenMissing: tour.Skip}))
		}, `tour "feature.first": copy key "feature.first.step.title" answers two fallbacks, "Name it" and "Give it a name"; one key is one sentence`},
		{"no steps", func(t *tour.Tour) { t.Steps = nil },
			`tour "feature.first": declares no steps; an offer that instructs nothing is not a tour`},
		{"step without an id", func(t *tour.Tour) {
			replace(t, "save", tour.Spotlight("", "app/feature/custom/editor#action.submit",
				tour.Content{Title: text("feature.first.step.save", "Save it"), Body: text("feature.first.step.save.body", "Saving writes the note and takes you to it.")},
				tour.Completes("feature-note-create"), tour.AnchoredOptions{}))
		}, `tour "feature.first": a step has no id; a run says where it is by that name`},
		{"modal sits on the public surface", func(t *tour.Tour) {
			replace(t, "done", tour.Modal("done", "public/feature/custom/welcome", tour.Content{
				Title: text("feature.first.step.done", "That is the shape of it"),
				Body:  text("feature.first.step.done.body", "Come back to the help button whenever you want it again.")}))
		}, `tour "feature.first", step "done": view public/feature/custom/welcome sits on the public surface, which admits no signed-in person, so no run can be kept`},
		{"anchor sits on the public surface", func(t *tour.Tour) {
			replace(t, "save", tour.Spotlight("save", "public/feature/custom/editor#action.submit",
				tour.Content{Title: text("feature.first.step.save", "Save it"), Body: text("feature.first.step.save.body", "Saving writes the note and takes you to it.")},
				tour.Completes("feature-note-create"), tour.AnchoredOptions{}))
		}, `tour "feature.first", step "save": view public/feature/custom/editor sits on the public surface, which admits no signed-in person, so no run can be kept`},
		{"step id with a space", func(t *tour.Tour) {
			replace(t, "title", tour.Tip("save now", "app/feature/custom/editor#field.title",
				tour.Content{Title: text("feature.first.step.title", "Name it"), Body: text("feature.first.step.title.body", "The title is what you will search for later.")},
				tour.On(screen.Committed), tour.AnchoredOptions{}))
		}, `tour "feature.first", step "save now": a step id is a lower-case identifier`},
		{"step named twice", func(t *tour.Tour) {
			t.Steps = append(t.Steps, t.Steps[1])
		}, `tour "feature.first", step "save": one tour cannot name a step twice`},
		{"zero step", func(t *tour.Tour) { t.Steps = []tour.Step{{}} },
			`tour "feature.first": step 0 is the zero value; build a step with Spotlight, Tip or Modal`},
		{"spotlight names no anchor", func(t *tour.Tour) {
			replace(t, "save", tour.Spotlight("save", "",
				tour.Content{Title: text("feature.first.step.save", "Save it"), Body: text("feature.first.step.save.body", "Saving writes the note and takes you to it.")},
				tour.Completes("feature-note-create"), tour.AnchoredOptions{}))
		}, `tour "feature.first", step "save": a spotlight step names no anchor`},
		{"modal names no view", func(t *tour.Tour) {
			replace(t, "done", tour.Modal("done", "", tour.Content{
				Title: text("feature.first.step.done", "That is the shape of it"),
				Body:  text("feature.first.step.done.body", "Come back to the help button whenever you want it again.")}))
		}, `tour "feature.first", step "done": a modal step names no view`},
		{"anchor is mispelled", func(t *tour.Tour) {
			replace(t, "save", tour.Spotlight("save", "app/feature/custom/editor#titel",
				tour.Content{Title: text("feature.first.step.save", "Save it"), Body: text("feature.first.step.save.body", "Saving writes the note and takes you to it.")},
				tour.Completes("feature-note-create"), tour.AnchoredOptions{}))
		}, `tour "feature.first", step "save": screen: invalid anchor name "titel"`},
		{"missing behaviour is a third word", func(t *tour.Tour) {
			replace(t, "save", tour.Spotlight("save", "app/feature/custom/editor#action.submit",
				tour.Content{Title: text("feature.first.step.save", "Save it"), Body: text("feature.first.step.save.body", "Saving writes the note and takes you to it.")},
				tour.Completes("feature-note-create"), tour.AnchoredOptions{WhenMissing: "hide"}))
		}, `tour "feature.first", step "save": missing-anchor behaviour "hide" is neither "pause" nor "skip"`},
		{"step says nothing about how it is left", func(t *tour.Tour) {
			replace(t, "save", tour.Spotlight("save", "app/feature/custom/editor#action.submit",
				tour.Content{Title: text("feature.first.step.save", "Save it"), Body: text("feature.first.step.save.body", "Saving writes the note and takes you to it.")},
				tour.Advance{}, tour.AnchoredOptions{}))
		}, `tour "feature.first", step "save": it says nothing about how it is left; build an advance with Next, On or Completes`},
		{"interaction is not one an anchor reports", func(t *tour.Tour) {
			replace(t, "title", tour.Tip("title", "app/feature/custom/editor#field.title",
				tour.Content{Title: text("feature.first.step.title", "Name it"), Body: text("feature.first.step.title.body", "The title is what you will search for later.")},
				tour.On("clicked"), tour.AnchoredOptions{}))
		}, `tour "feature.first", step "title": "clicked" is not an interaction an anchor reports; it reports "activated", "committed" or "submitted"`},
		{"completion names no operation", func(t *tour.Tour) {
			replace(t, "save", tour.Spotlight("save", "app/feature/custom/editor#action.submit",
				tour.Content{Title: text("feature.first.step.save", "Save it"), Body: text("feature.first.step.save.body", "Saving writes the note and takes you to it.")},
				tour.Completes(""), tour.AnchoredOptions{}))
		}, `tour "feature.first", step "save": operation "" is not a name an operation can be mounted under; a name is non-empty, valid UTF-8 and holds no control character`},
		{"completion names an operation that is not valid UTF-8", func(t *tour.Tour) {
			replace(t, "save", tour.Spotlight("save", "app/feature/custom/editor#action.submit",
				tour.Content{Title: text("feature.first.step.save", "Save it"), Body: text("feature.first.step.save.body", "Saving writes the note and takes you to it.")},
				tour.Completes("feature-note\xff-created"), tour.AnchoredOptions{}))
		}, `tour "feature.first", step "save": operation "feature-note\xff-created" is not a name an operation can be mounted under; a name is non-empty, valid UTF-8 and holds no control character`},
		{"trigger sits on the public surface", func(t *tour.Tour) {
			start := tour.OnVisit("public/feature/custom/welcome")
			t.Automatic = &start
		}, `tour "feature.first": its automatic trigger's view public/feature/custom/welcome sits on the public surface, which admits no signed-in person, so no run can be kept`},
		{"trigger's view is outside the view grammar", func(t *tour.Tour) {
			start := tour.OnVisit("app/feature/editor")
			t.Automatic = &start
		}, `tour "feature.first": its automatic trigger: screen: invalid view "app/feature/editor"`},
		{"zero automatic trigger", func(t *tour.Tour) { t.Automatic = new(tour.Automatic) },
			`tour "feature.first": its automatic trigger is the zero value; build one with OnVisit or After`},
		{"trigger names no event", func(t *tour.Tour) {
			start := tour.After("")
			t.Automatic = &start
		}, `tour "feature.first": its automatic trigger names no event`},
		{"trigger retires itself", func(t *tour.Tour) {
			start := tour.After("feature.note.created", "feature.note.created")
			t.Automatic = &start
		}, `tour "feature.first": its automatic trigger and its retirement both name "feature.note.created"; the event that offers a tour cannot be the event that retires it`},
		{"retirement names one event twice", func(t *tour.Tour) {
			start := tour.OnVisit("app/feature/custom/editor", "feature.a.created", "feature.a.created")
			t.Automatic = &start
		}, `tour "feature.first": its retirement names "feature.a.created" twice`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			declared := fixtureTour()
			tc.wrong(&declared)
			problems := declared.DeclarationProblems("feature")
			if len(problems) == 0 || problems[0] != tc.sentence {
				t.Fatalf("refusal = %q, want first %q", problems, tc.sentence)
			}
		})
	}
}

func TestTheOpaquePartsOfADeclarationCarryNoExportedField(t *testing.T) {
	for _, part := range []any{tour.Step{}, tour.Advance{}, tour.Automatic{}} {
		if exported := exportedFields(reflect.TypeOf(part)); len(exported) != 0 {
			t.Errorf("%T can be filled by a decoder: %v", part, exported)
		}
	}
}

func exportedFields(typ reflect.Type) []string {
	var names []string
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).IsExported() {
			names = append(names, typ.Field(i).Name)
		}
	}
	return names
}

// A document is how an authoring door would arrive. Decoded, the three opaque
// parts are the zero values they must be, and the declaration pass refuses what
// is left rather than playing a tour no module built.
func TestATourDecodedFromJSONIsRefusedRatherThanHalfBuilt(t *testing.T) {
	var declared tour.Tour
	body := `{"Ref":{"ID":"feature.first","Edition":1},"Title":{"Key":"feature.first","Fallback":"Add your first note"},
	  "Steps":[{"id":"save","kind":"spotlight","anchor":"app/feature/custom/editor#action.submit"}],
	  "Automatic":{"kind":"onVisit","view":"app/feature/custom/editor"}}`
	if err := json.Unmarshal([]byte(body), &declared); err != nil {
		t.Fatalf("decode: %v", err)
	}
	problems := declared.DeclarationProblems("feature")
	if len(problems) == 0 {
		t.Fatal("a decoded document built a tour no constructor was asked for")
	}
	want := `tour "feature.first": step 0 is the zero value; build a step with Spotlight, Tip or Modal`
	if !contains(problems, want) {
		t.Errorf("refusal = %q, want it to hold %q", problems, want)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
