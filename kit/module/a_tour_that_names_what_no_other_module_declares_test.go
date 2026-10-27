package module_test

// These rules are the ones only the whole set can answer: an event a tour waits
// on belongs to whoever emits it, a prerequisite names a tour somebody in this
// composition declares, and one edition belongs to one module. Every sentence
// arrives inside the single sorted report Validate prints, so each row asks that
// its own sentence is in it.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/kit/tour"
)

func copy2(key, fallback string) tour.Message { return tour.Message{Key: key, Fallback: fallback} }

func tourOf(id tour.ID, edition uint32, steps ...tour.Step) tour.Tour {
	return tour.Tour{
		Ref:   tour.Ref{ID: id, Edition: edition},
		Title: copy2("feature.first", "Add your first note"),
		Steps: steps,
	}
}

func saveStep() tour.Step {
	return tour.Spotlight("save", "app/feature/custom/editor#action.submit",
		tour.Content{Title: copy2("feature.first.step.save", "Save it"), Body: copy2("feature.first.step.save.body", "Saving writes the note.")},
		tour.Next(), tour.AnchoredOptions{})
}

// feature is the composition these rules are read against: it emits one event a
// tour can wait on, publishes the screen a step anchors to, and declares the tour
// the other one waits for.
func feature(mods ...tour.Tour) module.Module {
	m := module.Module{
		Name:   "feature",
		Events: []string{"feature.note.created"},
		Views: []screen.View{screen.MustView(screen.ViewSpec{
			Ref: "app/feature/custom/editor", Cardinality: screen.One,
			Anchors: []screen.AnchorSpec{{Name: "action.submit", Control: "button", Binding: screen.Submission("feature-note-create"), Interactions: []screen.Interaction{screen.Submitted}}},
		})},
	}
	if len(mods) > 0 {
		m.Tours = mods
		return m
	}
	signedIn := tourOf("feature.signed_in", 1, saveStep())
	first := tourOf("feature.first", 1, saveStep())
	first.Prerequisites = []tour.Ref{{ID: "feature.signed_in", Edition: 1}}
	start := tour.OnVisit("app/feature/custom/editor", "feature.note.created")
	first.Automatic = &start
	m.Tours = []tour.Tour{signedIn, first}
	return m
}

func refuse(t *testing.T, mods ...module.Module) string {
	t.Helper()
	err := module.Validate(module.Expand(mods))
	if err == nil {
		t.Fatal("Validate accepted a tour the manifest refuses")
	}
	return err.Error()
}

func TestAWellDeclaredTourOfItsOwnModulePassesTheManifest(t *testing.T) {
	if err := module.Validate(module.Expand([]module.Module{feature()})); err != nil {
		t.Fatalf("Validate refused a sound composition: %v", err)
	}
}

func TestATourIsRefusedForWhatOnlyTheWholeCompositionAnswers(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mods     []module.Module
		sentence string
	}{
		{"trigger event is not an event name", []module.Module{feature(withTrigger(tour.After("note created")))},
			`tour "feature.first": its automatic trigger names event "note created", which is not "<name>.<event>"`},
		{"trigger event is emitted by nobody", []module.Module{feature(withTrigger(tour.After("feature.note.archived")))},
			`tour "feature.first": its automatic trigger names "feature.note.archived", which no composed module emits`},
		{"retirement event is not an event name", []module.Module{feature(withTrigger(tour.OnVisit("app/feature/custom/editor", "note removed")))},
			`tour "feature.first": its retirement names event "note removed", which is not "<name>.<event>"`},
		{"retirement event is emitted by nobody", []module.Module{feature(withTrigger(tour.OnVisit("app/feature/custom/editor", "feature.note.removed")))},
			`tour "feature.first": its retirement names "feature.note.removed", which no composed module emits`},
		{"one edition is declared by two modules", []module.Module{feature(), other()},
			`tour "feature.first" edition 1: declared twice, by module "feature" and module "feature2"`},
		{"prerequisite names edition zero", []module.Module{featureWithPrerequisite(tour.Ref{ID: "feature.signed_in"})},
			`tour "feature.first" edition 1: prerequisite "feature.signed_in" names edition 0; a tour's first edition is 1`},
		{"prerequisite is no tour of this composition", []module.Module{featureWithPrerequisite(tour.Ref{ID: "feature.signed_in", Edition: 2})},
			`tour "feature.first" edition 1: prerequisite "feature.signed_in" edition 2 is not a tour of this composition`},
		{"prerequisite is the tour itself", []module.Module{featureWithPrerequisite(tour.Ref{ID: "feature.first", Edition: 1})},
			`tour "feature.first" edition 1: a tour cannot require a person to finish it first`},
		{"prerequisite is named twice", []module.Module{featureWithPrerequisite(tour.Ref{ID: "feature.signed_in", Edition: 1}, tour.Ref{ID: "feature.signed_in", Edition: 1})},
			`tour "feature.first" edition 1: prerequisite "feature.signed_in" edition 1 is named twice; one tour is finished once or it is not a prerequisite`},
		{"prerequisites form a cycle", []module.Module{cycled()},
			`tour "feature.b" edition 2: prerequisite cycle "feature.b@2" -> "feature.a@1" -> "feature.b@2"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if report := refuse(t, tc.mods...); !strings.Contains(report, tc.sentence) {
				t.Errorf("report does not hold %q:\n%s", tc.sentence, report)
			}
		})
	}
}

func featureWithPrerequisite(prerequisites ...tour.Ref) module.Module {
	m := feature()
	for i, t := range m.Tours {
		if t.Ref.ID == "feature.first" {
			m.Tours[i].Prerequisites = prerequisites
		}
	}
	return m
}

func withTrigger(start tour.Automatic) tour.Tour {
	m := feature()
	for _, t := range m.Tours {
		if t.Ref.ID == "feature.first" {
			t.Automatic = &start
			t.Prerequisites = nil
			return t
		}
	}
	panic("the fixture declares no tour feature.first")
}

// other declares a tour in a namespace it does not own, so the composition holds
// one edition written down twice and says which two manifests wrote it.
func other() module.Module {
	claimed := tourOf("feature.first", 1, saveStep())
	return module.Module{Name: "feature2", Tours: []tour.Tour{claimed}}
}

func cycled() module.Module {
	a := tourOf("feature.a", 1, saveStep())
	a.Prerequisites = []tour.Ref{{ID: "feature.b", Edition: 2}}
	b := tourOf("feature.b", 2, saveStep())
	b.Prerequisites = []tour.Ref{{ID: "feature.a", Edition: 1}}
	return feature(b, a)
}
