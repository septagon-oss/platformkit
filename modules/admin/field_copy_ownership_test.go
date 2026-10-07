package admin_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/modules/admin"
	content "github.com/septagon-oss/platformkit/modules/content"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	"github.com/septagon-oss/platformkit/modules/task"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/ui/resource"
	g "maragu.dev/gomponents"
)

// A field's words belong to the entity that defines the field, which means they may
// only ever be printed on that entity's screens. `field.title` is one key in one
// catalogue and four modules define a title, so filling that key in for the task put
// "Short summary of the task" above content's own "Headline" — in English, because a
// catalogue answers a key it holds whatever sentence the caller brought — and put the
// task's Portuguese on a Portuguese content form. The case that caught it was written
// while curing the missing task copy; it is kept because the failure is invisible
// from either module's own test: each one sees a page that looks right.
func TestOneModulesFieldCopySpeaksForItsOwnEntity(t *testing.T) {
	catalog := xtext.Load("en", admin.Catalogue(), task.Catalogue(), content.Catalogue(),
		xtext.Source{Name: "resource", FS: resource.Catalogues()})
	for _, tag := range []string{"en", "pt-PT"} {
		loc := catalog.Select(tag)
		drawn := func(module, entityName string, typ reflect.Type) string {
			t.Helper()
			r := resource.Resource{
				Schema: entity.Schema{Module: module, Entity: entityName, Fields: crud.FieldsOf(typ)},
				Name:   entity.Name{One: entityName, Other: entityName + "s"},
				Screen: "/app/" + module + "/" + entityName,
			}
			v := resource.Form(r, resource.Options{Locale: &loc}, r.Screen, "New", nil, nil, "", true)
			var body strings.Builder
			if err := g.Group(v.Body).Render(&body); err != nil {
				t.Fatal(err)
			}
			return body.String()
		}
		taskForm := drawn("task", "task", reflect.TypeFor[taskcontracts.Task]())
		contentForm := drawn("content", "content", reflect.TypeFor[contentcontracts.Content]())
		// Each entity's own note, in the language of the form being drawn: a form that
		// speaks Portuguese is expected to carry the Portuguese sentence its own module
		// wrote, not the English one still sitting in its schema's doc comment.
		taskNote := map[string]string{"en": "Short summary of the task", "pt-PT": "Resumo da tarefa"}[tag]
		contentNote := map[string]string{"en": "Headline", "pt-PT": "O que uma lista e um cabeçalho mostram"}[tag]
		if !strings.Contains(taskForm, taskNote) {
			t.Errorf("%s task form does not carry the task's own note %q", tag, taskNote)
		}
		if !strings.Contains(contentForm, contentNote) {
			t.Errorf("%s content form lost the note its own module wrote, %q", tag, contentNote)
		}
		for _, borrowed := range []string{"Short summary of the task", "Resumo da tarefa", "Detailed description of the task"} {
			if strings.Contains(contentForm, borrowed) {
				t.Errorf("%s content form prints the task's copy %q", tag, borrowed)
			}
		}
		for _, borrowed := range []string{"Headline", "O que uma lista e um cabeçalho mostram"} {
			if strings.Contains(taskForm, borrowed) {
				t.Errorf("%s task form prints content's copy %q", tag, borrowed)
			}
		}
	}
}
