package admin_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/modules/admin"
	"github.com/septagon-oss/platformkit/modules/content"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	"github.com/septagon-oss/platformkit/ui/resource"
	g "maragu.dev/gomponents"
)

func TestContentFormSpeaksPortugueseInItsLabelsHelpAndChoices(t *testing.T) {
	catalog := xtext.Load("en", admin.Catalogue(), content.Catalogue(),
		xtext.Source{Name: "resource", FS: resource.Catalogues()})
	loc := catalog.Select("pt-PT")
	if loc.Language != "pt-PT" {
		t.Fatalf("selected language = %q, want pt-PT", loc.Language)
	}
	r := resource.Resource{
		Schema: entity.Schema{Module: "content", Entity: "content", Fields: crud.FieldsOf(reflect.TypeFor[contentcontracts.Content]())},
		Name:   entity.Name{One: "Content item", Other: "Content items"}, Screen: "/app/content/contents",
	}
	view := resource.Form(r, resource.Options{Locale: &loc}, r.Screen, "Criar conteúdo", nil, nil, "", true)
	var body strings.Builder
	if err := g.Group(view.Body).Render(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body.String(), `name="title"`) || !strings.Contains(body.String(), `name="kind"`) {
		t.Fatal("the content form did not render its title and kind controls")
	}
	for _, english := range []string{">Title<", "Headline", "URL name, unique within the tenant", "The content itself, in Markdown", "Lifecycle state", ">Published<", ">Archived<"} {
		if strings.Contains(body.String(), english) {
			t.Errorf("Portuguese content form retains English interface copy %q", english)
		}
	}
}
