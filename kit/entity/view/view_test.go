package view_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// now is the instant every case stamps: a build reads no clock but the one it is
// given, so a test that did not pass one would be given none and refused.
var now = time.Date(2026, 10, 24, 5, 33, 7, 0, time.UTC)

func taskSchema() entity.Schema {
	return entity.Schema{
		Module: "m", Entity: "task", Path: "/api/v1/m/task",
		Fields: []entity.Field{
			{Name: "id", Type: entity.TypeUUID, ReadOnly: true},
			{Name: "title", Type: entity.TypeString, Required: true, MaxLength: 120},
			{Name: "status", Type: entity.TypeString, Enum: []string{"open", "done"}, Default: "open",
				Presentation: entity.FieldHints{EnumLabels: map[string]string{"open": "Open"}, EnumTones: map[string]string{"done": "success"}}},
			{Name: "done", Type: entity.TypeBool},
			{Name: "amount", Type: entity.TypeInt, Presentation: entity.FieldHints{Money: &entity.FieldMoney{CurrencyField: "currency", Scale: 2}}},
			{Name: "currency", Type: entity.TypeString},
			{Name: "dueAt", Type: entity.TypeTime},
			{Name: "labels", Type: entity.TypeList, Elem: entity.TypeString},
			{Name: "meta", Type: entity.TypeMap, Elem: entity.TypeString},
			{Name: "revision", Type: entity.TypeInt, ReadOnly: true},
			{Name: "createdAt", Type: entity.TypeTime, ReadOnly: true},
		},
	}
}

func taskPolicy(s entity.Schema, commands []present.CommandInput, operations []string, singleton bool) present.Resolved {
	return present.Resolve(present.Input{
		Schema:     s,
		Hints:      entity.EntryHints{Singular: "task", Plural: "tasks", StatusField: "status", EmptyDescription: "Nothing here yet"},
		Operations: operations, Commands: commands, Singleton: singleton, Language: "en",
	})
}

func commandInput(verb string) present.CommandInput {
	return present.CommandInput{Verb: verb, Summary: verb + " it", Undo: "undo" + verb, HasResult: true}
}

func archiveDescription() []view.CommandDescription {
	return []view.CommandDescription{{
		Verb: "archive", Collection: true, Undo: "unarchive", HasResult: true,
		Fields: []entity.Field{
			{Name: "reason", Type: entity.TypeText, MaxLength: 200},
			{Name: "notify", Type: entity.TypeBool},
		},
	}, {
		Verb: "assign", Undo: "unassign",
		Fields: []entity.Field{{Name: "owner", Type: entity.TypeUUID}},
	}}
}

// listInput is one list page: two rows in the read's order, a window, a count and
// the sort the read used.
func listInput() view.Input {
	rows := []map[string]any{
		{"id": "t1", "title": "Write", "status": "open", "amount": json.Number("1250"), "currency": "EUR",
			"dueAt": "2026-07-01T14:12:00Z", "labels": []any{"a"}, "revision": json.Number("3")},
		{"id": "t2", "title": "Ship", "status": "done", "amount": json.Number("-50"), "currency": "EUR",
			"labels": []any{}, "revision": json.Number("1")},
	}
	return input(view.KindList, rows, nil)
}

func input(kind view.Kind, rows []map[string]any, state func(*view.State)) view.Input {
	s := taskSchema()
	commands := []present.CommandInput{commandInput("archive"), commandInput("assign")}
	in := view.Input{
		Resource: view.Description{
			Resource: "m/task", Schema: s, Operations: []string{"list", "read", "create", "update", "delete"},
			Commands: archiveDescription(),
		},
		Policy: taskPolicy(s, commands, nil, false),
		Data: view.Data{
			Records: rows, Total: int64(len(rows)), Counted: true,
			Window: view.Window{Offset: 0, Limit: 50}, Sort: "-createdAt",
		},
		State: view.State{Kind: kind, Language: "en", Now: now, Writable: true, Zone: time.UTC, Home: "Workspace"},
	}
	if state != nil {
		state(&in.State)
	}
	return in
}

