package resource_test

// presentation_test.go is the renderer's half of the reading contract: the three
// hints a screen honours today. The words, the icon and the tones arrive in the
// document and are drawn nowhere yet — that is the design's decision to make, in
// the commit that makes it.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func hintedResource(present entity.EntryHints, fields []entity.Field) resource.Resource {
	r := note()
	if fields != nil {
		r.Schema.Fields = fields
	}
	r.Present = present
	return r
}

func fieldsWith(hints map[string]entity.FieldHints) []entity.Field {
	out := entity.Fields[*Note]()
	for i := range out {
		if h, ok := hints[out[i].Name]; ok {
			out[i].Presentation = h
		}
	}
	return out
}

// TestVisibilityKeepsAFieldOffTheScreensTheAuthorNamed: `detail` is the field a
// record answers and a row does not need; `hidden` is off both. Neither is a
// permission — the value stays in the schema the form and the PATCH read.
func TestVisibilityKeepsAFieldOffTheScreensTheAuthorNamed(t *testing.T) {
	row := map[string]any{"id": "11111111-1111-1111-1111-111111111111", "title": "Buy milk",
		"status": "open", "rank": 2.0, "pinned": true}
	fields := fieldsWith(map[string]entity.FieldHints{
		"rank":   {Visibility: "detail"},
		"pinned": {Visibility: "hidden"},
	})
	listed := render(t, resource.List(hintedResource(entity.EntryHints{}, fields), opts,
		[]map[string]any{row}, 1, 1, "", false).Body)
	detail := render(t, resource.Detail(hintedResource(entity.EntryHints{}, fields), opts, row, false).Body)

	for _, want := range []struct {
		page, needle string
		present      bool
	}{
		{"list", "Rank", false}, {"list", "Pinned", false}, {"list", "Status", true},
		{"detail", "Rank", true}, {"detail", "Pinned", false}, {"detail", "Status", true},
	} {
		page := listed
		if want.page == "detail" {
			page = detail
		}
		if found := strings.Contains(page, want.needle); found != want.present {
			t.Errorf("the %s page contains %q = %v, want %v:\n%s", want.page, want.needle, found, want.present, around(page, want.needle))
		}
	}
	// Editability is readOnly/immutable, never visibility: the hidden field is
	// still an argument the form offers.
	form := render(t, resource.Form(hintedResource(entity.EntryHints{}, fields), opts, "/app/note/notes", "Edit", row, nil, "", false).Body)
	if !strings.Contains(form, `name="pinned"`) {
		t.Errorf("a hidden field left the edit form; visibility is a reading decision:\n%s", around(form, "pinned"))
	}
}

// TestASortableListOffersOnlyTheColumnsTheEntryNames: declaring sortable fields is
// saying which headers may be clicked, and a column outside the list is not.
func TestASortableListOffersOnlyTheColumnsTheEntryNames(t *testing.T) {
	row := map[string]any{"id": "11111111-1111-1111-1111-111111111111", "title": "Buy milk", "status": "open"}
	rows := []map[string]any{row}
	declared := render(t, resource.List(hintedResource(entity.EntryHints{Sortable: []string{"status"}}, nil),
		opts, rows, 1, 1, "", false).Body)
	undeclared := render(t, resource.List(hintedResource(entity.EntryHints{}, nil), opts, rows, 1, 1, "", false).Body)

	if !strings.Contains(declared, "sort=status") && !strings.Contains(declared, "href*=status") {
		t.Errorf("the declared sortable column offers no control:\n%s", around(declared, "Status"))
	}
	if strings.Contains(declared, "sort=title") || strings.Contains(undeclared, "sort=") == false {
		t.Errorf("the undeclared list offers nothing either, so the declared one narrows nothing:\ndeclared:\n%s\nundeclared:\n%s",
			around(declared, "Status"), around(undeclared, "Status"))
	}
}

// TestDeclaredSectionsAreTheBlocksOfTheRecordScreen, with the fields that named
// none drawn after them rather than between them.
func TestDeclaredSectionsAreTheBlocksOfTheRecordScreen(t *testing.T) {
	row := map[string]any{"id": "11111111-1111-1111-1111-111111111111", "title": "Buy milk",
		"status": "open", "rank": 2.0, "pinned": true}
	fields := fieldsWith(map[string]entity.FieldHints{
		"rank":   {Section: "timing"},
		"status": {Section: "timing"},
	})
	sectioned := render(t, resource.Detail(hintedResource(entity.EntryHints{
		Sections: []entity.EntitySection{{Key: "timing", Label: "Timing"}, {Key: "other", Label: "Otherwise"}},
	}, fields), opts, row, false).Body)
	flat := render(t, resource.Detail(hintedResource(entity.EntryHints{}, fields), opts, row, false).Body)

	if strings.Contains(flat, "Timing") {
		t.Error("an entity that declares no section drew a section heading")
	}
	timing, other := strings.Index(sectioned, "Timing"), strings.Index(sectioned, "Otherwise")
	if timing < 0 || other < 0 {
		t.Fatalf("a declared section is missing from the record screen:\n%s", sectioned)
	}
	if timing > other {
		t.Error("the sections are drawn in an order nobody declared")
	}
	// `title` named no section, so it comes after both — and it is still on the page.
	title := strings.LastIndex(sectioned, "Title")
	if strings.Index(sectioned, "Timing") > other {
		t.Error("the sections are drawn in the wrong order")
	}
	if title < other {
		t.Errorf("an unsectioned field was drawn inside a declared block (title at %d, second block at %d)", title, other)
	}
}

