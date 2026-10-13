package admin_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/ui/resource"
	g "maragu.dev/gomponents"
)

func TestSelectPlaceholdersUseTheRequestsCatalogue(t *testing.T) {
	copy := xtext.Load("en", task.Catalogue(), xtext.Source{Name: "resource", FS: resource.Catalogues()})
	selected := copy.Select("pt-PT")
	want := selected.Text("screens.choose", "Choose…")
	if selected.Language != "pt-PT" || want == "Choose…" {
		t.Fatal("the fixture must carry the Portuguese select placeholder")
	}
	options := resource.Options{Locale: &selected, Module: "task", Entity: "task"}
	for _, defaultValue := range []string{"", "normal"} {
		field := entity.Field{Name: "priority", Type: entity.TypeText, Widget: "select",
			Enum: []string{"normal", "high"}, Default: defaultValue}
		r := resource.Resource{Schema: entity.Schema{Module: "task", Entity: "task", Fields: []entity.Field{field}},
			Name: entity.Name{One: "Task", Other: "Tasks"}, Screen: "/app/task/tasks"}
		for _, control := range []struct {
			name string
			node g.Node
		}{
			{"control", options.Control(field, "", "", false)},
			{"form", g.Group(resource.Form(r, options, r.Screen, "", nil, nil, "", true).Body)},
		} {
			t.Run(control.name+"/default="+defaultValue, func(t *testing.T) {
				var html strings.Builder
				if err := control.node.Render(&html); err != nil {
					t.Fatal(err)
				}
				body := html.String()
				if !strings.Contains(body, ">"+want+"</option>") {
					t.Errorf("Portuguese select lacks catalogue placeholder %q: %s", want, body)
				}
				if strings.Contains(body, "Choose a ") || strings.Contains(body, "Choose…") {
					t.Error("Portuguese select retains an English placeholder")
				}
			})
		}
	}
}
