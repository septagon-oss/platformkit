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

func TestKernelTaskFormTranslatesItsFieldsHelpAndOptions(t *testing.T) {
	catalog := xtext.Load("en", admin.Catalogue(), task.Catalogue(), xtext.Source{Name: "resource", FS: resource.Catalogues()})
	loc := catalog.Select("pt-PT")
	r := resource.Resource{
		Schema: entity.Schema{Module: "task", Entity: "task", Fields: crud.FieldsOf(reflect.TypeFor[taskcontracts.Task]())},
		Name:   entity.Name{One: "Task", Other: "Tasks"}, Screen: "/app/task/tasks",
	}
	view := resource.Form(r, resource.Options{Locale: &loc}, "/app/task/tasks", "Criar tarefa", nil, nil, "", true)
	var body strings.Builder
	if err := g.Group(view.Body).Render(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body.String(), `name="title"`) {
		t.Fatal("the task form did not render its title control")
	}
	for _, english := range []string{">Title<", "Short summary of the task", "Lifecycle state", "Task priority", ">Critical<"} {
		if strings.Contains(body.String(), english) {
			t.Errorf("Portuguese task form retains English copy %q", english)
		}
	}
}
