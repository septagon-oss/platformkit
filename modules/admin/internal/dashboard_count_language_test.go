package internal

import (
	"context"
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/modules/task"
)

func TestDashboardNumbersUseTheReadersLocale(t *testing.T) {
	catalog := xtext.Load("en", task.Catalogue())
	p := pages{resources: []httpx.Resource{{
		Module: "task", Entity: "task", Name: entity.Name{One: "Task", Other: "Tasks"},
		Screen: "/app/task/tasks", Count: func(context.Context) (int64, error) { return 12345, nil },
	}}}
	for _, tag := range []string{"en", "pt-PT"} {
		t.Run(tag, func(t *testing.T) {
			selected := catalog.Select(tag)
			view := p.dashboard(t.Context(), &selected)
			body := render(t, g.Group(view.Body))
			noun := selected.Plural("resource.task.task", 12345, "Task", "Tasks")
			want := selected.Text("dashboard_count_number", "%d", 12345) + " " + noun
			if !strings.Contains(body, ">"+want+"<") {
				t.Errorf("dashboard lacks localized count %q; unformatted count present=%t", want, strings.Contains(body, ">12345 "+noun+"<"))
			}
		})
	}
}
