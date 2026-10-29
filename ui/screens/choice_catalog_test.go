package screens_test

import (
	"encoding/json"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	picturecontracts "github.com/septagon-oss/platformkit/modules/picture/contracts"
	"github.com/septagon-oss/platformkit/ui/screens"
	"os"
	"strings"
	"testing"
)

func choiceCatalogField(appearance string) entity.Field {
	c := &entity.ChoiceSet{Cardinality: "many", Empty: entity.ChoiceEmpty{Mode: "refuse", Message: "Keep one shape."}, Options: []entity.ChoiceOption{{ID: "round", Label: "Round", Picture: &entity.ChoicePicture{Kind: "colour", Value: "#112233"}}, {ID: "square", Label: "Square", Picture: &entity.ChoicePicture{Kind: "colour", Value: "#AABBCC"}, Group: &entity.ChoiceGroup{Key: "corners", Label: "Corners"}}, {ID: "none", Label: "No preference"}}, NoPreference: "none", SearchThreshold: 2, Maximum: &entity.ChoiceMaximum{Count: 2, Behaviour: "replace-oldest"}}
	f := entity.Field{Name: "shapes", Type: entity.TypeList, Elem: entity.TypeString, Widget: appearance, Choices: c}
	if appearance == "figure" {
		// A figure reaches the field compiled by the owner that drew it, the way a
		// product's composition hands one to entity.WithChoices.
		figure, err := picturecontracts.CompileFigure("fixture", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 80"><circle data-choice="round" cx="20" cy="20" r="10"/><circle data-choice="round" cx="20" cy="60" r="10"/><rect data-choice="square" x="60" y="20" width="20" height="40"/></svg>`, []string{"round", "square"})
		if err != nil {
			panic(err)
		}
		f.Presentation.Figure = figure
	}
	return f
}

func TestChoiceCatalogGoldenAndGeneratedForm(t *testing.T) {
	catalog := screens.Catalog{Version: screens.CatalogVersion}
	for _, appearance := range []string{"tiles", "chips", "swatch", "figure"} {
		r := resource()
		f := choiceCatalogField(appearance)
		r.Schema.Fields = []entity.Field{f}
		r.Commands = []httpx.Command{{Verb: "choose", Summary: "Choose shapes", Fields: []entity.Field{f}}}
		entry := screens.Describe1(r, true)
		catalog.Resources = append(catalog.Resources, entry)
		html := renderView(screens.Form(r, opts, "/fixture/"+appearance, "Choose shapes", map[string]any{"shapes": []string{"square", "round"}}, nil, "", false))
		for _, want := range []string{`data-choice-surface="` + appearance + `"`, `2 selected`, `Square, Round`, `type="checkbox"`} {
			if !strings.Contains(html, want) {
				t.Fatalf("%s missing %s: %s", appearance, want, html)
			}
		}
		cmd := entry.Resolved.Commands[0].Fields[0]
		if !cmd.Editable.Create || cmd.Editable.Update || cmd.OnList || cmd.OnRecord {
			t.Fatalf("command field: %+v", cmd)
		}
	}
	got, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const path = "testdata/catalog-choices.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(got) {
		t.Fatal("choice catalogue changed; inspect and regenerate with UPDATE_GOLDEN=1")
	}
}
