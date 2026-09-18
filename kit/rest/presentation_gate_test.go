package rest_test

// The read axis of a field is a governed vocabulary in kit/entity, exactly as the
// write axis is (`ui:"widget:…"`, refused by widgetFault below). A presentation
// name no renderer knows has to refuse to mount rather than draw the value as
// text in silence — the failure the write axis had before widgetFault, and the
// reason both faults belong at the same mount site.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// Ghost is an entity whose only crime is naming a presentation nothing renders.
type Ghost struct {
	crud.Base
	Name string `json:"name" ui:"present:ghost"`
}

func (Ghost) TableName() string { return "rest_ghosts" }

// Claimed carries two directives in one tag, which is the grammar the entities
// here already depend on and the one that used to lose the second name.
type Claimed struct {
	crud.Base
	Handle string `json:"handle" ui:"present:person;hide:list"`
}

func (Claimed) TableName() string { return "rest_claimed" }

// TestAPresentationNoScreenRendersIsRefusedAtMount mirrors
// TestAWidgetNoScreenCanDrawIsRefusedAtMount on purpose, including the shape of
// the assertion: mount on an API with no router behind it panics for its own
// reasons, so a bare recover() would be satisfied by the wrong accident.
func TestAPresentationNoScreenRendersIsRefusedAtMount(t *testing.T) {
	var recovered any
	defer func() {
		recovered = recover()
		if recovered == nil {
			t.Error("Mount accepted an entity whose presentation renders nothing")
			return
		}
		message, _ := recovered.(string)
		if !strings.Contains(message, `presentation "ghost"`) || !strings.Contains(message, "no screen") {
			t.Errorf("Mount refused for another reason: %v", recovered)
		}
	}()
	rest.Spec[*Ghost]{Module: "ghost", Entity: "ghost", Path: "/api/ghosts",
		Read: "ghost:read", Write: "ghost:write"}.Mount(&httpx.API{})
}

// TestTheReadVocabularyIsClosedAndHoldsItsOwnNames. `select` is a control a
// person types into; `person` is a way a value is read. Keeping them apart is
// what stops a fake form control being invented for every read form.
func TestTheReadVocabularyIsClosedAndHoldsItsOwnNames(t *testing.T) {
	if len(entity.Presentations) == 0 {
		t.Fatal("kit/entity names no presentations, so the read axis has no vocabulary")
	}
	for _, name := range entity.Presentations {
		if !entity.ValidPresentation(name) {
			t.Errorf("ValidPresentation rejects %q, which it lists itself", name)
		}
		for _, widget := range entity.Widgets {
			if name == widget && name != "text" {
				t.Errorf("%q is in both vocabularies; the axes must stay separable", name)
			}
		}
	}
	if entity.ValidPresentation("ghost") {
		t.Error("ValidPresentation admits a name outside the vocabulary")
	}
	if !entity.ValidPresentation("") {
		t.Error(`"" must be valid: it means "derive from the type", which is most fields`)
	}
}

// TestPresentParsesFromTheExistingDirectiveGrammar: no new tag, no new separator.
// `,`/`;` separation is what the entities here already use, and losing the second
// directive is the exact bug the parser's comment records.
func TestPresentParsesFromTheExistingDirectiveGrammar(t *testing.T) {
	var handle entity.Field
	for _, f := range crud.Fields[*Claimed]() {
		if f.Name == "handle" {
			handle = f
		}
	}
	if handle.Present != "person" {
		t.Errorf(`present is %q; ui:"present:person;hide:list" must parse both directives`, handle.Present)
	}
	if !handle.HideList {
		t.Error("the second directive was lost next to the first")
	}
}
