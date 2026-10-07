package rest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

type patchWireSchema struct {
	Type                 any
	Format               string
	Description          string
	Properties           map[string]*patchWireSchema
	Required             []string
	AdditionalProperties any
	Default              any
	ReadOnly             bool
	Enum                 []any
	MinLength, MaxLength *int
	AnyOf                []*patchWireSchema
	Items                *patchWireSchema
}

func patchDocumentAPI() (*httpx.API, http.Handler) {
	return httpx.New(httpx.Options{Docs: true, Unwired: true, Tenants: caller{}, Authorize: caller{}, Cache: cache.Memory("patch-document"),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
	})
}

func patchComponents(t *testing.T, router http.Handler) map[string]*patchWireSchema {
	t.Helper()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("document = %d %s", w.Code, w.Body.String())
	}
	var doc struct {
		Components struct{ Schemas map[string]*patchWireSchema }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Components.Schemas
}

func TestPatchDocumentNamesOnlyTheFieldsItsDecoderWrites(t *testing.T) {
	api, router := patchDocumentAPI()
	s := spec
	s.Immutable = []string{"status", "done"}
	s.Mount(api.Surfaces(s.Module))
	schemas := patchComponents(t, router)
	patch := schemas["TaskPatch"]
	if patch == nil {
		t.Fatal("TaskPatch is missing from the served document")
	}
	if patch.Type != "object" || patch.AdditionalProperties != false || len(patch.Required) != 0 {
		t.Fatalf("PATCH must be a closed object with optional properties: %+v", patch)
	}
	want := []string{"title", "priority", "notes", "dueAt", "tags"}
	if len(patch.Properties) != len(want) {
		t.Errorf("properties = %v, want %v", patch.Properties, want)
	}
	for _, name := range want {
		if _, ok := patch.Properties[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	if !slices.Contains(schemas["Task"].Required, "title") {
		t.Error("entity schema lost its required title")
	}
	op := recorded(t, api, "tasks-task-update")
	if !op.RequestBody.Required {
		t.Error("request body is no longer required")
	}
	if got := op.RequestBody.Content["application/json"].Schema.Ref; got != "#/components/schemas/TaskPatch" {
		t.Errorf("PATCH reference = %q", got)
	}
}

// TaggedPatch exercises tags that metadata and the wire library interpret
// differently. ReadOnly below is writable to merge; a default is not a patch.
type TaggedPatch struct {
	crud.Base
	Title    string     `json:"title" minLength:"1" maxLength:"80" doc:"A title"`
	State    string     `json:"state" enum:"open,done" default:"open"`
	Count    int64      `json:"count" default:"7"`
	Enabled  bool       `json:"enabled" default:"true"`
	ReadOnly string     `json:"readOnly" readOnly:"true"`
	DueAt    *time.Time `json:"dueAt,omitempty"`
	Choices  []string   `json:"choices,omitempty"`
	Markdown string     `json:"markdown" ui:"widget:richtext"`
}

func (TaggedPatch) TableName() string { return "tagged_patch" }

func TestPatchPropertiesRetainTypesWithoutChangingEntitySchemas(t *testing.T) {
	api, router := patchDocumentAPI()
	s := rest.Spec[*TaggedPatch]{Module: "tagged", Entity: "tagged", Path: "/rows", Read: "tagged:read", Write: "tagged:write", RichTextFiles: richtext.RejectImages{}}
	s.Mount(api.Surfaces(s.Module))
	schemas := patchComponents(t, router)
	patch, entity := schemas["TaggedPatchPatch"], schemas["TaggedPatch"]
	if patch == nil {
		t.Fatal("typed patch is missing")
	}
	for name, typ := range map[string]string{"title": "string", "state": "string", "count": "integer", "enabled": "boolean", "readOnly": "string", "markdown": "string"} {
		p := patch.Properties[name]
		if p == nil || p.Type != typ || p.Default != nil || p.ReadOnly {
			t.Errorf("%s = %+v", name, p)
		}
	}
	if got := patch.Properties["count"].Format; got != "int64" {
		t.Errorf("count format = %q", got)
	}
	if got := patch.Properties["state"].Enum; !reflect.DeepEqual(got, []any{"open", "done"}) {
		t.Errorf("enum = %v", got)
	}
	title := patch.Properties["title"]
	if title.Description != "A title" || title.MinLength == nil || *title.MinLength != 1 || title.MaxLength == nil || *title.MaxLength != 80 {
		t.Errorf("title constraints = %+v", title)
	}
	for name, typ := range map[string]string{"dueAt": "string", "choices": "array"} {
		arms := patch.Properties[name].AnyOf
		if len(arms) != 2 || arms[0].Type != typ || arms[1].Type != "null" {
			t.Fatalf("%s nullable shape = %+v", name, arms)
		}
		if name == "dueAt" && arms[0].Format != "date-time" {
			t.Error("deadline lost date-time format")
		}
		if name == "choices" && (arms[0].Items == nil || arms[0].Items.Type != "string") {
			t.Error("list lost item type")
		}
	}
	if entity.Properties["state"].Default != "open" || !entity.Properties["readOnly"].ReadOnly || !slices.Contains(entity.Required, "title") {
		t.Error("projection altered the entity schema")
	}
	// Extensions are not populated by Schema.UnmarshalJSON, so read the wire.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	var doc map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	components := doc["components"].(map[string]any)["schemas"].(map[string]any)
	for _, name := range []string{"TaggedPatch", "TaggedPatchPatch"} {
		property := components[name].(map[string]any)["properties"].(map[string]any)["markdown"].(map[string]any)
		if property["contentMediaType"] != "text/markdown" {
			t.Errorf("%s markdown = %v", name, property)
		}
	}
}

func TestPatchComponentReuseCollisionAndAPILocality(t *testing.T) {
	api, router := patchDocumentAPI()
	first := spec
	first.Mount(api.Surfaces(first.Module))
	original, err := json.Marshal(patchComponents(t, router)["Task"])
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.Path, second.Entity = "/second", "second"
	second.Mount(api.Surfaces(second.Module))
	after, err := json.Marshal(patchComponents(t, router)["Task"])
	if err != nil {
		t.Fatal(err)
	}
	if string(original) != string(after) {
		t.Error("second projection changed the entity schema")
	}
	t.Run("conflicting projection", func(t *testing.T) {
		defer func() {
			if got := fmt.Sprint(recover()); got != "rest: PATCH schema TaskPatch already describes a different writable shape" {
				t.Errorf("collision = %s", got)
			}
		}()
		third := second
		third.Path, third.Entity, third.Immutable = "/third", "third", []string{"status"}
		third.Mount(api.Surfaces(third.Module))
	})
	other, otherRouter := patchDocumentAPI()
	second.Immutable = []string{"status"}
	second.Mount(other.Surfaces(second.Module))
	if _, exists := patchComponents(t, otherRouter)["TaskPatch"].Properties["status"]; exists {
		t.Error("another API reused the first API's projection")
	}
	if _, exists := patchComponents(t, router)["TaskPatch"].Properties["status"]; !exists {
		t.Error("collision replaced the existing projection")
	}
}

func TestNoUpdateOffersNoPatchComponent(t *testing.T) {
	api, router := patchDocumentAPI()
	s := spec
	s.Operations = []httpx.CRUD{httpx.CRUDRead}
	s.Mount(api.Surfaces(s.Module))
	if patchComponents(t, router)["TaskPatch"] != nil {
		t.Error("unused PATCH component")
	}
	for _, op := range api.Recorded() {
		if op.Method == http.MethodPatch {
			t.Errorf("unexpected PATCH: %s", op.Path)
		}
	}
}

func TestTypedPatchDocumentationKeepsMergeErrorsAndWritesAtomic(t *testing.T) {
	s := spec
	s.Immutable = []string{"status"}
	_, router, admin := mount(t, s)
	code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"before","priority":3,"dueAt":"2030-01-01T00:00:00Z"}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, body)
	}
	at := "/api/v1/tasks/task/" + id(t, body)
	for _, tt := range []struct{ body, detail string }{
		{`{"unknown":1}`, `there is no field`},
		{`{"Secret":"hidden"}`, `there is no field`},
		{`{"Title":"changed"}`, `there is no field`},
		{`{"createdAt":"2030-01-01T00:00:00Z"}`, `createdAt is read-only`},
		{`{"title":"after","STATUS":null}`, `status belongs to a route of its own`},
		{`{"ſtatus":"done"}`, `status belongs to a route of its own`},
		{`{"priority":"wrong"}`, `priority: json: cannot unmarshal string`},
		{`{"tags":[1]}`, `tags: json: cannot unmarshal number`},
		{`{"dueAt":"wrong"}`, `dueAt: parsing time`},
	} {
		t.Run(tt.body, func(t *testing.T) {
			code, body := call(t, router, http.MethodPatch, at, tt.body)
			if code != http.StatusUnprocessableEntity || !strings.Contains(body, tt.detail) {
				t.Errorf("PATCH = %d %s, want merge error %q", code, body, tt.detail)
			}
		})
	}
	if n := count(t, admin, s.Event(rest.Updated)); n != 0 {
		t.Errorf("refusals emitted %d updates", n)
	}
	if code, body := call(t, router, http.MethodGet, at, ""); code != 200 || !strings.Contains(body, `"title":"before"`) || !strings.Contains(body, `"priority":3`) || !strings.Contains(body, `"dueAt":"2030-01-01T00:00:00Z"`) {
		t.Fatalf("refused writes changed row: %d %s", code, body)
	}
	if code, body := call(t, router, http.MethodPatch, at, `{"dueAt":null,"priority":0,"done":false,"notes":""}`); code != 200 {
		t.Fatalf("clear = %d %s", code, body)
	}
	var deadline *time.Time
	var priority int
	if err := admin.QueryRowContext(t.Context(), `SELECT due_at, priority FROM rest_tasks WHERE id=$1`, strings.TrimPrefix(at, "/api/v1/tasks/task/")).Scan(&deadline, &priority); err != nil {
		t.Fatal(err)
	}
	if deadline != nil || priority != 0 {
		t.Errorf("clear stored deadline %v priority %d", deadline, priority)
	}
}
