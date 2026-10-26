package entity_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
)

func choiceSet() entity.ChoiceSet {
	return entity.ChoiceSet{Cardinality: "many", Empty: entity.ChoiceEmpty{Mode: "allow"},
		Options: []entity.ChoiceOption{{ID: "round", Label: "Round"}, {ID: "square", Label: "Square"}, {ID: "triangle", Label: "Triangle"}, {ID: "none", Label: "No preference"}}, NoPreference: "none"}
}

func TestChoiceActionsPreserveOrderAndExclusiveAnswers(t *testing.T) {
	c := choiceSet()
	c.Maximum = &entity.ChoiceMaximum{Count: 2, Behaviour: "replace-oldest"}
	var current []string
	for _, tc := range []struct {
		kind, id string
		want     []string
	}{
		{"pick", "square", []string{"square"}},
		{"pick", "round", []string{"square", "round"}},
		{"pick", "square", []string{"square", "round"}},
		{"pick", "triangle", []string{"round", "triangle"}},
		{"pick", "none", []string{"none"}},
		{"pick", "round", []string{"round"}},
		{"remove", "square", []string{"round"}},
		{"clear", "", []string{}},
	} {
		before := slices.Clone(current)
		got, err := c.Apply(current, entity.ChoiceAction{Kind: tc.kind, ID: tc.id})
		if err != nil || !slices.Equal(got, tc.want) || !slices.Equal(before, current) {
			t.Fatalf("%s %s: got %v, %v; want %v; current %v", tc.kind, tc.id, got, err, tc.want, current)
		}
		current = got
	}
}

func TestChoiceRefusalsReturnNoCandidate(t *testing.T) {
	c := choiceSet()
	c.Empty = entity.ChoiceEmpty{Mode: "refuse", Message: "Choose a shape."}
	c.Maximum = &entity.ChoiceMaximum{Count: 2, Behaviour: "refuse", Message: "Choose at most two."}
	for _, input := range [][]string{nil, {"unknown"}, {"round", "round"}, {"none", "round"}, {"round", "square", "triangle"}} {
		if got, err := c.Answer(input); err == nil || got != nil {
			t.Fatalf("accepted %v: %v, %v", input, got, err)
		}
	}
	if got, err := c.Change(nil, nil); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("already empty change: %v, %v", got, err)
	}
	if got, err := c.Apply([]string{"round"}, entity.ChoiceAction{Kind: "remove", ID: "round"}); err == nil || got != nil || err.Error() != "Choose a shape." {
		t.Fatalf("last removal: %v, %v", got, err)
	}
	c.Maximum.Behaviour, c.Maximum.Message = "replace-oldest", ""
	for _, input := range [][]string{{"unknown", "round", "square"}, {"round", "round", "square"}} {
		if got, err := c.Answer(input); err == nil || got != nil {
			t.Fatalf("discarded invalid prefix: %v, %v", got, err)
		}
	}
}

func TestOneChoiceReplacesAndRejectsMultipleAnswers(t *testing.T) {
	c := choiceSet()
	c.Cardinality = "one"
	got, err := c.Apply([]string{"round"}, entity.ChoiceAction{Kind: "pick", ID: "square"})
	if err != nil || !slices.Equal(got, []string{"square"}) {
		t.Fatalf("replacement: %v, %v", got, err)
	}
	if got, err := c.Answer([]string{"round", "square"}); err == nil || got != nil {
		t.Fatalf("multiple: %v, %v", got, err)
	}
}

func TestChoiceDeclarationsRefuseInvalidMembers(t *testing.T) {
	for name, change := range map[string]func(*entity.ChoiceSet){
		"cardinality": func(c *entity.ChoiceSet) { c.Cardinality = "" },
		"empty":       func(c *entity.ChoiceSet) { c.Empty.Mode = "" },
		"sentence":    func(c *entity.ChoiceSet) { c.Empty.Mode = "refuse" },
		"options":     func(c *entity.ChoiceSet) { c.Options = nil },
		"duplicate":   func(c *entity.ChoiceSet) { c.Options[1].ID = "round" },
		"label":       func(c *entity.ChoiceSet) { c.Options[0].Label = "\t" },
		"threshold":   func(c *entity.ChoiceSet) { c.SearchThreshold = 257 },
		"maximum":     func(c *entity.ChoiceSet) { c.Maximum = &entity.ChoiceMaximum{Count: 0, Behaviour: "replace-oldest"} },
		"exclusive":   func(c *entity.ChoiceSet) { c.NoPreference = "missing" },
		"picture": func(c *entity.ChoiceSet) {
			c.Options[0].Picture = &entity.ChoicePicture{Kind: "colour", Value: "url(x)"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := choiceSet()
			change(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("accepted invalid declaration")
			}
		})
	}
}

func TestChoiceOverlayCopiesMetadataAndChecksShape(t *testing.T) {
	type value struct {
		Shapes []string `json:"shapes"`
	}
	fields := entity.FieldsOf(reflect.TypeFor[value]())
	c := choiceSet()
	c.Options[0].Group = &entity.ChoiceGroup{Key: "shapes", Label: "Shapes"}
	d := &entity.ChoiceFields{Fields: []entity.ChoiceField{{Name: "shapes", Choices: c}}}
	got, err := entity.WithChoices(fields, d)
	if err != nil {
		t.Fatal(err)
	}
	got[0].Choices.Options[0].Group.Label = "Changed"
	if fields[0].Choices != nil || c.Options[0].Group.Label != "Shapes" {
		t.Fatal("shared metadata changed")
	}
	d.Fields[0].Choices.Cardinality = "one"
	if _, err := entity.WithChoices(fields, d); err == nil {
		t.Fatal("accepted scalar declaration on list")
	}
}
