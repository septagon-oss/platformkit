package rest_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// square is a drawing of exactly strokes strokes and exactly points points: the
// ceiling the kernel declares, not one point under it.
func square(strokes, points int) string {
	per := points / strokes
	extra := points - per*strokes
	var b strings.Builder
	b.WriteByte('[')
	for s := 0; s < strokes; s++ {
		if s > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('[')
		for p := 0; p < per+boolInt(s < extra); p++ {
			if p > 0 {
				b.WriteByte(',')
			}
			b.WriteString("[0,0,0]")
		}
		b.WriteByte(']')
	}
	b.WriteByte(']')
	return b.String()
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// TestADrawingAtTheDeclaredCeilingSurvivesEveryDoor writes the largest drawing the
// kernel promises and the smallest it refuses, through the same row: a limit read
// one way at the door and another way in the value is a drawing a person loses.
func TestADrawingAtTheDeclaredCeilingSurvivesEveryDoor(t *testing.T) {
	s := rest.Spec[*SketchRow]{Module: "drawing", Entity: "drawing", Path: "/drawings", Read: "drawing:read", Write: "drawing:write"}
	_, router, admin := mountAs(t, s, hosts{map[string]tenancy.Tenant{host: acme}})
	if _, err := admin.ExecContext(t.Context(), sketchDDL); err != nil {
		t.Fatal(err)
	}
	atTheCeiling := square(entity.SketchMaxStrokes, entity.SketchMaxPoints)
	code, body := call(t, router, "POST", "/api/v1/drawing/drawings", `{"title":"Full","drawing":`+atTheCeiling+`}`)
	if code != 201 {
		t.Fatalf("the declared ceiling refused: %d %s", code, body)
	}
	path := "/api/v1/drawing/drawings/" + id(t, body)
	stored := func() string {
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
	if got := stored(); got != atTheCeiling {
		t.Fatalf("a drawing at the ceiling came back %s bytes from %d strokes of storage", got[:min(60, len(got))], entity.SketchMaxStrokes)
	}
	// One more stroke, or one more point, is refused at the door and changes nothing.
	oneMoreStroke := square(entity.SketchMaxStrokes+1, entity.SketchMaxStrokes+1)
	oneMorePoint := square(1, entity.SketchMaxPoints+1)
	for _, over := range []string{oneMoreStroke, oneMorePoint} {
		if code, body := call(t, router, "PATCH", path, `{"drawing":`+over+`}`); code != 422 || !strings.Contains(body, "drawing") {
			t.Errorf("over the ceiling: %d %s", code, body)
		}
		if got := stored(); got != atTheCeiling {
			t.Fatalf("a refused write moved the row: %d bytes", len(got))
		}
	}
	if n := count(t, admin, s.Event(rest.Created)); n != 1 {
		t.Errorf("%d created events for one accepted drawing", n)
	}
	if n := count(t, admin, s.Event(rest.Updated)); n != 0 {
		t.Errorf("%d updates for two refused ones", n)
	}
}

// RequiredSketchRow is a resource that will not exist without a drawing — the one
// requiredness a drawing can declare, and the one the value alone cannot answer.
type RequiredSketchRow struct {
	crud.Base
	Title    string        `json:"title"`
	Drawing  entity.Sketch `json:"drawing" validate:"required" gorm:"type:jsonb" ui:"label:Drawing"`
	Revision int64         `json:"revision" required:"false" readOnly:"true" gorm:"default:1"`
}

func (RequiredSketchRow) TableName() string { return "rest_required_drawings" }

const requiredSketchDDL = `CREATE TABLE rest_required_drawings (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL, created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL, deleted_at timestamptz, title text NOT NULL,
 drawing jsonb, revision bigint NOT NULL DEFAULT 1);
 ALTER TABLE rest_required_drawings ENABLE ROW LEVEL SECURITY;
 ALTER TABLE rest_required_drawings FORCE ROW LEVEL SECURITY;
 CREATE POLICY required_drawings_tenant ON rest_required_drawings
	USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id));`

// TestARequiredSketchRefusesEveryBodyWithoutADrawing asks the question a value
// cannot answer on its own: nil and [] are both valid drawings and both fail a
// field that requires one, on every door that writes it.
func TestARequiredSketchRefusesEveryBodyWithoutADrawing(t *testing.T) {
	s := rest.Spec[*RequiredSketchRow]{Module: "drawing", Entity: "drawing", Path: "/drawings", Read: "drawing:read", Write: "drawing:write"}
	_, router, admin := mountAs(t, s, hosts{map[string]tenancy.Tenant{host: acme}}, func(api *httpx.API) {
		rest.Command(api.Surfaces(s.Module), s, "form", "Write from a form", "", []string{s.Event(rest.Updated)}, func(ctx context.Context, tx db.Tx[db.Tenant], rowID uuid.UUID, in struct {
			Body string `json:"body"`
		}) (*RequiredSketchRow, error) {
			sent, err := rest.UpdateValues([]byte(in.Body), crud.Fields[*RequiredSketchRow](), nil)
			if err != nil {
				return nil, err
			}
			return s.UpdateRow(ctx, tx, rowID, sent)
		}, rest.CommandOptions{})
	})
	if _, err := admin.ExecContext(t.Context(), requiredSketchDDL); err != nil {
		t.Fatal(err)
	}
	// No drawing at all, an unset one and an explicitly empty one are three ways of
	// asking for the same refusal on the create door.
	for _, raw := range []string{`{"title":"Nothing"}`, `{"title":"Nothing","drawing":null}`, `{"title":"Nothing","drawing":[]}`} {
		code, body := call(t, router, "POST", "/api/v1/drawing/drawings", raw)
		if code != 422 || !strings.Contains(strings.ToLower(body), "drawing") {
			t.Errorf("required create %s: %d %s", raw, code, body)
		}
	}
	if n := count(t, admin, s.Event(rest.Created)); n != 0 {
		t.Fatalf("%d rows created without a drawing", n)
	}
	code, body := call(t, router, "POST", "/api/v1/drawing/drawings", `{"title":"One","drawing":`+drawingA+`}`)
	if code != 201 {
		t.Fatal(code, body)
	}
	path := "/api/v1/drawing/drawings/" + id(t, body)
	// The update door refuses the same three, and the row keeps what it held.
	for _, raw := range []string{`{"drawing":null}`, `{"drawing":[]}`} {
		code, answer := call(t, router, "PATCH", path, raw)
		if code != 422 || strings.Contains(answer, `"revision":`) {
			t.Errorf("required patch %s: %d %s", raw, code, answer)
		}
	}
	for _, raw := range []string{"null", "[]", ""} {
		code, answer := call(t, router, "POST", path+"/form", mustJSON(t, map[string]string{"body": "drawing=" + raw}))
		if code != 422 || strings.Contains(answer, `"revision":`) {
			t.Errorf("required form body %q: %d %s", raw, code, answer)
		}
	}
	code, body = call(t, router, "GET", path, "")
	var kept struct {
		Drawing  json.RawMessage `json:"drawing"`
		Revision int64           `json:"revision"`
	}
	if code != 200 || json.Unmarshal([]byte(body), &kept) != nil || string(kept.Drawing) != drawingA || kept.Revision != 1 {
		t.Fatalf("the refusals left %s", body)
	}
	if n := count(t, admin, s.Event(rest.Updated)); n != 0 {
		t.Errorf("%d updates for five refused writes", n)
	}
}
