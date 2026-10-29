package change_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/change"
	contracts "github.com/septagon-oss/platformkit/modules/change/contracts"
)

// resolve is the resolver a complete binding carries, never called.
func resolve(context.Context, db.Tx[db.Tenant], uuid.UUID) (contracts.Subject, error) {
	return nil, nil
}

// checkedProtection answers that no changed field is protected. Its method is never
// called: the refusals below happen while the gate is being built.
type checkedProtection struct{}

func (checkedProtection) Protected(context.Context, db.Tx[db.Tenant], contracts.DirectWrite) ([]string, error) {
	return nil, nil
}

// checkedRefusal is one Deps the constructor refuses and the sentence it answers
// with, in the order the constructor reaches it.
type checkedRefusal struct {
	name string
	deps change.Deps
	want string
}

func checkedRefusals() []checkedRefusal {
	bound := contracts.SubjectBinding{Module: "task", Entity: "task", Resolve: resolve}
	return []checkedRefusal{
		{"a shared service and the bindings beside it", change.Deps{Service: change.NewService(nil), Subjects: []contracts.SubjectBinding{bound}}, "change: bind the subjects on the shared service or supply them to Module, not both"},
		{"a binding that names no entity", change.Deps{Subjects: []contracts.SubjectBinding{{Module: "task"}}}, `change: the subject binding "task"/"" is incomplete`},
		{"one subject bound twice", change.Deps{Subjects: []contracts.SubjectBinding{bound, bound}}, "change: task/task is bound twice"},
	}
}

// TestNewCheckedNamesTheRefusalAndNewPanicsWithTheSameSentence: one proposal, one subject. Two bindings
// for one subject mean a reviewer's decision lands on whichever answer the map
// happened to keep.
func TestNewCheckedNamesTheRefusalAndNewPanicsWithTheSameSentence(t *testing.T) {
	for _, r := range checkedRefusals() {
		t.Run(r.name, func(t *testing.T) {
			_, err := change.NewChecked(r.deps)
			if err == nil {
				t.Fatal("NewChecked built a review queue that cannot say whose write it held")
			}
			if !strings.Contains(err.Error(), r.want) {
				t.Errorf("refusal = %q, want it to contain %q", err, r.want)
			}
			var got any
			func() {
				defer func() { got = recover() }()
				change.New(r.deps)
			}()
			if s, ok := got.(string); !ok || s != err.Error() {
				t.Errorf("panicked with %#v, want the string the checked form answered %q", got, err)
			}
		})
	}
}

// TestNewGateCheckedNamesTheWiringThatProtectsNothing: a gate is a door with a
// subject behind it and an address beside the refusal.
func TestNewGateCheckedNamesTheWiringThatProtectsNothing(t *testing.T) {
	gate := change.Gate{
		Subject:    contracts.SubjectRef{Module: "task", Entity: "task"},
		Protection: checkedProtection{},
		WayOn:      "/app/task/{id}",
	}
	for _, w := range []struct {
		name string
		gate change.Gate
		want string
	}{
		{"nothing protected", change.Gate{Subject: gate.Subject, WayOn: gate.WayOn}, "protects nothing"},
		{"no subject named", change.Gate{Protection: gate.Protection, WayOn: gate.WayOn}, "name the subject its door writes"},
		{"no way through named", change.Gate{Protection: gate.Protection, Subject: gate.Subject}, "name the address the write goes through"},
	} {
		t.Run(w.name, func(t *testing.T) {
			if _, err := change.NewGateChecked(w.gate); err == nil {
				t.Fatal("a gate that protects nothing was accepted")
			} else if !strings.Contains(err.Error(), w.want) {
				t.Errorf("refusal = %q, want it to contain %q", err, w.want)
			}
		})
	}
	if _, err := change.NewGateChecked(gate); err != nil {
		t.Errorf("NewGateChecked with a complete gate: %v", err)
	}
}

// TestNewCheckedBuildsFromItsRequiredDependenciesAlone: change control over nothing
// is what a composition with no protected field asked for — the queue stays empty.
func TestNewCheckedBuildsFromItsRequiredDependenciesAlone(t *testing.T) {
	m, err := change.NewChecked(change.Deps{})
	if err != nil {
		t.Fatalf("NewChecked with an empty Deps: %v", err)
	}
	if m.Name != "change" {
		t.Errorf("manifest name = %q, want change", m.Name)
	}
}
