package resource_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestCommandControlsExplainTheirInputInTheRequestsLanguage(t *testing.T) {
	catalog := xtext.Load("en", xtext.Source{Name: "resource", FS: resource.Catalogues()})
	for _, tag := range []string{"en", "pt-PT"} {
		for _, tc := range []struct {
			name, key, english string
			field              entity.Field
		}{
			{"list", "screens.comma_separated", "Comma separated.", entity.Field{Name: "fields", Type: entity.TypeList}},
			{"reference", "screens.related_record", "", entity.Field{Name: "target", Type: entity.TypeString, Widget: "entity-picker"}},
		} {
			t.Run(tag+"/"+tc.name, func(t *testing.T) {
				selected := catalog.Select(tag)
				r := note()
				r.Commands = []resource.Command{{Verb: "choose", Label: "Choose", Base: r.Schema.Path, Fields: []entity.Field{tc.field}}}
				o := opts
				o.Locale = &selected
				body := render(t, resource.Detail(r, o, map[string]any{"id": "1", "title": "Example"}, true).Body)
				want := selected.Text(tc.key, tc.english)
				if want == "" || !strings.Contains(body, want) {
					t.Errorf("command control lacks its %s note %q", tag, want)
				}
				if tag == "pt-PT" && strings.Contains(body, "Comma separated.") {
					t.Error("Portuguese command retains the English list note")
				}
			})
		}
	}
}
