package task_test

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestTaskQueueSortControlsCarryTheNextDueOrder(t *testing.T) {
	t.Parallel()
	queue := resource.Resource{Schema: task.Spec.Schema(), Present: task.Spec.Present,
		Immutable: task.Spec.Immutable, Screen: "/app/task/tasks"}
	rows := []map[string]any{{"id": "overdue", "title": "Inspect ventilation",
		"status": "open", "priority": "high", "slaBreached": true}}
	for _, tc := range []struct{ name, current, next string }{
		{"default order", "", "dueAt"},
		{"ascending due", "dueAt", "-dueAt"},
		{"descending due", "-dueAt", "dueAt"},
		{"another field", "title", "dueAt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := resource.List(queue, resource.Options{}, rows, 1, 1, tc.current, false)
			var out strings.Builder
			if err := g.Group(view.Body).Render(&out); err != nil {
				t.Fatal(err)
			}
			doc, err := html.Parse(strings.NewReader(out.String()))
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for node := range doc.Descendants() {
				if node.Type != html.ElementNode || attr(node, "data-pk-sort") != "dueAt" {
					continue
				}
				found = true
				want := queue.Screen + "?sort=" + tc.next
				if node.Data != "a" || attr(node, "href") != want {
					t.Errorf("every Due control must navigate without JavaScript to %q, got <%s href=%q>", want, node.Data, attr(node, "href"))
				}
				if got := attr(node, "hx-get"); got != want {
					t.Errorf("enhanced Due navigation = %q, want the same next order %q", got, want)
				}
			}
			if !found {
				t.Fatal("the task queue offers no Due ordering control")
			}
		})
	}
}
