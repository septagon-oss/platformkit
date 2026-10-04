package admin_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/modules/admin"
	"github.com/septagon-oss/platformkit/ui/resource"
	g "maragu.dev/gomponents"
)

func TestGeneratedNumericValuesUseTheRequestLocale(t *testing.T) {
	catalog := xtext.Load("en", admin.Catalogue(), xtext.Source{Name: "resource", FS: resource.Catalogues()})
	r := resource.Resource{
		Schema: entity.Schema{Module: "note", Entity: "note", Fields: []entity.Field{
			{Name: "rank", Type: entity.TypeFloat},
		}},
		Name: entity.Name{One: "Note", Other: "Notes"}, Screen: "/app/note/notes",
	}
	for _, tag := range []string{"en", "pt-PT"} {
		t.Run(tag, func(t *testing.T) {
			loc := catalog.Select(tag)
			want := loc.Text("number", "%g", 12345.5)
			view := resource.Detail(r, resource.Options{Locale: &loc}, map[string]any{"id": "1", "rank": 12345.5}, false)
			var body strings.Builder
			if err := g.Group(view.Body).Render(&body); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(body.String(), ">"+want+"<") {
				t.Errorf("%s numeric detail does not use x/text's %q; raw 12345.5 present=%t", tag, want, strings.Contains(body.String(), ">12345.5<"))
			}
		})
	}
}
