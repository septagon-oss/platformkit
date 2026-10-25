package view_test

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// manyViewers is how many shells ask for the same page at once: two devices, a
// terminal and a browser, each on its own goroutine.
const manyViewers = 8

// TestBuildIsSafeForConcurrentViewers is the contract's promise that a caller may
// build the same page for two viewers at once: one Input, the same rows and the same
// maps, eight goroutines, every member written the same way each time. Run under
// -race, it is also the case that refuses a build which reaches back into its input
// to write what a kind does not show.
func TestBuildIsSafeForConcurrentViewers(t *testing.T) {
	t.Parallel()
	schema := sharedSchema()
	in := view.Input{
		Resource: view.Description{
			Resource: "m/task", Schema: schema,
			Operations: []string{"list", "read", "create", "update", "delete"},
			Commands:   []view.CommandDescription{{Verb: "archive", Collection: true, Fields: oneArgument()}},
		},
		Policy: present.Resolve(present.Input{
			Schema:   schema,
			Hints:    entity.EntryHints{Singular: "task", Plural: "tasks", EmptyDescription: "Nothing here yet"},
			Commands: []present.CommandInput{{Verb: "archive", Summary: "archive it"}},
			Language: "en",
		}),
		Data: view.Data{
			Records: []map[string]any{{"id": "t1", "title": "Write", "labels": []any{"a"},
				"meta": map[string]any{"k": "v", "z": "z"}, "when": "2026-07-01T14:12:00Z",
				"amount": json.Number("1250"), "currency": "EUR"}},
			Window: view.Window{Offset: 0, Limit: 20}, Total: 1, Counted: true,
			Violations: map[string]string{"title": "too short", "amount": "unknown currency"},
			Detail:     "two fields need attention",
		},
		State: view.State{Kind: view.KindList, Language: "en", Now: time.Now().UTC(), Writable: true, Zone: time.UTC},
	}
	var first string
	var once sync.Once
	var wg sync.WaitGroup
	for range manyViewers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				page, err := view.Build(in)
				if err != nil {
					t.Errorf("a concurrent build refused: %v", err)
					return
				}
				raw, err := json.Marshal(page)
				if err != nil {
					t.Errorf("a concurrent page does not marshal: %v", err)
					return
				}
				once.Do(func() { first = string(raw) })
				if string(raw) != first {
					t.Errorf("two viewers of one page got different bytes:\n%s\n%s", first, raw)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// sharedSchema is one registration every viewer reads at once.
func sharedSchema() entity.Schema {
	return entity.Schema{
		Module: "m", Entity: "task", Path: "/api/v1/m/task",
		Fields: []entity.Field{
			{Name: "id", Type: entity.TypeUUID, ReadOnly: true},
			{Name: "title", Type: entity.TypeString, Required: true},
			{Name: "when", Type: entity.TypeTime},
			{Name: "labels", Type: entity.TypeList, Elem: entity.TypeString},
			{Name: "meta", Type: entity.TypeMap, Elem: entity.TypeString},
			{Name: "amount", Type: entity.TypeInt, Presentation: entity.FieldHints{Money: &entity.FieldMoney{CurrencyField: "currency", Scale: 2}}},
			{Name: "currency", Type: entity.TypeString},
		},
	}
}

// oneArgument is one command argument, so an action's input list is built too.
func oneArgument() []entity.Field {
	return []entity.Field{{Name: "reason", Type: entity.TypeText, MaxLength: 200}}
}
