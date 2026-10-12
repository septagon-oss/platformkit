package main

// The reference gate's own two halves: a composition whose declaration points at
// a resource nobody registered refuses the document it is about to publish, and
// the composition beside it in the same process answers for its own list. The
// gate used to remember its answer for the whole process, so whichever
// composition built a document first decided for every later one — a second
// composition's dangling reference went unchecked, and a correct one behind a
// broken one had nothing to serve but 500s.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// pointing registers `module/entity` with one field reaching for `target`, which
// is the whole shape of the mistake: the declaration is legal, the spelling is
// legal, and the thing it names was never composed.
func pointing(module, entityName, target string) httpx.Resource {
	return httpx.Resource{Module: module, Entity: entityName, Schema: entity.Schema{Fields: []entity.Field{
		{Name: "who", Type: entity.TypeUUID,
			Presentation: entity.FieldHints{Reference: &entity.FieldReference{Resource: target}}}}}}
}

// TestADanglingReferenceRefusesItsOwnCompositionFirstDocument is the loud
// refusal, asked of the reference composition itself with one dangling
// declaration planted in its list: the answer names the declaration and the
// target it could not find, and no document arrives beside it. The second call
// is the once — the same refusal, not a second walk of the same schemas.
func TestADanglingReferenceRefusesItsOwnCompositionFirstDocument(t *testing.T) {
	resources := append(referenceResources(t), pointing("planted", "planted", "usr/user"))
	build := documentBuilder(new(referenceGate))
	document, err := build(t.Context(), resources)
	if err == nil {
		t.Fatalf("a composition whose reference names no registered resource served a document: %+v", document)
	}
	if document != nil {
		t.Errorf("a refused document came back with a body anyway: %+v", document)
	}
	for _, want := range []string{"planted/planted", "who", "usr/user"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %s, so the author is left to guess which declaration reached: %s", want, err)
		}
	}
	if _, again := build(t.Context(), resources); again == nil {
		t.Error("the second document of the same composition answered: the refusal is the answer until the composition is fixed")
	}
}

// TestTwoCompositionsAskTheReferenceQuestionEachForItsOwnList is the half the
// process-wide once made untrue: the broken composition builds first, the
// reference composition builds after it and is not poisoned by it — which is
// also the shipped promise, that the five resources' own declarations answer for
// the list this application registers — and the broken one still refuses after.
// Both builds run in one process, which is what every test binary here does.
func TestTwoCompositionsAskTheReferenceQuestionEachForItsOwnList(t *testing.T) {
	resources := referenceResources(t)
	broken := documentBuilder(new(referenceGate))
	if _, err := broken(t.Context(), append(append([]httpx.Resource{}, resources...), pointing("planted", "planted", "usr/user"))); err == nil {
		t.Fatal("the planted composition served a document, so there is nothing for the reference composition to be poisoned by")
	}
	healthy := documentBuilder(new(referenceGate))
	if _, err := healthy(t.Context(), resources); err != nil {
		t.Fatalf("the reference composition refused because another one built first: %s", err)
	}
	if _, err := broken(t.Context(), append(append([]httpx.Resource{}, resources...), pointing("planted", "planted", "usr/user"))); err == nil {
		t.Error("the planted composition stopped refusing after a healthy one built beside it")
	}
}
