package main

// The served document is all a generated client knows about a resolved policy, and
// the conformance check this repository runs against it names the component an object
// answers from the members it holds: an object whose keys cover that component's
// required members and hold nothing outside its published properties is read as one of
// them. A stepped form and a field's editability are told apart by those names alone.
//
// They print the same names. A field's editability is {"create":…,"update":…} and
// requires both; the record's forms publish create and update too and require neither,
// so every editability object is also a forms answer. The check then asks one
// component's question — may this member be left out? — of an object of the other, and
// the document, which is right about both, answers the wrong question. Anything that
// types a resolved object the same way is wrong the same way.

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestARecordsSteppedFormIsNotReadAsAFieldsEditability(t *testing.T) {
	components := goldenComponents(t)
	forms := requireComponent(t, components, "ResolvedForms")
	editability := requireComponent(t, components, "ResolvedEditable")

	// The stepped answer has to be in the document at all: a component emptied of its
	// members would pass the question below by publishing nothing to read.
	if !publishesASteppedForm(t, components, forms) {
		t.Fatal("the served document publishes no forms member whose answer carries the layout and steps a stepped record prints")
	}

	// An editability object is printed as its own component requires it and nothing more.
	printed := members(editability, requiredOnly)
	if readsAs(forms, printed) {
		t.Fatalf("a field's editability %v is read as the record's forms: ResolvedForms requires %v and publishes %v, "+
			"so the members meant to tell one component's object from the other's name both",
			printed, members(forms, requiredOnly), members(forms, publishedOnly))
	}
}

type memberKind bool

const (
	publishedOnly memberKind = false
	requiredOnly  memberKind = true
)

// members answers a component's member names, sorted: every one it publishes, or the
// ones it cannot be read without.
func members(component map[string]any, kind memberKind) []string {
	holders, _ := component["properties"].(map[string]any)
	wanted := map[string]bool{}
	if kind == requiredOnly {
		list, _ := component["required"].([]any)
		for _, item := range list {
			if name, ok := item.(string); ok {
				wanted[name] = true
			}
		}
	}
	var names []string
	for name := range holders {
		if kind == publishedOnly || wanted[name] {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// readsAs asks whether an object holding exactly these members answers the component:
// it covers what the component requires and names nothing it does not publish.
func readsAs(component map[string]any, held []string) bool {
	return len(held) > 0 && within(held, members(component, publishedOnly)) &&
		within(members(component, requiredOnly), held)
}

// within answers whether every name of need appears among have, both already sorted.
func within(need, have []string) bool {
	for _, name := range need {
		if !slices.Contains(have, name) {
			return false
		}
	}
	return true
}

// publishesASteppedForm asks whether any member of the component resolves to the answer
// a step is read from: one that carries the layout and the steps.
func publishesASteppedForm(t *testing.T, components map[string]map[string]any, component map[string]any) bool {
	t.Helper()
	for _, schema := range members(component, publishedOnly) {
		holders, _ := component["properties"].(map[string]any)
		body, _ := holders[schema].(map[string]any)
		ref, _ := body["$ref"].(string)
		name, _ := strings.CutPrefix(ref, "#/components/schemas/")
		if answer := components[name]; answer != nil && within([]string{"layout", "steps"}, members(answer, publishedOnly)) {
			return true
		}
	}
	return false
}

// goldenComponents reads the components the reference composition publishes.
func goldenComponents(t *testing.T) map[string]map[string]any {
	t.Helper()
	raw, err := os.ReadFile(openapiGolden)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	parts, _ := doc["components"].(map[string]any)
	schemas, _ := parts["schemas"].(map[string]any)
	if len(schemas) == 0 {
		t.Fatal("the golden document publishes no components at all")
	}
	out := make(map[string]map[string]any, len(schemas))
	for name, schema := range schemas {
		if body, ok := schema.(map[string]any); ok {
			out[name] = body
		}
	}
	return out
}

func requireComponent(t *testing.T, components map[string]map[string]any, name string) map[string]any {
	t.Helper()
	body, ok := components[name]
	if !ok {
		t.Fatalf("the served document publishes no %s component", name)
	}
	return body
}
