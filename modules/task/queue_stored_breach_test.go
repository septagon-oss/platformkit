package task_test

import (
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestTaskQueueReadsTheStoredBreachInsteadOfTheDeadline(t *testing.T) {
	t.Parallel()
	queue := resource.Resource{Schema: task.Spec.Schema(), Present: task.Spec.Present,
		Immutable: task.Spec.Immutable, Screen: "/app/task/tasks"}
	for _, tc := range []struct {
		name, deadline, status, want string
		breached                     bool
	}{
		{"past deadline awaiting a check", "2001-01-01T00:00:00Z", "open", "No", false},
		{"future deadline with stored breach", "2100-01-01T00:00:00Z", "open", "Yes", true},
		{"resolved task keeps its breach", "2001-01-01T00:00:00Z", "resolved", "Yes", true},
		{"no response deadline", "", "open", "No", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := map[string]any{"id": "stored", "title": tc.name, "priority": "normal",
				"status": tc.status, "dueAt": tc.deadline, "slaDeadline": tc.deadline, "slaBreached": tc.breached}
			opts := resource.Options{Workspace: "/app", Home: "Dashboard"}
			for surface, nodes := range map[string][]g.Node{
				"queue":  resource.List(queue, opts, []map[string]any{row}, 1, 1, "", false).Body,
				"detail": resource.Detail(queue, opts, row, false).Body,
			} {
				var out strings.Builder
				if err := g.Group(nodes).Render(&out); err != nil {
					t.Fatal(err)
				}
				doc, err := html.Parse(strings.NewReader(out.String()))
				if err != nil {
					t.Fatal(err)
				}
				var header, cells []string
				got := ""
				for n := range doc.Descendants() {
					if n.Type != html.ElementNode {
						continue
					}
					if n.Data == "tr" {
						if attr(n, "data-pk-row") == "stored" {
							cells = childTexts(n, "td")
						} else if labels := childTexts(n, "th"); len(labels) > 0 {
							header = labels
						}
					}
					if n.Data == "dt" && strings.TrimSpace(text(n)) == "Response overdue" {
						for next := n.NextSibling; next != nil; next = next.NextSibling {
							if next.Type == html.ElementNode && next.Data == "dd" {
								got = strings.TrimSpace(text(next))
								break
							}
						}
					}
				}
				if surface == "queue" {
					column := slices.Index(header, "Response overdue")
					if column >= 0 && column < len(cells) {
						got = cells[column]
					}
				}
				if got != tc.want {
					t.Errorf("%s reads Response overdue %q, want stored value %q", surface, got, tc.want)
				}
			}
		})
	}
}
