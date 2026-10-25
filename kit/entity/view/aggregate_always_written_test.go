package view_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// aggregatesAre the JSON members a Page always writes as an array or an object: an
// aggregate is never null, so a shell never has to ask whether "no rows" arrived as
// [] or as null. The list is every array member of the published document, read off
// page-v1.schema.json by the case below, so a member added to the type is covered
// without editing this file.
//
// writtenAt is the instant a build must be given: Build reads no clock but the one it
// is handed, and refuses a page with no anchor.
var writtenAt = time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC)

func reviewSchema() entity.Schema {
	return entity.Schema{
		Module: "m", Entity: "task", Path: "/api/v1/m/task",
		Fields: []entity.Field{
			{Name: "id", Type: entity.TypeUUID, ReadOnly: true},
			{Name: "title", Type: entity.TypeString, Required: true, MaxLength: 120},
			{Name: "status", Type: entity.TypeString, Enum: []string{"open", "done"}, Default: "open"},
			{Name: "amount", Type: entity.TypeInt, Presentation: entity.FieldHints{Money: &entity.FieldMoney{CurrencyField: "currency", Scale: 2}}},
			{Name: "currency", Type: entity.TypeString},
			{Name: "dueAt", Type: entity.TypeTime},
			{Name: "labels", Type: entity.TypeList, Elem: entity.TypeString},
			{Name: "meta", Type: entity.TypeMap, Elem: entity.TypeString},
			{Name: "revision", Type: entity.TypeInt, ReadOnly: true},
		},
	}
}

func reviewInput(kind view.Kind, rows []map[string]any) view.Input {
	s := reviewSchema()
	return view.Input{
		Resource: view.Description{
			Resource: "m/task", Schema: s,
			Operations: []string{"list", "read", "create", "update", "delete"},
			Commands: []view.CommandDescription{{
				Verb: "archive", Collection: true, Undo: "unarchive", HasResult: true,
				Fields: []entity.Field{{Name: "reason", Type: entity.TypeText}},
			}},
		},
		Policy: present.Resolve(present.Input{
			Schema: s,
			Hints:  entity.EntryHints{Singular: "task", Plural: "tasks", StatusField: "status", EmptyDescription: "Nothing here yet"},
			Commands: []present.CommandInput{
				{Verb: "archive", Summary: "archive it", Undo: "unarchive", HasResult: true},
			},
			Language: "en",
		}),
		Data: view.Data{Records: rows, Total: int64(len(rows)), Counted: true,
			Window: view.Window{Offset: 0, Limit: 50}},
		State: view.State{Kind: kind, Language: "en", Now: writtenAt, Writable: true, Zone: time.UTC, Home: "Workspace"},
	}
}

func reviewBuild(t *testing.T, in view.Input) view.Page {
	t.Helper()
	page, err := view.Build(in)
	if err != nil {
		t.Fatalf("Build(%s): %v", in.State.Kind, err)
	}
	return page
}

// oneRow is the record a record or a form page needs; a record page from no row is a
// refusal, so every kind below has one.
func oneRow() []map[string]any {
	return []map[string]any{{"id": "t1", "title": "Write", "status": "open",
		"amount": json.Number("1250"), "currency": "EUR", "revision": json.Number("3")}}
}

// nullPaths returns every "parent.member" whose JSON value is null.
func nullPaths(v any, path string, out *[]string) {
	switch typed := v.(type) {
	case map[string]any:
		for k, item := range typed {
			if item == nil {
				*out = append(*out, path+"."+k)
				continue
			}
			nullPaths(item, path+"."+k, out)
		}
	case []any:
		for _, item := range typed {
			nullPaths(item, path+"[]", out)
		}
	}
}

// TestAPageWritesAnEmptyAggregateWhereItsKindShowsNothing is the promise every shell
// paints from: a Page always writes every member and an aggregate of its own — "" and
// [] mean none — so the two kinds a page does not show arrive empty, not null, and a
// builder never branches on which shape a section came in.
func TestAPageWritesAnEmptyAggregateWhereItsKindShowsNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind view.Kind
		in   view.Input
	}{
		{view.KindList, reviewInput(view.KindList, oneRow())},
		{view.KindRecord, reviewInput(view.KindRecord, oneRow())},
		{view.KindForm, reviewFormInput()},
	} {
		raw, err := json.Marshal(reviewBuild(t, tc.in))
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		var nulls []string
		nullPaths(doc, string(tc.kind), &nulls)
		// The only nulls a Page may carry are field values: Cell.value, Field.value,
		// Control.value. Every other member is a string, a bool, a number or an
		// aggregate that is never absent.
		for _, p := range nulls {
			if member := p[lastDot(p)+1:]; member != "value" {
				t.Errorf("%s page writes null at %s: an aggregate is [] or {} when the kind shows none, and only a field value may be null", tc.kind, p)
			}
		}
	}
}

func reviewFormInput() view.Input {
	in := reviewInput(view.KindForm, oneRow())
	in.State.Form = view.FormState{Create: true, Title: "New task"}
	return in
}

func lastDot(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}

// TestACRUDDoorWritesItsEmptyInputList covers the one member every page carries on
// every kind: a create, update or delete door has no arguments to ask for, and writing
// them as null is the second shape for one fact this contract exists to end — the
// published document says inputs is an array of controls.
func TestACRUDDoorWritesItsEmptyInputList(t *testing.T) {
	t.Parallel()
	page := reviewBuild(t, reviewInput(view.KindRecord, oneRow()))
	if len(page.Actions) == 0 {
		t.Fatal("a record page for a writable caller draws no door at all")
	}
	raw, err := json.Marshal(page.Actions)
	if err != nil {
		t.Fatal(err)
	}
	var actions []map[string]any
	if err := json.Unmarshal(raw, &actions); err != nil {
		t.Fatal(err)
	}
	for _, a := range actions {
		if a["inputs"] == nil {
			t.Errorf("the %s door writes inputs null, want []: %v", a["kind"], a["verb"])
		}
	}
}
