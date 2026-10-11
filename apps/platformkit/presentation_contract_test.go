package main

// presentation_contract_test.go reads the served OpenAPI document rather than a
// claim about it. Every new key arrives beside a shape a response already
// carries, so the byte-stability promise is checkable: no `presentation` key in
// any required array, and no new component whose required list could turn an
// existing document into one a shipped shell refuses.

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// presentationComponents are the seven new component names. The two top-level
// ones are all-optional by design — an entry a nobody hinted prints no block at
// all — and the five nested ones are the unit of declaration, so every key
// inside one is always written and the mount gate refuses the declaration that
// would leave one empty.
var presentationComponents = map[string][]string{
	"EntryPresentation":   nil,
	"CommandPresentation": nil,
	"ResourceGroup":       {"key", "label"},
	"EntitySection":       {"key", "label"},
	"FieldReference":      {"resource"},
	"FieldMoney":          {"currencyField", "scale"},
	"CommandConfirmation": {"title", "body", "confirmLabel"},
}

func TestThePresentationKeysAreNeverRequired(t *testing.T) {
	raw, err := os.ReadFile("testdata/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	schemas := wireMap(doc["components"])
	components := wireMap(schemas["schemas"])
	for name, wantRequired := range presentationComponents {
		component, ok := components[name]
		if !ok {
			t.Fatalf("the served document describes no %q component, so a shell cannot read a hint", name)
		}
		required := wireStringList(wireMap(component)["required"])
		if got := wireMap(component)["required"]; got == nil && len(wantRequired) > 0 {
			t.Errorf("%q declares no required list; an object is the unit of declaration and every key inside one is written", name)
		}
		for _, key := range wantRequired {
			if !slices.Contains(required, key) {
				t.Errorf("%q does not require %q: %v", name, key, required)
			}
		}
		if wantRequired == nil && len(required) != 0 {
			t.Errorf("%q requires %v; every key of a presentation block is optional, or a new document would be a new version", name, required)
		}
	}

	// And the three shapes a presentation hangs off keep their own required
	// lists: an optional key added beside them moved no byte a shell reads.
	for _, name := range []string{"Entry", "Command", "rest_Entry", "entity_Field", "Field"} {
		component, ok := components[name]
		if !ok {
			continue
		}
		for _, key := range required2(wireMap(component)) {
			if key == "presentation" || strings.Contains(key, "Presentation") {
				t.Errorf("%q requires %q, which is optional on the wire", name, key)
			}
		}
	}
}

func required2(component map[string]any) []string { return wireStringList(component["required"]) }

// TestEveryDeclaredReferenceResolvesInTheReferenceComposition is the boot gate
// for a `reference`: a hint naming a module this installation did not compose is
// a composition mistake, and the served catalogue is where it would be published.
func TestEveryDeclaredReferenceResolvesInTheReferenceComposition(t *testing.T) {
	// The reference gate runs once per composition, at the first build of the
	// document — see fault.go's referenceGate for why the answer is remembered per
	// mount and not per process, and TestADanglingReferenceRefusesItsOwnComposition
	// FirstDocument for the refusal itself. So a product that serves a document is
	// a product whose references resolve. What is asserted here is the gate itself,
	// both ways, because "nothing was declared" is the shape a vacuous pass hides
	// behind.
	resources := []httpx.Resource{
		{Module: "task", Entity: "task", Schema: entity.Schema{Fields: []entity.Field{
			{Name: "assigneeId", Type: entity.TypeUUID,
				Presentation: entity.FieldHints{Reference: &entity.FieldReference{Resource: "user/user"}}}}}},
		{Module: "user", Entity: "user"},
	}
	if bad := rest.CheckReferences(resources); bad != "" {
		t.Errorf("the reference composition refuses its own wiring: %s", bad)
	}
	broken := []httpx.Resource{
		{Module: "task", Entity: "task", Schema: entity.Schema{Fields: []entity.Field{
			{Name: "assigneeId", Type: entity.TypeUUID,
				Presentation: entity.FieldHints{Reference: &entity.FieldReference{Resource: "usr/user"}}}}}},
		{Module: "user", Entity: "user"},
	}
	bad := rest.CheckReferences(broken)
	if bad == "" {
		t.Fatal("a reference to a module nobody composed is served")
	}
	for _, needle := range []string{`"usr/user"`, "assigneeId", "no composed module registers"} {
		if !strings.Contains(bad, needle) {
			t.Errorf("the refusal does not name %q: %s", needle, bad)
		}
	}
	// The same question asked of the declarations this application actually ships,
	// not of a pair written here: `assigneeId` and `author` reach for `user/user`,
	// and the logo field deliberately reaches for nothing (its target, `file/file`,
	// is a resource no composition registers). Counted, because "they all resolve"
	// is worth nothing if nothing was declared.
	real := referenceResources(t)
	declared := 0
	for _, r := range real {
		for _, f := range r.Schema.Fields {
			if f.Presentation.Reference != nil {
				declared++
			}
		}
	}
	if declared == 0 {
		t.Fatal("the reference composition declares no `reference:` at all, so the gate above checks nothing here")
	}
	if bad := rest.CheckReferences(real); bad != "" {
		t.Errorf("the reference composition's own declarations do not resolve: %s", bad)
	}
}
