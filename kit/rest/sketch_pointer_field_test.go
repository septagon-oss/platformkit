package rest_test

import (
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// PointerSketchRow is the same drawing declared as a nullable Go field, which is
// how a product asks for a column that may hold nothing at all.
type PointerSketchRow struct {
	crud.Base
	Title    string         `json:"title"`
	Drawing  *entity.Sketch `json:"drawing" required:"false" gorm:"type:jsonb" ui:"label:Drawing"`
	Revision int64          `json:"revision" required:"false" readOnly:"true" gorm:"default:1"`
}

func (PointerSketchRow) TableName() string { return "rest_pointer_drawings" }

const pointerSketchDDL = `CREATE TABLE rest_pointer_drawings (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL, created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL, deleted_at timestamptz, title text NOT NULL,
 drawing jsonb, revision bigint NOT NULL DEFAULT 1);
 ALTER TABLE rest_pointer_drawings ENABLE ROW LEVEL SECURITY;
 ALTER TABLE rest_pointer_drawings FORCE ROW LEVEL SECURITY;
 CREATE POLICY pointer_drawings_tenant ON rest_pointer_drawings
	USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id));`

// TestAPointerSketchFieldKeepsUnsetApartFromAClear writes the drawing through a
// pointer field, so the answer comes from the column rather than from the codec:
// a drawing nobody drew stays unset, an explicitly empty board stays empty, and
// the limit is the same 422 the value itself carries.
func TestAPointerSketchFieldKeepsUnsetApartFromAClear(t *testing.T) {
	s := rest.Spec[*PointerSketchRow]{Module: "drawing", Entity: "drawing", Path: "/drawings", Read: "drawing:read", Write: "drawing:write"}
	_, router, admin := mountAs(t, s, hosts{map[string]tenancy.Tenant{host: acme}})
	if _, err := admin.ExecContext(t.Context(), pointerSketchDDL); err != nil {
		t.Fatal(err)
	}
	stored := func(path string) string {
		t.Helper()
		code, body := call(t, router, "GET", path, "")
		if code != 200 {
			t.Fatal(code, body)
		}
		var got struct {
			Drawing json.RawMessage `json:"drawing"`
		}
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatal(err)
		}
		return string(got.Drawing)
	}
	// Unset, empty and drawn are three states a column has to answer differently.
	for _, raw := range []string{"null", "[]", drawingA} {
		code, body := call(t, router, "POST", "/api/v1/drawing/drawings", `{"title":"Pointer","drawing":`+raw+`}`)
		if code != 201 {
			t.Fatalf("create %s: %d %s", raw, code, body)
		}
		if got := stored("/api/v1/drawing/drawings/" + id(t, body)); got != raw {
			t.Errorf("a pointer field read back %s where %s was written", got, raw)
		}
	}
	// The clear a person made is the state the record keeps: an erased board is an
	// empty drawing, not a drawing nobody ever drew.
	code, body := call(t, router, "POST", "/api/v1/drawing/drawings", `{"title":"Erased","drawing":`+drawingA+`}`)
	if code != 201 {
		t.Fatal(code, body)
	}
	path := "/api/v1/drawing/drawings/" + id(t, body)
	if code, body = call(t, router, "PATCH", path, `{"drawing":[]}`); code != 200 {
		t.Fatal(code, body)
	}
	if got := stored(path); got != "[]" {
		t.Errorf("an erased drawing reads back %s", got)
	}
	if code, body = call(t, router, "PATCH", path, `{"drawing":`+drawingA+`}`); code != 200 {
		t.Fatal(code, body)
	}
	if got := stored(path); got != drawingA {
		t.Errorf("a drawing written over an erased one reads back %s", got)
	}
	if code, body = call(t, router, "PATCH", path, `{"drawing":[[[1001,0,0]]]}`); code != 422 {
		t.Errorf("a pointer field answered the square differently: %d %s", code, body)
	}
}
