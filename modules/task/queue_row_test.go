package task_test

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// The queue renders through the module's own declaration, so the column is
// there because task.Spec says so, and the cell under it reads the stored fact.
func TestAQueueRowSaysWhenTheResponseIsOverdue(t *testing.T) {
	t.Parallel()
	queue := resource.Resource{Schema: task.Spec.Schema(), Present: task.Spec.Present,
		Immutable: task.Spec.Immutable, Screen: "/app/task/tasks"}
	rows := []map[string]any{
		{"id": "breached", "title": "Lift stopped", "status": "open", "priority": "critical", "slaBreached": true},
		{"id": "kept", "title": "Bulb out", "status": "open", "priority": "low", "slaBreached": false},
	}
	var b strings.Builder
	if err := g.Group(resource.List(queue, resource.Options{Workspace: "/app", Home: "Dashboard"}, rows, 2, 1, "", true).Body).Render(&b); err != nil {
		t.Fatal(err)
	}
	doc, err := html.Parse(strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	var header []string
	cells := map[string][]string{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			if id := attr(n, "data-pk-row"); id != "" {
				cells[id] = childTexts(n, "td")
			} else if th := childTexts(n, "th"); len(th) > 0 {
				header = th
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	column := -1
	for i, label := range header {
		if label == "Response overdue" {
			column = i
		}
	}
	if column < 0 {
		t.Fatalf("the queue has no Response overdue column: header %q", header)
	}
	for id, want := range map[string]string{"breached": "Yes", "kept": "No"} {
		if row := cells[id]; len(row) <= column || row[column] != want {
			t.Fatalf("row %s reads %q under Response overdue, want %q (header %q)", id, row, want, header)
		}
	}
	if kept := strings.Join(cells["kept"], " "); strings.Contains(kept, "Yes") {
		t.Fatalf("a row whose response is not overdue says Yes: %q", kept)
	}
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// childTexts is the trimmed text of each direct child element named tag.
func childTexts(n *html.Node, tag string) []string {
	var out []string
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == tag {
			out = append(out, strings.TrimSpace(text(c)))
		}
	}
	return out
}

func text(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(text(c))
	}
	return b.String()
}
