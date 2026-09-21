package resource_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// assignCommand and sweepCommand are the two shapes a lifecycle route comes in:
// one acts on the row somebody is looking at and asks for an argument, the other
// acts on the whole collection and asks for nothing.
var assignCommand = resource.Command{
	Verb: "assign", Label: "Assign this note", Description: "Makes somebody responsible.",
	Fields: []entity.Field{{Name: "who", Type: entity.TypeString}},
	Base:   "/app/note/notes",
}

var sweepCommand = resource.Command{
	Verb: "sweep", Label: "Reindex everything", Description: "Rebuilds the search index.",
	Collection: true, Base: "/app/note/notes",
}

// withCommands is the resource these cases render — the package's own Note, with the
// commands a caller was given. Reusing the fixture means a change to what a generated
// page shows shows up here as a failing assertion rather than a page nobody
// recognises.
func withCommands(cmds ...resource.Command) resource.Resource {
	r := note()
	r.Commands = cmds
	return r
}

// around is a failure's worth of markup rather than a whole page.
func around(body, needle string) string {
	i := strings.Index(body, needle)
	if i < 0 {
		return body[:min(len(body), 400)]
	}
	return body[max(0, i-240):min(len(body), i+240)]
}

// rowPage and listPage render the two screens a command can belong to.
func rowPage(t *testing.T, r resource.Resource) string {
	t.Helper()
	return render(t, resource.Detail(r, opts, map[string]any{"id": "11111111-1111-1111-1111-111111111111", "title": "Buy milk"}, true).Body)
}

func listPage(t *testing.T, r resource.Resource) string {
	t.Helper()
	return render(t, resource.List(r, opts, []map[string]any{{"id": "11111111-1111-1111-1111-111111111111", "title": "Buy milk"}}, 1, 1, "", false).Body)
}

// TestEveryCommandTheCallerMayUseHasExactlyOneControl is the gate the whole change
// is for. modules/task declares assign, resolve and check-sla and not one of them
// has ever had a control: the route, the permission and the event existed while the
// screen offered Edit and Delete and nothing else. Each command gets one form on the
// screen it acts from, and one only — a duplicated door is a duplicated action.
func TestEveryCommandTheCallerMayUseHasExactlyOneControl(t *testing.T) {
	r := withCommands(assignCommand, sweepCommand)
	row, list := rowPage(t, r), listPage(t, r)

	for _, c := range []struct{ page, action, label string }{
		{row, "/app/note/notes/11111111-1111-1111-1111-111111111111/assign", "Assign this note"},
		{list, "/app/note/notes/sweep", "Reindex everything"},
	} {
		if n := strings.Count(c.page, `action="`+c.action+`"`); n != 1 {
			t.Errorf("%q appears as a form %d times, want exactly one", c.action, n)
		}
		if !strings.Contains(c.page, c.label) {
			t.Errorf("the door for %q does not say what it does (%s): %s", c.action, c.label, around(c.page, c.label))
		}
	}
	// And neither screen offers the other's shape.
	if strings.Contains(row, "/sweep") {
		t.Errorf("a command over every row was offered beside one row: %s", around(row, "sweep"))
	}
	if strings.Contains(list, "/assign") {
		t.Errorf("a command over one row was offered above a table of many: %s", around(list, "assign"))
	}
}

// TestACommandThatTakesArgumentsAsksForThoseAndNoOthers: the form is the command's
// own schema, so an argument invented here would be a value the command never reads,
// and one dropped would be a refusal the person cannot see coming.
func TestACommandThatTakesArgumentsAsksForThoseAndNoOthers(t *testing.T) {
	out := rowPage(t, withCommands(assignCommand))
	if !strings.Contains(out, `name="who"`) {
		t.Errorf("the command asked for nothing: %s", around(out, "assign"))
	}
	if !strings.Contains(out, "Makes somebody responsible.") {
		t.Errorf("the form does not say what performing it does: %s", around(out, "assign"))
	}
	if n := strings.Count(out, `name="title"`); n != 0 {
		t.Errorf("the command form grew a field of the entity it was not given (%d): %s", n, around(out, "title"))
	}
}

// TestAScreenWithoutACallerDrawsNoCommands is docs/adr/0007's purity kept honest:
// a design export, a golden file or a test has no person on the other end, so
// "may this action be taken" has no answer and no control is drawn. A rendered page
// that offered doors nobody authorized is how a fixture starts to disagree with the
// application it claims to show.
func TestAScreenWithoutACallerDrawsNoCommands(t *testing.T) {
	bare := note()
	if out := rowPage(t, bare); strings.Contains(out, "<form") && strings.Contains(out, "/assign") {
		t.Errorf("a screen with no caller drew a command: %s", around(out, "assign"))
	}
	if out := listPage(t, bare); strings.Contains(out, "/sweep") {
		t.Errorf("a screen with no caller drew a collection command: %s", around(out, "sweep"))
	}
}
