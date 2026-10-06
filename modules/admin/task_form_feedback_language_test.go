package admin_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/modules/admin"
	"github.com/septagon-oss/platformkit/modules/task"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/ui/resource"
	g "maragu.dev/gomponents"
)

func TestTaskFormFeedbackUsesTheRequestsLanguage(t *testing.T) {
	catalog := xtext.Load("en", admin.Catalogue(), task.Catalogue(),
		xtext.Source{Name: "resource", FS: resource.Catalogues()})
	loc := catalog.Select("pt-PT")
	r := resource.Resource{
		Schema: entity.Schema{Module: "task", Entity: "task", Fields: crud.FieldsOf(reflect.TypeFor[taskcontracts.Task]())},
		Name:   entity.Name{One: "Task", Other: "Tasks"}, Screen: "/app/task/tasks",
		Immutable: []string{"assigneeId", "slaBreached", "resolvedAt", "resolution"},
	}
	view := resource.Form(r, resource.Options{Locale: &loc}, r.Screen+"/one", "Editar tarefa",
		map[string]any{"title": "Tarefa", "status": "open"}, nil, "O título é obrigatório.", false)
	var body strings.Builder
	if err := g.Group(view.Body).Render(&body); err != nil {
		t.Fatal(err)
	}
	if loc.Language != "pt-PT" || !strings.Contains(body.String(), `name="assigneeId"`) || !strings.Contains(body.String(), "O título é obrigatório.") {
		t.Fatal("the Portuguese edit form did not render its assignee control and submitted refusal")
	}
	for _, english := range []string{
		"That could not be saved",
		"Changed by a command of its own, not by this form.",
		"The identifier of the related record. There is no picker for it yet.",
	} {
		if strings.Contains(body.String(), english) {
			t.Errorf("Portuguese task edit form retains English feedback %q", english)
		}
	}
}