func build(t *testing.T, in view.Input) view.Page {
	t.Helper()
	page, err := view.Build(in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return page
}

func bytes(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func TestAVersionThisBuildReadsIsTheOneItWrites(t *testing.T) {
	t.Parallel()
	if !view.SupportedVersion(view.PageVersion) {
		t.Fatalf("a build does not read the version it writes")
	}
	for _, v := range []int{0, 2, 99} {
		if view.SupportedVersion(v) {
			t.Errorf("SupportedVersion(%d) is true: a revision this build does not read is refused whole", v)
		}
	}
}

func TestAListPageCarriesTheWindowCountsAndSortsItWasReadWith(t *testing.T) {
	t.Parallel()
	page := build(t, listInput())
	if page.PageVersion != view.PageVersion || page.Kind != view.KindList {
		t.Fatalf("pageVersion %d kind %q", page.PageVersion, page.Kind)
	}
	if page.Title != "tasks" || page.Language != "en" || page.Zone != "UTC" {
		t.Fatalf("title %q language %q zone %q", page.Title, page.Language, page.Zone)
	}
	if !page.BuiltAt.Equal(now) {
		t.Errorf("builtAt %s, want the instant the state carried", page.BuiltAt)
	}
	table := page.Table
	if table.Window != (view.Window{Offset: 0, Limit: 50}) || table.Total != 2 || !table.Counted {
		t.Errorf("window %+v total %d counted %v", table.Window, table.Total, table.Counted)
	}
	if table.Sort != "-createdAt" || table.DefaultSort != "-createdAt" {
		t.Errorf("sort %q defaultSort %q", table.Sort, table.DefaultSort)
	}
	if len(table.Sorts) == 0 || len(table.Filters) == 0 {
		t.Fatalf("the page offers no orders or narrowings the policy offered")
	}
	if len(table.Rows) != 2 || table.Rows[0].ID != "t1" || table.Rows[1].ID != "t2" {
		t.Fatalf("rows came in another order: %+v", table.Rows)
	}
	for _, row := range table.Rows {
		if len(row.Cells) != len(table.Columns) {
			t.Errorf("row %s has %d cells for %d columns", row.ID, len(row.Cells), len(table.Columns))
		}
		for i, cell := range row.Cells {
			if cell.Field != table.Columns[i].Field {
				t.Errorf("row %s cell %d is %s, want %s", row.ID, i, cell.Field, table.Columns[i].Field)
			}
		}
	}
	if page.Empty != "" {
		t.Errorf("empty %q with rows present", page.Empty)
	}
}

func TestAListPageOfNothingSaysSo(t *testing.T) {
	t.Parallel()
	page := build(t, input(view.KindList, nil, nil))
	if page.Empty != "Nothing here yet" {
		t.Errorf("empty %q, want the policy's own empty description", page.Empty)
	}
	if page.Table.Rows == nil || len(page.Table.Rows) != 0 {
		t.Errorf("rows %v, want an empty list written as []", page.Table.Rows)
	}
}

func TestARecordPageShowsTheThreeBlocksInOrder(t *testing.T) {
	t.Parallel()
	page := build(t, input(view.KindRecord, []map[string]any{{"id": "t1", "title": "Write", "status": "done"}}, nil))
	if page.Title != "Write" || page.Ref != (view.Ref{Resource: "m/task", ID: "t1"}) {
		t.Fatalf("title %q ref %+v", page.Title, page.Ref)
	}
	if len(page.Sections) == 0 {
		t.Fatal("a record with no block to show")
	}
	if page.Sections[0].Key != "overview" {
		t.Errorf("first section %q, want overview", page.Sections[0].Key)
	}
	last := page.Sections[len(page.Sections)-1]
	if last.Key != "information" {
		t.Errorf("last section %q, want information", last.Key)
	}
	for _, s := range page.Sections {
		if len(s.Fields) == 0 {
			t.Errorf("section %q is written with no fields", s.Key)
		}
	}
	if len(page.Table.Columns) != 0 || len(page.Table.Rows) != 0 || len(page.Form.Controls) != 0 {
		t.Error("a record page wrote rows or controls")
	}
	if page.Breadcrumbs[len(page.Breadcrumbs)-1].Ref.ID != "t1" {
		t.Errorf("the last crumb does not name the record: %+v", page.Breadcrumbs)
	}
}

func TestADeclaredBlockKeepsItsOwnKeyAndLabel(t *testing.T) {
	t.Parallel()
	s := taskSchema()
	in := input(view.KindRecord, []map[string]any{{"id": "t1", "title": "Write", "notes": "n"}}, nil)
	s.Fields = append(s.Fields, entity.Field{Name: "notes", Type: entity.TypeString, Presentation: entity.FieldHints{Section: "detail"}})
	in.Resource.Schema = s
	in.Policy = present.Resolve(present.Input{
		Schema: s, Hints: entity.EntryHints{Sections: []entity.EntitySection{{Key: "detail", Label: "Detail"}}},
		Language: "en",
	})
	page := build(t, in)
	found := false
	for _, sec := range page.Sections {
		if sec.Key == "detail" {
			found = sec.Label == "Detail"
		}
	}
	if !found {
		t.Errorf("the declared block lost its key or its label: %+v", page.Sections)
	}
}

func TestABuildIsByteIdenticalAcrossAThousandRuns(t *testing.T) {
	t.Parallel()
	in := input(view.KindForm, []map[string]any{{"id": "t1", "title": "Write", "meta": map[string]any{"z": 1, "a": 2}}}, func(s *view.State) {
		s.Form = view.FormState{Create: true, Title: "New task"}
	})
	in.Data.Violations = map[string]string{"zebra": "z", "alpha": "a", "middle": "m", "beta": "b", "delta": "d"}
	in.Data.Detail = "two fields need attention"
	first := string(bytes(t, build(t, in)))
	for range 1000 {
		if got := string(bytes(t, build(t, in))); got != first {
			t.Fatalf("two builds of one input differ:\n%s\n%s", first, got)
		}
	}
}

func TestBuildMutatesNoInput(t *testing.T) {
	t.Parallel()
	in := listInput()
	before := string(bytes(t, in))
	page := build(t, in)
	page.Table.Rows[0].Cells[0].Text = "mutated"
	page.Actions = append(page.Actions, view.Action{Verb: "invented"})
	if after := string(bytes(t, in)); after != before {
		t.Fatalf("Build wrote its input:\n%s\n%s", before, after)
	}
}

func TestAPageCarriesNoAddressAndNoRowPosition(t *testing.T) {
	t.Parallel()
	for _, kind := range []view.Kind{view.KindList, view.KindRecord, view.KindForm} {
		in := input(kind, []map[string]any{{"id": "t1", "title": "Write"}}, nil)
		if kind == view.KindForm {
			in.State.Form = view.FormState{Create: true, Title: "New task"}
		}
		var doc map[string]any
		if err := json.Unmarshal(bytes(t, build(t, in)), &doc); err != nil {
			t.Fatal(err)
		}
		members := map[string]bool{}
		collectKeys(doc, members)
		for name := range members {
			switch name {
			case "href", "url", "path", "screen", "writePath", "index", "position", "row":
				t.Errorf("%s: the Page carries the member %q, which is an address or a position", kind, name)
			}
		}
	}
}

func collectKeys(v any, out map[string]bool) {
	switch typed := v.(type) {
	case map[string]any:
		for k, item := range typed {
			out[k] = true
			collectKeys(item, out)
		}
	case []any:
		for _, item := range typed {
			collectKeys(item, out)
		}
	}
}

func TestARowOrderMovesNoActionTarget(t *testing.T) {
	t.Parallel()
	in := input(view.KindRecord, []map[string]any{{"id": "t1", "title": "Write"}}, nil)
	one := build(t, in)
	in.Data.Records = []map[string]any{{"id": "t9", "title": "Other"}}
	two := build(t, in)
	if one.Actions[0].Target.ID != "t1" || two.Actions[0].Target.ID != "t9" {
		t.Fatalf("an action's target is not the row: %+v %+v", one.Actions, two.Actions)
	}
	if one.Actions[0].Kind != two.Actions[0].Kind || one.Actions[0].Verb != two.Actions[0].Verb {
		t.Error("the same door changed kind or verb between rows")
	}
}

func TestADeclaredPageDrawsItsDoorsInOrder(t *testing.T) {
	t.Parallel()
	page := build(t, listInput())
	if len(page.Actions) != 2 || page.Actions[0].Kind != view.ActionCreate || page.Actions[1].Kind != view.ActionCommand {
		t.Fatalf("list actions %+v, want the create door then the command", page.Actions)
	}
	if page.Actions[0].Scope != view.ScopeCollection || page.Actions[0].Target.ID != "" {
		t.Errorf("the create door is not a collection door: %+v", page.Actions[0])
	}
	if page.Actions[0].Label != "" {
		t.Errorf("the create door carries the words %q, which are the shell's to compose", page.Actions[0].Label)
	}
	command := page.Actions[1]
	if command.Verb != "archive" || command.Label != "archive it" || command.Scope != view.ScopeCollection ||
		command.Undo != "unarchive" || !command.HasResult || len(command.Inputs) != 2 {
		t.Errorf("the command action is not what both halves declared: %+v", command)
	}
	// A collection command has no record to act on, so a record page draws the
	// record's own commands and a list page draws the collection's.
	record := build(t, input(view.KindRecord, []map[string]any{{"id": "t1", "title": "Write"}}, nil))
	kinds := []view.ActionKind{}
	for _, a := range record.Actions {
		kinds = append(kinds, a.Kind)
	}
	if len(kinds) != 3 || kinds[0] != view.ActionUpdate || kinds[1] != view.ActionDelete || kinds[2] != view.ActionCommand {
		t.Fatalf("record doors %+v, want update, delete, command", kinds)
	}
	if record.Actions[2].Verb != "assign" || record.Actions[2].Scope != view.ScopeRecord || record.Actions[2].Target.ID != "t1" {
		t.Errorf("the record's command is not aimed at the record: %+v", record.Actions[2])
	}
	if !record.Actions[1].Destructive {
		t.Error("a delete door does not say it costs something")
	}
}

func TestAPageForAReadOnlyCallerDrawsNoWriteDoor(t *testing.T) {
	t.Parallel()
	for _, kind := range []view.Kind{view.KindList, view.KindRecord} {
		page := build(t, input(kind, []map[string]any{{"id": "t1", "title": "Write"}}, func(s *view.State) { s.Writable = false }))
		for _, a := range page.Actions {
			if a.Kind != view.ActionCommand {
				t.Errorf("%s page offers %s to a caller with no write grant", kind, a.Kind)
			}
		}
	}
}

func TestACommandTheCallerMayNotRunIsNotAnAction(t *testing.T) {
	t.Parallel()
	in := listInput()
	in.Policy = taskPolicy(in.Resource.Schema, nil, nil, false)
	if actions := build(t, in).Actions; len(actions) != 1 {
		t.Fatalf("actions %+v, want only the create door", actions)
	}
}

func TestACommandWithNoDeclaredArgumentsIsNotAnAction(t *testing.T) {
	t.Parallel()
	in := listInput()
	in.Resource.Commands = nil
	for _, a := range build(t, in).Actions {
		if a.Kind == view.ActionCommand {
			t.Fatalf("an action with undeclared arguments was drawn: %+v", a)
		}
	}
	in.Resource.Commands = []view.CommandDescription{{Verb: "archive", Collection: true}}
	page := build(t, in)
	if len(page.Actions) != 2 || len(page.Actions[1].Inputs) != 0 {
		t.Fatalf("a command that takes nothing is not an action with no inputs: %+v", page.Actions)
	}
}

func TestASystemCommandIsNeverDrawn(t *testing.T) {
	t.Parallel()
	in := listInput()
	in.Policy = taskPolicy(in.Resource.Schema, []present.CommandInput{{Verb: "archive", Hints: entity.CommandHints{System: true}}}, nil, false)
	for _, a := range build(t, in).Actions {
		if a.Kind == view.ActionCommand {
			t.Fatalf("a command nobody is offered was drawn: %+v", a)
		}
	}
}

func TestAFormDrawsTheLayoutsOwnControlsInOrder(t *testing.T) {
	t.Parallel()
	in := input(view.KindForm, nil, func(s *view.State) { s.Form = view.FormState{Create: true, Title: "New task"} })
	page := build(t, in)
	form := page.Form
	if !form.Create || form.Title != "New task" || form.ExpectedRevision != 0 {
		t.Fatalf("form %+v", form)
	}
	// A create page over a row still names no condition: the row it is drawn beside is
	// not the row this write would create.
	withRow := in
	withRow.Data.Records = []map[string]any{{"id": "t1", "title": "Write", "revision": json.Number("7")}}
	if page := build(t, withRow); page.Form.ExpectedRevision != 0 || page.Ref.ID != "" {
		t.Errorf("a create page over a row carries revision %d and ref %+v, want none",
			page.Form.ExpectedRevision, page.Ref)
	}
	if len(form.Controls) == 0 {
		t.Fatal("a create form with no controls")
	}
	if form.Controls[0].Name != "title" || !form.Controls[0].Required || form.Controls[0].MaxLength != 120 {
		t.Errorf("first control %+v", form.Controls[0])
	}
	for _, c := range form.Controls {
		if c.Name == "id" || c.Name == "revision" {
			t.Errorf("the form takes %q, which the caller may not write", c.Name)
		}
		if c.Options == nil || c.Keys == nil {
			t.Errorf("control %s writes a null aggregate", c.Name)
		}
		if c.Name == "status" && (c.Default != "open" || c.Value != "open") {
			t.Errorf("the declared default did not reach the control: %+v", c)
		}
	}
	if len(page.Actions) != 0 {
		t.Errorf("a form page drew %d doors of its own", len(page.Actions))
	}
}

func TestAnUpdatePageCarriesTheRevisionItWasDrawnFrom(t *testing.T) {
	t.Parallel()
	in := input(view.KindForm, []map[string]any{{"id": "t1", "title": "Write", "revision": int64(7)}}, func(s *view.State) {
		s.Form = view.FormState{Title: "Edit task"}
	})
	page := build(t, in)
	if page.Form.ExpectedRevision != 7 || page.Form.Create {
		t.Fatalf("form %+v", page.Form)
	}
	if page.Ref.ID != "t1" || page.Breadcrumbs[len(page.Breadcrumbs)-1].Ref.ID != "t1" {
		t.Errorf("the update form does not name its record: %+v", page.Ref)
	}
	for _, c := range page.Form.Controls {
		if c.Name == "title" && c.Value != "Write" {
			t.Errorf("the control starts at %v, want the row's own value", c.Value)
		}
	}
}

func TestAFormPageCarriesTheRefusedWritesVerdictInFieldOrder(t *testing.T) {
	t.Parallel()
	in := input(view.KindForm, []map[string]any{{"id": "t1", "title": ""}}, func(s *view.State) {
		s.Form = view.FormState{Title: "Edit task"}
	})
	in.Data.Violations = map[string]string{"title": "a name is required", "dueAt": "not a day"}
	in.Data.Detail = "two fields need attention"
	page := build(t, in)
	if page.Form.Detail != "two fields need attention" {
		t.Fatalf("detail %q", page.Form.Detail)
	}
	if len(page.Form.Violations) != 2 || page.Form.Violations[0].Field != "dueAt" || page.Form.Violations[1].Field != "title" {
		t.Fatalf("violations %+v, want them ordered by field name", page.Form.Violations)
	}
}

func TestACommandInputCarriesItsArguments(t *testing.T) {
	t.Parallel()
	in := input(view.KindRecord, []map[string]any{{"id": "t1", "title": "Write"}}, nil)
	in.Resource.Commands = []view.CommandDescription{{Verb: "archive", Fields: []entity.Field{
		{Name: "reason", Type: entity.TypeText, Required: true},
		{Name: "fields", Type: entity.TypeMap, Elem: entity.TypeString, Keys: []entity.MapKey{{Name: "title"}}},
	}}}
	page := build(t, in)
	var inputs []view.Control
	for _, a := range page.Actions {
		if a.Kind == view.ActionCommand {
			inputs = a.Inputs
		}
	}
	if len(inputs) != 2 || inputs[0].Name != "reason" || inputs[0].Control != "textarea" {
		t.Fatalf("command inputs %+v", inputs)
	}
	if len(inputs[1].Keys) != 1 || inputs[1].Keys[0].Name != "title" {
		t.Errorf("the map argument lost its keys: %+v", inputs[1])
	}
}

func TestASingletonPageHasNoCreateDoorAndNoId(t *testing.T) {
	t.Parallel()
	in := input(view.KindRecord, []map[string]any{{"title": "The one row"}}, nil)
	in.Resource.Singleton = true
	in.Resource.Operations = nil
	in.Policy = taskPolicy(in.Resource.Schema, []present.CommandInput{commandInput("archive")}, nil, true)
	page := build(t, in)
	if page.Ref.ID != "" || page.Actions[0].Kind != view.ActionUpdate || page.Actions[0].Target.ID != "" {
		t.Fatalf("a tenant's one row is named and door-served like many: %+v %+v", page.Ref, page.Actions)
	}
}

// TestTheColumnThatOrdersTheListSaysSo checks the header a person would click again:
// exactly one column claims the order the list is in — the one the read sorted by, or
// the policy's default when the read sent none — and offers its own ascending spelling
// as the next click.
func TestTheColumnThatOrdersTheListSaysSo(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ sort, ordered string }{
		{"-createdAt", "createdAt"}, {"", "createdAt"}, {"-title", "title"},
	} {
		in := listInput()
		in.Data.Sort = tc.sort
		for _, c := range build(t, in).Table.Columns {
			if c.Current != (c.Field == tc.ordered) {
				t.Errorf("sort %q: column %s current %v, want %v", tc.sort, c.Field, c.Current, c.Field == tc.ordered)
			}
			if c.Field == tc.ordered && (!c.Sortable || c.Sort != tc.ordered) {
				t.Errorf("sort %q: the ordered column offers %q sortable %v, want %q", tc.sort, c.Sort, c.Sortable, tc.ordered)
			}
		}
	}
}
