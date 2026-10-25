package entity_test

import (
	"github.com/septagon-oss/platformkit/kit/entity"
	"strings"
	"testing"
)

func TestChoiceFieldDeclarationsRejectConflictingMetadataAndDefaults(t *testing.T) {
	base := func() entity.Field {
		return entity.Field{Name: "shapes", Type: entity.TypeList, Elem: entity.TypeString, Choices: new(choiceSet())}
	}
	for name, change := range map[string]func(*entity.Field){
		"reserved name":      func(f *entity.Field) { f.Name = "__pkit_choice_present" },
		"enum":               func(f *entity.Field) { f.Enum = []string{"round"} },
		"required empty":     func(f *entity.Field) { f.Required = true },
		"wrong widget":       func(f *entity.Field) { f.Widget = "text" },
		"wrong presentation": func(f *entity.Field) { f.Present = "list" },
		"missing figure":     func(f *entity.Field) { f.Widget = "figure" },
		"missing colour":     func(f *entity.Field) { f.Widget = "swatch" },
		"unknown default":    func(f *entity.Field) { f.Default = `["retired"]` },
		"duplicate default":  func(f *entity.Field) { f.Default = `["round","round"]` },
		"exclusive default":  func(f *entity.Field) { f.Default = `["round","none"]` },
		"truncated default": func(f *entity.Field) {
			f.Default = `["square","round"]`
			f.Choices.Maximum = &entity.ChoiceMaximum{Count: 1, Behaviour: "replace-oldest"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := base()
			change(&f)
			if err := entity.ValidateChoices(f); err == nil {
				t.Fatal("accepted invalid field")
			}
		})
	}
	f := base()
	f.Default = `["square","round"]`
	f.Choices.Maximum = &entity.ChoiceMaximum{Count: 2, Behaviour: "replace-oldest"}
	if err := entity.ValidateChoices(f); err != nil {
		t.Fatal(err)
	}
	for _, overlay := range []*entity.ChoiceFields{{Fields: []entity.ChoiceField{{Name: "missing", Choices: choiceSet()}}}, {Fields: []entity.ChoiceField{{Name: "shapes", Choices: choiceSet()}, {Name: "shapes", Choices: choiceSet()}}}} {
		if got, err := entity.WithChoices([]entity.Field{f}, overlay); err == nil || got != nil {
			t.Fatal("accepted ambiguous overlay")
		}
	}
}

func TestChoiceDeclarationBoundsAndMaximumPolicies(t *testing.T) {
	for name, change := range map[string]func(*entity.ChoiceSet){
		"negative threshold": func(c *entity.ChoiceSet) { c.SearchThreshold = -1 },
		"invalid group":      func(c *entity.ChoiceSet) { c.Options[0].Group = &entity.ChoiceGroup{Key: "UPPER", Label: "Shapes"} },
		"inconsistent group": func(c *entity.ChoiceSet) {
			c.Options[0].Group = &entity.ChoiceGroup{Key: "shapes", Label: "Shapes"}
			c.Options[1].Group = &entity.ChoiceGroup{Key: "shapes", Label: "Other"}
		},
		"decorated exclusive": func(c *entity.ChoiceSet) {
			c.Options[3].Picture = &entity.ChoicePicture{Kind: "colour", Value: "#112233"}
		},
		"maximum on one": func(c *entity.ChoiceSet) {
			c.Cardinality = "one"
			c.Maximum = &entity.ChoiceMaximum{Count: 1, Behaviour: "replace-oldest"}
		},
		"maximum too large":        func(c *entity.ChoiceSet) { c.Maximum = &entity.ChoiceMaximum{Count: 4, Behaviour: "replace-oldest"} },
		"missing maximum sentence": func(c *entity.ChoiceSet) { c.Maximum = &entity.ChoiceMaximum{Count: 1, Behaviour: "refuse"} },
		"unexpected maximum sentence": func(c *entity.ChoiceSet) {
			c.Maximum = &entity.ChoiceMaximum{Count: 1, Behaviour: "replace-oldest", Message: "No."}
		},
		"unknown maximum": func(c *entity.ChoiceSet) { c.Maximum = &entity.ChoiceMaximum{Count: 1, Behaviour: "other"} },
		"long label":      func(c *entity.ChoiceSet) { c.Options[0].Label = strings.Repeat("a", 161) },
	} {
		t.Run(name, func(t *testing.T) {
			c := choiceSet()
			change(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("accepted invalid declaration")
			}
		})
	}
	c := choiceSet()
	c.Options[0].Label = strings.Repeat("é", 160)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, cardinality := range []string{"one", "many"} {
		c.Cardinality = cardinality
		for _, bad := range []any{nil, 42, []any{"round", nil}} {
			if _, err := c.ChoiceIDs(bad); err == nil {
				t.Fatalf("%s accepted %v", cardinality, bad)
			}
		}
	}
}