// TestAFieldTheRecordDoesNotDrawDoesNotNameIt. `hidden` keeps a field off the
// description list, and the record's title is its toolbar, its breadcrumb and the
// browser tab a person bookmarks: a value the author kept off the page that came
// back through the row's name would be the same promise broken by the identity
// selection rather than the column loop. `detail` is drawn here, so it may name it.
func TestAFieldTheRecordDoesNotDrawDoesNotNameIt(t *testing.T) {
	row := map[string]any{"id": "11111111-1111-1111-1111-111111111111", "title": "The name nobody is shown",
		"body": "The body a record answers", "status": "open"}
	hidden := resource.Detail(hintedResource(entity.EntryHints{}, fieldsWith(map[string]entity.FieldHints{
		"title": {Visibility: "hidden"}})), opts, row, false)
	if strings.Contains(hidden.Title, "The name nobody is shown") {
		t.Errorf("visibility:hidden names the record from a field it does not draw: %q", hidden.Title)
	}
	if !strings.Contains(hidden.Title, "The body a record answers") {
		t.Errorf("the record fell back past the next field it draws rather than onto it: %q", hidden.Title)
	}
	detail := resource.Detail(hintedResource(entity.EntryHints{}, fieldsWith(map[string]entity.FieldHints{
		"body": {Visibility: "detail"}})), opts, row, false)
	if !strings.Contains(detail.Title, "The name nobody is shown") {
		t.Errorf("the field the record leads with lost its name to a visibility that kept nothing off it: %q", detail.Title)
	}
}

// TestABlankRowNamesItselfWithAFieldTheListDraws: the fallback for an empty leading
// cell walks the same fields the columns are drawn from, so a row with nothing in
// its name cannot be linked by a value its author kept off the list.
func TestABlankRowNamesItselfWithAFieldTheListDraws(t *testing.T) {
	row := map[string]any{"id": "11111111-1111-1111-1111-111111111111", "title": "The name nobody is shown",
		"status": "", "rank": 0.0, "pinned": false}
	page := render(t, resource.List(hintedResource(entity.EntryHints{}, fieldsWith(map[string]entity.FieldHints{
		"title": {Visibility: "hidden"}})), opts, []map[string]any{row}, 1, 1, "", false).Body)
	if strings.Contains(page, "The name nobody is shown") {
		t.Errorf("a blank row was named by a field off the list:\n%s", around(page, "link"))
	}
}

// TestADeclaredConfirmationIsReadBeforeTheButtonAndAnUndeclaredOneIsInventedNowhere.
func TestADeclaredConfirmationIsReadBeforeTheButtonAndAnUndeclaredOneIsInventedNowhere(t *testing.T) {
	asked := withCommands(resource.Command{Verb: "sweep", Label: "Reindex everything",
		Description: "Rebuilds the search index.", Collection: true, Base: "/app/note/notes",
		Present: entity.CommandHints{Confirmation: &entity.CommandConfirmation{
			Title: "Rebuild the whole index?", Body: "Search is slow until it finishes.", ConfirmLabel: "Reindex"}}})
	page := listPage(t, asked)
	for _, want := range []string{"Rebuild the whole index?", "Search is slow until it finishes.", "Reindex"} {
		if !strings.Contains(page, want) {
			t.Errorf("the declared consequence does not reach the form (want %q):\n%s", want, around(page, "sweep"))
		}
	}
	if strings.Contains(page, "Are you sure") {
		t.Error("the renderer invented a warning of its own beside the declared one")
	}
	quiet := listPage(t, withCommands(sweepCommand))
	if strings.Contains(quiet, "Are you sure") || strings.Contains(quiet, "Search is slow") {
		t.Error("a command that declared no confirmation was given one anyway")
	}
	if strings.Count(quiet, `<form`)-strings.Count(listPage(t, withCommands()), `<form`) != 1 {
		t.Error("the command's form is no longer exactly one")
	}
}

// TestTheButtonWordIsTheAuthor'sAndTheSummaryIsTheFallback.
func TestTheButtonWordIsTheAuthorsAndTheSummaryIsTheFallback(t *testing.T) {
	page := listPage(t, withCommands(sweepCommand))
	if !strings.Contains(page, "Reindex everything") {
		t.Error("a command with no declared label lost the Summary it has always been drawn with")
	}
}
