package present_test

import (
	"fmt"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
)

func TestChoicePolicyResolvesBothAxesAndCommandInputs(t *testing.T) {
	c := &entity.ChoiceSet{Cardinality: "many", Empty: entity.ChoiceEmpty{Mode: "allow"}, Options: []entity.ChoiceOption{{ID: "round", Label: "Round"}, {ID: "square", Label: "Square"}}, SearchThreshold: 1}
	for _, hint := range []string{"", "tiles", "chips", "figure", "swatch"} {
		f := entity.Field{Name: "shapes", Type: entity.TypeList, Elem: entity.TypeString, Choices: c, Widget: hint}
		in := present.Input{Schema: entity.Schema{Fields: []entity.Field{f}}, Commands: []present.CommandInput{{Verb: "pick", Fields: []entity.Field{f}}}}
		resolved := present.Resolve(in)
		field := resolved.Fields[0]
		want := hint
		if want == "" {
			want = "chips"
		}
		if field.Control != want || field.Kind != "choice" || !field.Choice.SearchEnabled || field.Options[0].Value != "round" {
			t.Fatalf("policy: %+v", field)
		}
		arg := resolved.Commands[0].Fields[0]
		if !arg.Editable.Create || arg.Editable.Update || arg.OnRecord || arg.OnList {
			t.Fatalf("command policy: %+v", arg)
		}
		field.Options[0].Label = "Changed"
		field.Choice.Empty.Mode = "refuse"
		if c.Options[0].Label != "Round" || c.Empty.Mode != "allow" {
			t.Fatal("resolver changed declaration")
		}
	}
}

func TestChoiceSearchStartsAboveTheEffectiveThreshold(t *testing.T) {
	for _, tc := range []struct {
		threshold, count, effective int
		search                      bool
	}{{0, 12, 12, false}, {0, 13, 12, true}, {2, 2, 2, false}, {2, 3, 2, true}} {
		c := &entity.ChoiceSet{Cardinality: "many", Empty: entity.ChoiceEmpty{Mode: "allow"}, SearchThreshold: tc.threshold}
		for i := range tc.count {
			c.Options = append(c.Options, entity.ChoiceOption{ID: fmt.Sprintf("shape-%d", i), Label: fmt.Sprintf("Shape %d", i)})
		}
		r := present.Resolve(present.Input{Schema: entity.Schema{Fields: []entity.Field{{Name: "shapes", Type: entity.TypeList, Elem: entity.TypeString, Choices: c}}}})
		if choice := r.Fields[0].Choice; choice.SearchThreshold != tc.effective || choice.SearchEnabled != tc.search {
			t.Fatalf("threshold %d, count %d: %+v", tc.threshold, tc.count, choice)
		}
	}
}
