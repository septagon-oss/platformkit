package forms_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	picturecontracts "github.com/septagon-oss/platformkit/modules/picture/contracts"
	"github.com/septagon-oss/platformkit/ui/forms"
)

func choiceField(t *testing.T, appearance, cardinality string) forms.Field {
	t.Helper()
	c := &entity.ChoiceSet{Cardinality: cardinality, Empty: entity.ChoiceEmpty{Mode: "allow"}, Options: []entity.ChoiceOption{{ID: "round", Label: "Round", Picture: &entity.ChoicePicture{Kind: "colour", Value: "#112233"}}, {ID: "square", Label: "Square", Picture: &entity.ChoicePicture{Kind: "colour", Value: "#AABBCC"}}}}
	f := entity.Field{Name: "shapes", Type: entity.TypeString, Widget: appearance, Choices: c}
	if cardinality == "many" {
		f.Type, f.Elem = entity.TypeList, entity.TypeString
	}
	if appearance == "figure" {
		figure, err := picturecontracts.CompileFigure("fixture", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 80"><circle data-choice="round" cx="20" cy="20" r="10"/><circle data-choice="round" cx="20" cy="60" r="10"/><rect data-choice="square" x="50" y="10" width="20" height="20"/></svg>`, []string{"round", "square"})
		if err != nil {
			t.Fatal(err)
		}
		f.Presentation.Figure = figure
	}
	resolved := present.Resolve(present.Input{Schema: entity.Schema{Fields: []entity.Field{f}}})
	return forms.Field{Definition: f, Resolved: &resolved.Fields[0]}
}

func TestChoiceAppearancesKeepNativeInputsAndReadOnlyValues(t *testing.T) {
	for _, appearance := range []string{"tiles", "chips", "swatch", "figure"} {
		for _, cardinality := range []string{"one", "many"} {
			f := choiceField(t, appearance, cardinality)
			var out strings.Builder
			err := forms.Control(forms.ControlProps{Field: f, ID: "fixture", ChoiceValues: []string{"round"}}).Render(&out)
			if err != nil {
				t.Fatal(err)
			}
			html := out.String()
			kind := "radio"
			if cardinality == "many" {
				kind = "checkbox"
			}
			for _, want := range []string{`type="` + kind + `"`, `name="shapes"`, `data-choice-check`, `__pkit_choice_present[shapes]`, `1 selected`, `Round`} {
				if !strings.Contains(html, want) {
					t.Errorf("%s/%s missing %s: %s", appearance, cardinality, want, html)
				}
			}
			if cardinality == "many" && !strings.Contains(html, `__pkit_choice_order[shapes]`) {
				t.Fatal("no retained order")
			}
			out.Reset()
			if err := forms.Control(forms.ControlProps{Field: f, Immutable: true, ChoiceValues: []string{"round"}}).Render(&out); err != nil {
				t.Fatal(err)
			}
			for _, bad := range []string{"<input", "tabindex", "__pkit_choice_", "data-choice-config"} {
				if strings.Contains(out.String(), bad) {
					t.Fatalf("read-only %s has %s", appearance, bad)
				}
			}
			if appearance == "figure" && !strings.Contains(out.String(), `data-selected="true"`) {
				t.Fatal("read-only highlight absent")
			}
		}
	}
}

func TestChoiceFormRequiresResolvedMetadataAndScopesInputs(t *testing.T) {
	f := choiceField(t, "chips", "many")
	missing := f
	missing.Resolved = nil
	if _, err := forms.Example("missing", forms.Model{Fields: []forms.Field{missing}}, forms.Options{Namespace: "one"}); err == nil {
		t.Fatal("guessed missing policy")
	}
	for _, scope := range []string{"one", "two"} {
		ex, err := forms.Example(scope, forms.Model{Fields: []forms.Field{f}, ChoiceValues: map[string][]string{"shapes": {"round"}}}, forms.Options{Namespace: scope})
		if err != nil {
			t.Fatal(err)
		}
		var out strings.Builder
		if err := ex.Node.Render(&out); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), `id="`+scope+`-field-736861706573-option-726f756e64"`) {
			t.Fatal("choice input lost instance scope")
		}
	}
}

func TestChoiceSearchAndRetiredIDRemainNative(t *testing.T) {
	f := choiceField(t, "figure", "many")
	f.Definition.Choices.SearchThreshold = 1
	r := present.Resolve(present.Input{Schema: entity.Schema{Fields: []entity.Field{f.Definition}}})
	f.Resolved = &r.Fields[0]
	var out strings.Builder
	if err := forms.Control(forms.ControlProps{Field: f, ID: "fixture", ChoiceValues: []string{"retired"}}).Render(&out); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, node := range elements(t, out.String()) {
		if node.Data == "input" && attribute(node, "value") == "retired" {
			for _, a := range node.Attr {
				if a.Key == "checked" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("retired choice is not checked for native removal")
	}
	for _, want := range []string{`data-choice-search-label`, `hidden`, `data-choice-stale="true"`, `retired — Unavailable choice`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %s: %s", want, out.String())
		}
	}
}

func TestChoiceSummaryKeepsFourPicturesAndEverySelectedLabel(t *testing.T) {
	f := choiceField(t, "chips", "many")
	ids := []string{"round", "square", "triangle", "line", "dot", "ring"}
	for _, id := range ids[2:] {
		f.Definition.Choices.Options = append(f.Definition.Choices.Options, entity.ChoiceOption{ID: id, Label: id, Picture: &entity.ChoicePicture{Kind: "colour", Value: "#112233"}})
	}
	resolved := present.Resolve(present.Input{Schema: entity.Schema{Fields: []entity.Field{f.Definition}}})
	f.Resolved = &resolved.Fields[0]
	var out strings.Builder
	if err := forms.Control(forms.ControlProps{Field: f, ID: "summary", ChoiceValues: ids}).Render(&out); err != nil {
		t.Fatal(err)
	}
	pictures := 0
	for _, node := range elements(t, out.String()) {
		for _, a := range node.Attr {
			if a.Key == "data-choice-stack" {
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					pictures++
				}
			}
		}
	}
	if pictures != 4 || !strings.Contains(out.String(), "6 selected") || !strings.Contains(out.String(), "Round, Square, triangle, line, dot, ring") {
		t.Fatalf("summary: %d pictures, %s", pictures, out.String())
	}
}

func TestReadOnlyChoiceLocalizesRetiredValues(t *testing.T) {
	f := choiceField(t, "chips", "many")
	var out strings.Builder
	word := func(key, fallback string) string {
		if key == "forms.choice.unavailable" {
			return "Opção indisponível"
		}
		return fallback
	}
	if err := forms.Control(forms.ControlProps{Field: f, Immutable: true, ChoiceValues: []string{"retired"}, Text: word}).Render(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "retired — Opção indisponível") || strings.Contains(out.String(), "Unavailable choice") {
		t.Fatal(out.String())
	}
}
