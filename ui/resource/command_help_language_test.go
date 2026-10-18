package resource_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestCommandHelpUsesItsOwnTranslationAndFallsBackToItsDeclaration(t *testing.T) {
	for _, collection := range []bool{false, true} {
		for _, empty := range []bool{false, true} {
			name := "record"
			if collection {
				name = "collection"
			}
			if empty {
				name += "/empty-formatter"
			}
			t.Run(name, func(t *testing.T) {
				copy := spoken{words: map[string]string{
					"hints.line/line.command.assign.field.slug.label": "Nome do endereço",
					"hints.line/line.command.assign.field.slug.help":  "Ajuda para esta ação",
					"hints.line/line.field.slug.help":                 "Ajuda para o registo",
				}}
				want := "Ajuda para esta ação"
				if empty {
					copy.silent = []string{"hints.line/line.command.assign.field.slug.help"}
					want = "Declared command help"
				}
				r := lines()
				r.Commands = []resource.Command{{
					Verb: "assign", Label: "Assign", Base: r.Screen, Collection: collection,
					Fields: []entity.Field{{Name: "slug", Type: entity.TypeString,
						Doc:          "Developer documentation",
						Presentation: entity.FieldHints{Label: "Address", Help: "Declared command help"}}},
				}}
				o := resource.Options{Locale: speak("pt-PT", copy)}
				row := map[string]any{"id": "11111111-1111-1111-1111-111111111111", "title": "A line"}
				view := resource.Detail(r, o, row, true)
				if collection {
					view = resource.List(r, o, []map[string]any{row}, 1, 1, "", false)
				}
				body := render(t, view.Body)
				for _, text := range []string{`name="slug"`, "Nome do endereço", want} {
					if !strings.Contains(body, text) {
						t.Errorf("command form lacks %q: %s", text, body)
					}
				}
				for _, text := range []string{"Ajuda para o registo", "Developer documentation"} {
					if strings.Contains(body, text) {
						t.Errorf("command help used the wrong fallback or resource scope: %q", text)
					}
				}
				if !empty && strings.Contains(body, "Declared command help") {
					t.Error("translated command retains its English help")
				}
			})
		}
	}
}
