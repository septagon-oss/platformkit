package entity_test

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
)

// JSONSchema is what a consumer that never compiled against PlatformKit reads,
// so each case below asks the question that consumer asks of the document: what
// type is this, may I leave it out, and what does it become if I do.
//
// The comparisons go through JSON rather than reflect.DeepEqual on Go values so
// that a test does not pin which integer width the conversion happened to use:
// what the document has to say is `3`, and how Go spells that is nobody's
// contract.

func TestJSONSchemaShapesEveryFieldType(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		field entity.Field
		want  string
	}{
		{"string", entity.Field{Name: "v", Type: entity.TypeString}, `{"type":"string"}`},
		{"text is a string that wants a textarea", entity.Field{Name: "v", Type: entity.TypeText},
			`{"type":"string","x-platformkit-widget":"textarea"}`},
		{"int", entity.Field{Name: "v", Type: entity.TypeInt}, `{"type":"integer"}`},
		{"float", entity.Field{Name: "v", Type: entity.TypeFloat}, `{"type":"number"}`},
		{"bool", entity.Field{Name: "v", Type: entity.TypeBool}, `{"type":"boolean"}`},
		{"time", entity.Field{Name: "v", Type: entity.TypeTime}, `{"type":"string","format":"date-time"}`},
		{"uuid", entity.Field{Name: "v", Type: entity.TypeUUID}, `{"type":"string","format":"uuid"}`},
		{"list of strings", entity.Field{Name: "v", Type: entity.TypeList, Elem: entity.TypeString},
			`{"type":"array","items":{"type":"string"}}`},
		{"list of text", entity.Field{Name: "v", Type: entity.TypeList, Elem: entity.TypeText},
			`{"type":"array","items":{"type":"string"}}`},
		{"list of ints", entity.Field{Name: "v", Type: entity.TypeList, Elem: entity.TypeInt},
			`{"type":"array","items":{"type":"integer"}}`},
		{"list of floats", entity.Field{Name: "v", Type: entity.TypeList, Elem: entity.TypeFloat},
			`{"type":"array","items":{"type":"number"}}`},
		{"list of bools", entity.Field{Name: "v", Type: entity.TypeList, Elem: entity.TypeBool},
			`{"type":"array","items":{"type":"boolean"}}`},
		{"list of times", entity.Field{Name: "v", Type: entity.TypeList, Elem: entity.TypeTime},
			`{"type":"array","items":{"type":"string","format":"date-time"}}`},
		{"list of uuids", entity.Field{Name: "v", Type: entity.TypeList, Elem: entity.TypeUUID},
			`{"type":"array","items":{"type":"string","format":"uuid"}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := entity.JSONSchema([]entity.Field{tt.field})
			if err != nil {
				t.Fatal(err)
			}
			jsonEqual(t, property(t, doc, "v"), tt.want)
		})
	}
}

// TestJSONSchemaNamesTheWidgetItCarries: a declared widget is the control a
// screen draws, so the projection passes it over as it stands and does not add
// one where the type already said enough.
func TestJSONSchemaNamesTheWidgetItCarries(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		field entity.Field
		want  string
	}{
		{"a select over a closed set", entity.Field{Name: "v", Type: entity.TypeString,
			Enum: []string{"open", "done"}, Widget: "select"},
			`{"type":"string","enum":["open","done"],"x-platformkit-widget":"select"}`},
		{"a declared widget wins over the one text would pick", entity.Field{Name: "v", Type: entity.TypeText, Widget: "textarea"},
			`{"type":"string","x-platformkit-widget":"textarea"}`},
		{"an email is still a string", entity.Field{Name: "v", Type: entity.TypeString, Widget: "email"},
			`{"type":"string","x-platformkit-widget":"email"}`},
		{"which screen shows the column is not the record's business", entity.Field{Name: "v", Type: entity.TypeString, HideList: true},
			`{"type":"string"}`},
		{"how a value reads is not the record's business either", entity.Field{Name: "v", Type: entity.TypeUUID, Present: "person"},
			`{"type":"string","format":"uuid"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := entity.JSONSchema([]entity.Field{tt.field})
			if err != nil {
				t.Fatal(err)
			}
			jsonEqual(t, property(t, doc, "v"), tt.want)
		})
	}
}

