package components_test

import (
	"reflect"
	"strings"
	"testing"

	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/document"
	g "maragu.dev/gomponents"
)

func listFixture() c.DataListProps {
	return c.DataListProps{
		ComponentProps: c.ComponentProps{ID: "items"}, Label: "Items", ResultKey: "page-one",
		Columns: []c.TableColumn{{Key: "title", Label: "Title", Primary: true}},
		Groups: []c.DataGroup{{Key: "open", Title: "Open", CountText: "10 items", Rows: []c.DataRow{
			{TableRow: c.TableRow{ID: "a", Cells: map[string]any{"title": "Alpha"}}, Selectable: true, SelectionLabel: "Select Alpha", Href: "/items/a"},
			{TableRow: c.TableRow{ID: "b", Cells: map[string]any{"title": "Beta"}}, Selectable: true, SelectionLabel: "Select Beta"},
			{TableRow: c.TableRow{ID: "c", Cells: map[string]any{"title": "Gamma"}}, SelectionLabel: "Select Gamma", StatusLabel: "Unavailable"},
		}}},
		SelectionName: "selected", FormID: "bulk", SelectAllLabel: "Select all items",
		ClearSelectionLabel: "Clear selection", SelectionCountLabels: []string{"None selected", "One selected", "Two selected"},
	}
}

func TestDataListNativeSelectionAndSuppliedCounts(t *testing.T) {
	p := listFixture()
	p.SelectedIDs = []string{"a", "c", "stale"}
	before := append([]string(nil), p.SelectedIDs...)
	out := html(t, c.DataList(p))
	for _, want := range []string{`name="selected"`, `form="bulk"`, `value="a"`, `value="b"`, `value="c"`, `href="/items/a"`, "10 items", "One selected", `data-result-key="page-one"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("P4/P6: missing %s: %s", want, out)
		}
	}
	if strings.Count(out, " checked") != 1 || strings.Contains(out, `value="stale"`) || !reflect.DeepEqual(before, p.SelectedIDs) {
		t.Fatalf("P4: selection must intersect eligible rows without mutating inputs: %s", out)
	}
	if !strings.Contains(out, `data-component="selection-control" hidden`) {
		t.Fatal("select-all must be hidden until its enhancement runs")
	}
}

func TestDataListRefusesMalformedAndStalePayloads(t *testing.T) {
	for name, change := range map[string]func(*c.DataListProps){
		"label":              func(p *c.DataListProps) { p.Label = "" },
		"duplicate rows":     func(p *c.DataListProps) { p.Groups[0].Rows[1].ID = "a" },
		"duplicate groups":   func(p *c.DataListProps) { p.Groups = append(p.Groups, p.Groups[0]) },
		"missing row label":  func(p *c.DataListProps) { p.Groups[0].Rows[0].SelectionLabel = "" },
		"missing count copy": func(p *c.DataListProps) { p.SelectionCountLabels = []string{"None"} },
		"missing result key": func(p *c.DataListProps) { p.ResultKey = "" },
		"unknown state":      func(p *c.DataListProps) { p.State.Status = "unknown" },
		"failed payload": func(p *c.DataListProps) {
			p.State = c.ContentState{Status: c.MediaFailed, Title: "Error", Text: "Cannot read"}
		},
		"refused payload": func(p *c.DataListProps) {
			p.State = c.ContentState{Status: c.MediaRefused, Title: "Refused", Text: "No access"}
		},
		"missing choice href": func(p *c.DataListProps) { p.Filters = []c.ChoiceLink{{Key: "filter", Label: "Filter"}} },
		"incomplete removal": func(p *c.DataListProps) {
			p.Filters = []c.ChoiceLink{{Key: "filter", Label: "Filter", Href: "/items", RemoveHref: "/items"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := listFixture()
			change(&p)
			if p.Validate() == nil {
				t.Fatal("P1/P2: invalid composition admitted")
			}
			var raw strings.Builder
			if err := c.DataList(p).Render(&raw); err == nil || raw.Len() != 0 {
				t.Fatalf("P1/P2: invalid composition leaked bytes: %q, %v", raw.String(), err)
			}
			if out, err := document.Render(c.DataList(p)); err == nil || out != nil {
				t.Fatalf("P1/P2: document leaked rows: %s, %v", out, err)
			}
		})
	}
}

func TestDataListClearedStatesDoNotInvokeDataSlots(t *testing.T) {
	for _, status := range []c.MediaStatus{c.MediaLoading, c.MediaEmpty, c.MediaFailed, c.MediaRefused} {
		p := c.DataListProps{Label: "Items", State: c.ContentState{Status: status, Title: "State", Text: "State details", LoadingLabel: "Loading"}}
		slots := c.DataListSlots{
			TableSlots:  c.TableSlots{Cell: func(c.TableRow, c.TableColumn) g.Node { t.Fatal("P2: data slot invoked in absent state"); return nil }},
			EmptyAction: []g.Node{g.Text("create")}, RetryAction: []g.Node{g.Text("retry")},
			BulkActions: []g.Node{g.Text("protected actions")},
		}
		if status == c.MediaFailed || status == c.MediaRefused {
			var rejected strings.Builder
			if err := c.DataListWithSlots(p, slots).Render(&rejected); err == nil || rejected.Len() != 0 {
				t.Fatalf("P2: %s state retained stale slots: %q, %v", status, rejected.String(), err)
			}
			slots = c.DataListSlots{}
			if status == c.MediaFailed {
				// The failed read keeps its own recovery control and nothing else.
				slots.RetryAction = []g.Node{g.Text("retry")}
			}
		}
		out := html(t, c.DataListWithSlots(p, slots))
		if strings.Contains(out, "protected actions") || (status == c.MediaRefused && strings.Contains(out, "retry")) {
			t.Fatalf("P2: absent state retained controls: %s", out)
		}
		if status == c.MediaEmpty && !strings.Contains(out, "create") || status == c.MediaFailed && !strings.Contains(out, "retry") {
			t.Fatalf("P2: legitimate recovery missing: %s", out)
		}
	}
}
