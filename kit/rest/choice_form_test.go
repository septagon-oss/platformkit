package rest

import (
	"net/url"
	"reflect"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
)

func formChoices() []entity.Field {
	c := &entity.ChoiceSet{Cardinality: "many", Empty: entity.ChoiceEmpty{Mode: "allow"}, Options: []entity.ChoiceOption{{ID: "round", Label: "Round"}, {ID: "square", Label: "Square"}, {ID: "triangle", Label: "Triangle"}, {ID: "none", Label: "No preference"}}, NoPreference: "none", Maximum: &entity.ChoiceMaximum{Count: 2, Behaviour: "replace-oldest"}}
	return []entity.Field{{Name: "shapes", Type: entity.TypeList, Elem: entity.TypeString, Choices: c}}
}

func TestNativeChoiceOrderAndPresence(t *testing.T) {
	for _, tc := range []struct {
		values url.Values
		want   []string
		absent bool
	}{
		{url.Values{"shapes": {"round", "square"}}, []string{"round", "square"}, false},
		{url.Values{"shapes": {"round", "triangle"}, "__pkit_choice_order[shapes]": {`["square","round"]`}}, []string{"round", "triangle"}, false},
		{url.Values{"shapes": {"round", "triangle"}, "__pkit_choice_order[shapes]": {`["triangle","round"]`}}, []string{"triangle", "round"}, false},
		{url.Values{"shapes": {"round", "none"}}, []string{"none"}, false},
		{url.Values{"__pkit_choice_present[shapes]": {"1"}}, []string{}, false},
		{url.Values{}, nil, true},
	} {
		got, err := UpdateValues([]byte(tc.values.Encode()), formChoices(), nil)
		if err != nil {
			t.Fatal(err)
		}
		ids, found := got["shapes"].([]string)
		if found == tc.absent || !slices.Equal(ids, tc.want) {
			t.Fatalf("%v -> %v", tc.values, got)
		}
	}
}

func TestNativeChoiceTransportRefusesForgedMarkers(t *testing.T) {
	for _, values := range []url.Values{
		{"__pkit_choice_present[missing]": {"1"}},
		{"__pkit_choice_present[shapes]": {"0"}},
		{"__pkit_choice_present[shapes]": {"1", "1"}},
		{"__pkit_choice_order[shapes]": {"[]"}},
		{"shapes": {"round"}, "__pkit_choice_order[shapes]": {"null"}},
		{"shapes": {"round", "round"}},
		{"shapes": {"none", "missing"}},
		{"shapes": {"round,square"}},
	} {
		if got, err := UpdateValues([]byte(values.Encode()), formChoices(), nil); err == nil || got != nil {
			t.Fatalf("accepted %v: %v, %v", values, got, err)
		}
	}
	body := []byte(url.Values{"__pkit_choice_present[shapes]": {"1"}}.Encode())
	if _, err := UpdateValues(body, formChoices(), []string{"shapes"}); err == nil {
		t.Fatal("accepted immutable marker")
	}
}

func TestChoiceCommandUsesTheSameAnswerDecision(t *testing.T) {
	type input struct {
		Shapes []string `json:"shapes"`
	}
	fields := entity.FieldsOf(reflect.TypeFor[input]())
	fields[0].Choices = formChoices()[0].Choices
	fields[0].Choices.Empty = entity.ChoiceEmpty{Mode: "refuse", Message: "Choose a shape."}
	prepare, fault, err := commandDefaults[input](fields)
	if err != nil || fault == "" {
		t.Fatalf("declaration: %s, %v", fault, err)
	}
	if _, err := prepare(nil); err == nil {
		t.Fatal("accepted empty answer")
	}
	got, err := prepare(map[string]any{"shapes": []string{"round", "square", "triangle"}})
	if err != nil || !slices.Equal(got.Shapes, []string{"square", "triangle"}) {
		t.Fatalf("normalization: %+v, %v", got, err)
	}
	if _, err := prepare(map[string]any{"shapes": nil}); err == nil {
		t.Fatal("accepted null")
	}
}

func TestChoiceRequiredDraftDoesNotHideInvalidTypedDefaults(t *testing.T) {
	type input struct {
		Shapes []string `json:"shapes"`
		Count  int8     `json:"count" default:"128"`
	}
	fields := entity.FieldsOf(reflect.TypeFor[input]())
	fields[0].Choices = &entity.ChoiceSet{Cardinality: "many", Options: []entity.ChoiceOption{{ID: "round", Label: "Round"}}, Empty: entity.ChoiceEmpty{Mode: "refuse", Message: "Keep one."}}
	if _, _, err := commandDefaults[input](fields); err == nil {
		t.Fatal("required choice hid an overflowing typed default")
	}
}