// TestJSONSchemaDefaultsAreTheJSONType is the half a form renderer cannot
// recover from the document on its own: a bool whose default is the text
// "false" is an unchecked checkbox that reads as true to anything validating
// the document, and a select preselected by `"open"` rather than `open` is a
// select that preselects nothing.
func TestJSONSchemaDefaultsAreTheJSONType(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		field entity.Field
		want  string
	}{
		{"an int counts", entity.Field{Name: "v", Type: entity.TypeInt, Default: "3"}, `{"type":"integer","default":3}`},
		{"a negative int counts", entity.Field{Name: "v", Type: entity.TypeInt, Default: "-7"}, `{"type":"integer","default":-7}`},
		{"a float adds a fraction", entity.Field{Name: "v", Type: entity.TypeFloat, Default: "1.5"}, `{"type":"number","default":1.5}`},
		{"a true is a boolean", entity.Field{Name: "v", Type: entity.TypeBool, Default: "true"}, `{"type":"boolean","default":true}`},
		{"a false is a boolean too", entity.Field{Name: "v", Type: entity.TypeBool, Default: "false"}, `{"type":"boolean","default":false}`},
		{"an option preselects itself", entity.Field{Name: "v", Type: entity.TypeString, Default: "open"}, `{"type":"string","default":"open"}`},
		// An instant and an identifier are strings in JSON; the conversion is to
		// the JSON type, so the declared text is the value. Asserting the format
		// is a validator's job, the same one it does for a supplied value.
		{"an instant is the string it is", entity.Field{Name: "v", Type: entity.TypeTime,
			Default: "2026-01-01T00:00:00Z"}, `{"type":"string","format":"date-time","default":"2026-01-01T00:00:00Z"}`},
		{"an identifier is the string it is", entity.Field{Name: "v", Type: entity.TypeUUID,
			Default: "00000000-0000-0000-0000-000000000000"},
			`{"type":"string","format":"uuid","default":"00000000-0000-0000-0000-000000000000"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := entity.JSONSchema([]entity.Field{tt.field})
			if err != nil {
				t.Fatal(err)
			}
			jsonEqual(t, property(t, doc, "v"), tt.want)
		})
	}
}

// TestJSONSchemaRefusesADefaultItCannotConvert is the error the signature is
// for. Emitting the tag's text instead would put a value in the document the
// type printed beside it refuses, and the reader would find out by trusting it.
func TestJSONSchemaRefusesADefaultItCannotConvert(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, want string
		field      entity.Field
	}{
		{"an int that is a word", "count", entity.Field{Name: "count", Type: entity.TypeInt, Default: "three"}},
		{"a float that is a word", "rate", entity.Field{Name: "rate", Type: entity.TypeFloat, Default: "fast"}},
		{"a bool that is a word", "done", entity.Field{Name: "done", Type: entity.TypeBool, Default: "maybe"}},
		// No separator is defined for a default list anywhere the foundation
		// reads, so this document would be the only place "a,b" meant two things.
		{"a list has no default spelling", "roles", entity.Field{Name: "roles", Type: entity.TypeList, Elem: entity.TypeString, Default: "admin,member"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := entity.JSONSchema([]entity.Field{tt.field})
			if err == nil {
				t.Fatalf("a default no type can hold was accepted: %v", doc)
			}
			if doc != nil {
				t.Errorf("a failed projection returned a document anyway: %v", doc)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("the error does not name the field: %v", err)
			}
		})
	}
}

// TestJSONSchemaDescribesTheRecord is what the per-property tables cannot see:
// the document's own keys, and which names belong in required.
func TestJSONSchemaDescribesTheRecord(t *testing.T) {
	t.Parallel()
	doc, err := entity.JSONSchema([]entity.Field{
		{Name: "id", Type: entity.TypeUUID, ReadOnly: true},
		{Name: "title", Type: entity.TypeString, Required: true, Doc: "Short summary of the task"},
		{Name: "priority", Type: entity.TypeString, Default: "normal"},
		{Name: "roles", Type: entity.TypeList, Elem: entity.TypeString, Required: true},
		// Required and ReadOnly together mean the server owns the value, so a
		// caller that had to send it could not create the row at all.
		{Name: "createdAt", Type: entity.TypeTime, Required: true, ReadOnly: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := doc["$schema"], "https://json-schema.org/draft/2020-12/schema"; got != want {
		t.Errorf("$schema = %v, want %v", got, want)
	}
	if got, want := doc["type"], "object"; got != want {
		t.Errorf("type = %v, want %v", got, want)
	}
	if got, ok := doc["additionalProperties"].(bool); !ok || got {
		t.Errorf("additionalProperties = %v, want false: a field nobody declared is a misspelling", doc["additionalProperties"])
	}
	// In field order, and without the read-only field that asked to be required.
	jsonEqual(t, doc["required"], `["title","roles"]`)
	if got, want := len(doc["properties"].(map[string]any)), 5; got != want {
		t.Errorf("%d properties, want %d", got, want)
	}
	jsonEqual(t, property(t, doc, "title"), `{"type":"string","description":"Short summary of the task"}`)
	jsonEqual(t, property(t, doc, "createdAt"), `{"type":"string","format":"date-time","readOnly":true}`)

	// A record with nothing to send says so by carrying no required at all,
	// rather than by carrying an empty list a consumer has to read as nothing.
	optional, err := entity.JSONSchema([]entity.Field{{Name: "note", Type: entity.TypeString}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := optional["required"]; ok {
		t.Errorf("an all-optional record carries required: %v", optional["required"])
	}
	// And a record of nothing at all is still a record: an object with no
	// properties, which is what an empty command argument is.
	empty, err := entity.JSONSchema(nil)
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, empty["properties"], `{}`)
}

// TestJSONSchemaLeavesTheFieldsItIsGiven: the fields are the caller's — the
// same slice a form renders and a PATCH merge decodes into — and the document
// is a reading of them, not a use of them.
func TestJSONSchemaLeavesTheFieldsItIsGiven(t *testing.T) {
	t.Parallel()
	fields := []entity.Field{
		{Name: "status", Type: entity.TypeString, Enum: []string{"open", "done"}, Default: "open", Widget: "select"},
		{Name: "at", Type: entity.TypeTime},
	}
	before := append([]entity.Field(nil), fields...)
	first, err := entity.JSONSchema(fields)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fields, before) {
		t.Errorf("the projection wrote to the caller's fields: %+v", fields)
	}
	second, err := entity.JSONSchema(fields)
	if err != nil {
		t.Fatal(err)
	}
	a, b := mustJSON(t, first), mustJSON(t, second)
	if string(a) != string(b) {
		t.Errorf("the same fields produced two documents:\n%s\n%s", a, b)
	}
}

// TestJSONSchemaGolden is the whole projection at once, on the shape of the
// entity this repository is usually asked about.
//
// The fields are written out rather than derived here because kit/entity is a
// leaf: modules/task imports it, so importing task to describe a task would be
// a cycle, not a test. The literal mirrors modules/task/contracts.Task — its
// json, enum, default, doc and ui tags, and the three read-only columns
// crud.Base contributes — and modules/task's TestTaskSchemaIsTheProjectedShape
// re-derives this document from the real entity, so a tag that changes there
// fails here rather than leaving this file to be believed.
// Regenerate with
//
//	UPDATE_GOLDEN=1 go test ./kit/entity -run TestJSONSchemaGolden
func TestJSONSchemaGolden(t *testing.T) {
	t.Parallel()
	doc, err := entity.JSONSchema(taskFields())
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const golden = "testdata/task.schema.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(got) {
		t.Fatalf("testdata/task.schema.json is stale; run with UPDATE_GOLDEN=1.\n%s", got)
	}
}

// taskFields is what Fields[*contracts.Task]() derives, spelled out: the Base
// columns first, then the entity's own in declaration order.
func taskFields() []entity.Field {
	return []entity.Field{
		{Name: "id", Type: entity.TypeUUID, ReadOnly: true},
		{Name: "createdAt", Type: entity.TypeTime, ReadOnly: true},
		{Name: "updatedAt", Type: entity.TypeTime, ReadOnly: true},
		{Name: "title", Type: entity.TypeString, Required: true, Doc: "Short summary of the task"},
		{Name: "description", Type: entity.TypeText, Widget: "textarea", HideList: true, Doc: "Detailed description of the task"},
		{Name: "status", Type: entity.TypeString, Widget: "select", Doc: "Lifecycle state",
			Enum: []string{"open", "acknowledged", "in_progress", "resolved", "closed"}, Default: "open"},
		{Name: "priority", Type: entity.TypeString, Widget: "select", Doc: "Task priority",
			Enum: []string{"low", "normal", "high", "critical"}, Default: "normal"},
		{Name: "source", Type: entity.TypeString, HideList: true, Doc: "Origin of the task"},
		{Name: "sourceRef", Type: entity.TypeString, HideList: true, Doc: "Reference into the source system"},
		{Name: "assigneeId", Type: entity.TypeUUID, Widget: "entity-picker", Doc: "User responsible for the task"},
		{Name: "dueAt", Type: entity.TypeTime, Widget: "datetime", Doc: "Soft target completion time"},
		{Name: "slaDeadline", Type: entity.TypeTime, Widget: "datetime", Doc: "Hard SLA deadline; a breach is measured against this"},
		{Name: "slaBreached", Type: entity.TypeBool, Widget: "checkbox", Default: "false",
			Doc: "True once the deadline elapsed with the task unresolved"},
		{Name: "resolvedAt", Type: entity.TypeTime, Widget: "datetime", HideList: true, Doc: "When the task was resolved"},
		{Name: "resolution", Type: entity.TypeText, Widget: "textarea", HideList: true, Doc: "How the task was resolved"},
	}
}

// property is one property of a returned document, which is where every case
// above but the record-level ones looks.
func property(t *testing.T, doc map[string]any, name string) any {
	t.Helper()
	props, ok := doc["properties"].(map[string]any)
	if !ok {
		t.Fatalf("the document has no properties object: %v", doc)
	}
	p, ok := props[name]
	if !ok {
		t.Fatalf("no %q property in %v", name, props)
	}
	return p
}

// jsonEqual compares a value with the JSON it should produce, both read back as
// generic JSON values so the test asks about the document and not about Go.
func jsonEqual(t *testing.T, got any, want string) {
	t.Helper()
	var gotJSON, wantJSON any
	if err := json.Unmarshal(mustJSON(t, got), &gotJSON); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &wantJSON); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Fatalf("got %s, want %s", mustJSON(t, got), want)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
